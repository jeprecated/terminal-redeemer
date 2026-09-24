package mirror

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

// The pinned 0.44.3 IPC contract is LE32 length + a single protobuf oneof.
// We inspect only its envelope, never interpret actions/keys/layouts. See
// docs/testing/exact-mirror-attachment.md for the upstream source contract.
const maxAttachmentFrame = 4 << 20

func readAttachmentFrame(r io.Reader) (kind uint64, body, frame []byte, err error) {
	var header [4]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 || size > maxAttachmentFrame {
		err = fmt.Errorf("invalid Zellij frame length %d", size)
		return
	}
	frame = make([]byte, 4+int(size))
	copy(frame, header[:])
	if _, err = io.ReadFull(r, frame[4:]); err != nil {
		return
	}
	payload := frame[4:]
	tag, n := binary.Uvarint(payload)
	if n <= 0 || tag&7 != 2 || tag>>3 == 0 {
		err = fmt.Errorf("invalid Zellij oneof tag")
		return
	}
	size64, m := binary.Uvarint(payload[n:])
	if m <= 0 || size64 != uint64(len(payload)-n-m) {
		err = fmt.Errorf("invalid Zellij oneof payload")
		return
	}
	kind, body = tag>>3, payload[n+m:]
	return
}

func attachmentFrame(kind uint64, body []byte) []byte {
	payload := binary.AppendUvarint(nil, kind<<3|2)
	payload = binary.AppendUvarint(payload, uint64(len(body)))
	payload = append(payload, body...)
	frame := binary.LittleEndian.AppendUint32(nil, uint32(len(payload)))
	return append(frame, payload...)
}

func writeAttachmentFrame(w io.Writer, frame []byte) error {
	_, err := io.Copy(w, bytes.NewReader(frame))
	return err
}

// The readiness marker travels as a synthetic Render immediately before the
// first real Render. Only the Zellij client writes it to the terminal, avoiding
// interleaving helper writes with client output and proving client processing.
func attachmentReadyFrame(attempt string) []byte {
	marker := AttachmentMarker(attempt, "ready")
	body := binary.AppendUvarint(nil, 1<<3|2)
	body = binary.AppendUvarint(body, uint64(len(marker)))
	return attachmentFrame(1, append(body, marker...))
}

type attachmentRelay struct {
	listener                  net.Listener
	endpoint, attempt         string
	mu                        sync.Mutex
	attached, ready, detached bool
	failed                    chan error
	readyCh                   chan struct{}
	done                      chan struct{}
}

func startAttachmentRelay(ctx context.Context, pinned *pinnedSessionSocket, attempt string) (*attachmentRelay, error) {
	if !validAttachmentAttempt(attempt) {
		return nil, fmt.Errorf("invalid attachment attempt")
	}
	listener, err := net.Listen("unix", filepath.Join(pinned.view, zellijlive.SocketContractDir, "session"))
	if err != nil {
		return nil, err
	}
	relay := &attachmentRelay{listener: listener, endpoint: pinned.endpoint, attempt: attempt, failed: make(chan error, 1), readyCh: make(chan struct{}), done: make(chan struct{})}
	go relay.serve(ctx)
	return relay, nil
}

func (r *attachmentRelay) serve(ctx context.Context) {
	defer close(r.done)
	stop := context.AfterFunc(ctx, func() { r.listener.Close() })
	defer stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, 4)
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			wg.Add(1)
			go func() { defer wg.Done(); defer func() { <-slots }(); r.connection(ctx, conn) }()
		default:
			conn.Close()
		}
	}
}

func (r *attachmentRelay) fail(err error) {
	select {
	case r.failed <- err:
	default:
	}
}

func (r *attachmentRelay) connection(ctx context.Context, client net.Conn) {
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	// Probe connections must not hold resources indefinitely. The interactive
	// connection is instead bounded by the wrapper's pre-readiness deadline.
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, _, first, err := readAttachmentFrame(client)
	if err != nil {
		return
	}
	if kind != 8 && kind != 13 {
		r.fail(fmt.Errorf("refused non-attach initial Zellij message %d", kind))
		return
	}
	interactive := kind == 8
	if interactive {
		r.mu.Lock()
		duplicate := r.attached
		r.attached = true
		r.mu.Unlock()
		if duplicate {
			r.fail(fmt.Errorf("refused second interactive attachment"))
			return
		}
	}
	server, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", r.endpoint)
	if err != nil {
		if interactive {
			r.fail(err)
		}
		return
	}
	defer server.Close()
	cancelServer := context.AfterFunc(ctx, func() { server.Close() })
	defer cancelServer()
	if !interactive {
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		if err = writeAttachmentFrame(server, first); err != nil {
			return
		}
		reply, _, frame, e := readAttachmentFrame(server)
		if e == nil && reply == 4 {
			_ = writeAttachmentFrame(client, frame)
		}
		return // Connected is a probe reply, never attachment readiness.
	}
	_ = client.SetReadDeadline(time.Time{})
	if err = writeAttachmentFrame(server, first); err != nil {
		r.fail(err)
		return
	}
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			kind, _, frame, err := readAttachmentFrame(client)
			if err != nil {
				server.Close()
				return
			}
			// No NewClient, attach replay, independent CLI request or kill-session
			// authority can pass through this attempt after its initial AttachClient.
			if kind == 7 || kind == 8 || kind == 12 || kind == 13 || kind == 16 || kind == 0 || kind > 20 {
				r.fail(fmt.Errorf("refused Zellij message %d on attached connection", kind))
				server.Close()
				return
			}
			if err := writeAttachmentFrame(server, frame); err != nil {
				server.Close()
				return
			}
		}
	}()
	defer func() { client.Close(); server.Close(); <-inputDone }()
	for {
		kind, body, frame, err := readAttachmentFrame(server)
		if err != nil {
			return
		} // EOF is not intentional detach or session end.
		if kind == 7 {
			// Zellij otherwise loops into a new session (and can create it). Convert
			// switching to detach, preserving this projection's immutable identity.
			frame = attachmentFrame(3, []byte{8, 2})
			kind, body = 3, []byte{8, 2}
		}
		r.mu.Lock()
		if kind == 1 && !r.ready {
			r.ready = true
			r.mu.Unlock()
			if err := writeAttachmentFrame(client, attachmentReadyFrame(r.attempt)); err != nil {
				return
			}
			close(r.readyCh)
		} else {
			r.mu.Unlock()
		}
		if kind == 3 && (bytes.Equal(body, []byte{8, 1}) || bytes.Equal(body, []byte{8, 2})) {
			r.mu.Lock()
			r.detached = r.ready
			r.mu.Unlock()
		}
		if err := writeAttachmentFrame(client, frame); err != nil {
			return
		}
		if kind == 3 {
			return
		}
	}
}

func (r *attachmentRelay) wasDetached() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.detached }
