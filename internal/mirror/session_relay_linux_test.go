package mirror

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

func relayFixture(t *testing.T) (*attachmentRelay, *net.UnixListener, func() net.Conn) {
	t.Helper()
	base, id, server := attachmentSocketFixture(t, "s", "boot")
	pin, err := pinSessionSocket(base, "s", id, "boot")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	relay, err := startAttachmentRelay(ctx, pin, testAttachmentAttempt)
	if err != nil {
		cancel()
		pin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-relay.done:
		case <-time.After(2 * time.Second):
			t.Error("relay cleanup blocked")
		}
		pin.Close()
	})
	dial := func() net.Conn {
		c, err := net.Dial("unix", filepath.Join(pin.view, zellijlive.SocketContractDir, "session"))
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(2 * time.Second))
		t.Cleanup(func() { c.Close() })
		return c
	}
	return relay, server, dial
}

func readFrameKind(t *testing.T, c net.Conn, want uint64) []byte {
	t.Helper()
	kind, body, _, err := readAttachmentFrame(c)
	if err != nil || kind != want {
		t.Fatalf("frame kind=%d want=%d err=%v", kind, want, err)
	}
	return body
}
func acceptRelay(t *testing.T, s *net.UnixListener) net.Conn {
	t.Helper()
	s.SetDeadline(time.Now().Add(2 * time.Second))
	c, err := s.Accept()
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRelayReadinessRequiresAttachAndRenderOnSameConnection(t *testing.T) {
	r, server, dial := relayFixture(t)
	probe := dial()
	writeAttachmentFrame(probe, attachmentFrame(13, nil))
	peer := acceptRelay(t, server)
	readFrameKind(t, peer, 13)
	writeAttachmentFrame(peer, attachmentFrame(4, nil))
	readFrameKind(t, probe, 4)
	probe.Close()
	peer.Close()
	select {
	case <-r.readyCh:
		t.Fatal("probe marked ready")
	default:
	}
	client := dial()
	writeAttachmentFrame(client, attachmentFrame(8, nil))
	peer = acceptRelay(t, server)
	readFrameKind(t, peer, 8)
	writeAttachmentFrame(peer, attachmentFrame(4, nil))
	readFrameKind(t, client, 4)
	select {
	case <-r.readyCh:
		t.Fatal("Connected marked ready")
	default:
	}
	render := []byte{10, 3, 'a', 'b', 'c'}
	writeAttachmentFrame(peer, attachmentFrame(1, render))
	body := readFrameKind(t, client, 1)
	if !bytes.Contains(body, []byte(AttachmentMarker(testAttachmentAttempt, "ready"))) {
		t.Fatalf("missing ready render: %q", body)
	}
	if !bytes.Equal(readFrameKind(t, client, 1), render) {
		t.Fatal("changed real render")
	}
	select {
	case <-r.readyCh:
	case <-time.After(time.Second):
		t.Fatal("missing readiness notification")
	}
	writeAttachmentFrame(peer, attachmentFrame(1, render))
	if !bytes.Equal(readFrameKind(t, client, 1), render) {
		t.Fatal("duplicate ready marker")
	}
	writeAttachmentFrame(client, attachmentFrame(10, []byte{8, 1}))
	if !bytes.Equal(readFrameKind(t, peer, 10), []byte{8, 1}) {
		t.Fatal("changed input payload")
	}
	// A server-directed session switch must not reach the Zellij client, which
	// could otherwise loop into another session or create it.
	writeAttachmentFrame(peer, attachmentFrame(7, nil))
	if !bytes.Equal(readFrameKind(t, client, 3), []byte{8, 2}) {
		t.Fatal("switch not converted to detach")
	}
	if !r.wasDetached() {
		t.Fatal("lost intentional detach evidence")
	}
}

func TestRelayForwardsTerminalCapabilityAndFocusMessages(t *testing.T) {
	_, server, dial := relayFixture(t)
	client := dial()
	writeAttachmentFrame(client, attachmentFrame(8, nil))
	peer := acceptRelay(t, server)
	readFrameKind(t, peer, 8)
	for _, kind := range []uint64{21, 22, 23, 24, 26, 27} {
		body := []byte{8, 1}
		if err := writeAttachmentFrame(client, attachmentFrame(kind, body)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(readFrameKind(t, peer, kind), body) {
			t.Fatalf("changed payload for message %d", kind)
		}
	}
}

func TestRelayRejectsCreationAndAttachmentReplay(t *testing.T) {
	for _, kind := range []uint64{7, 8, 12, 13, 16, 25, 28} {
		t.Run(string(rune('a'+kind)), func(t *testing.T) {
			r, s, dial := relayFixture(t)
			c := dial()
			writeAttachmentFrame(c, attachmentFrame(8, nil))
			peer := acceptRelay(t, s)
			readFrameKind(t, peer, 8)
			writeAttachmentFrame(c, attachmentFrame(kind, nil))
			select {
			case <-r.failed:
			case <-time.After(time.Second):
				t.Fatal("forbidden message forwarded")
			}
			var one [1]byte
			if n, _ := peer.Read(one[:]); n != 0 {
				t.Fatal("forbidden data reached server")
			}
		})
	}
	r, _, dial := relayFixture(t)
	c := dial()
	writeAttachmentFrame(c, attachmentFrame(7, nil))
	select {
	case <-r.failed:
	case <-time.After(time.Second):
		t.Fatal("creation accepted")
	}
}

func TestRelayEOFNeverProvesDetach(t *testing.T) {
	r, s, dial := relayFixture(t)
	c := dial()
	writeAttachmentFrame(c, attachmentFrame(8, nil))
	peer := acceptRelay(t, s)
	readFrameKind(t, peer, 8)
	peer.Close()
	io.ReadAll(c)
	if r.wasDetached() {
		t.Fatal("EOF treated as deliberate detach")
	}
	select {
	case <-r.readyCh:
		t.Fatal("EOF marked ready")
	default:
	}
}

func TestAttachmentFrameBoundsAndShape(t *testing.T) {
	bad := [][]byte{{0, 0, 0, 0}, binary.LittleEndian.AppendUint32(nil, maxAttachmentFrame+1), {1, 0, 0, 0, 8}, {2, 0, 0, 0, 10, 3}, {4, 0, 0, 0, 10, 0, 10, 0}}
	for _, frame := range bad {
		if _, _, _, err := readAttachmentFrame(bytes.NewReader(frame)); err == nil {
			t.Fatalf("accepted %x", frame)
		}
	}
	for _, token := range []string{"", "Case", strings.Repeat("f", 31), "../x", testAttachmentAttempt + "\x1f"} {
		if AttachmentMarker(token, "ready") != "" {
			t.Fatalf("accepted token %q", token)
		}
	}
	if AttachmentMarker(testAttachmentAttempt, "ended") != "" {
		t.Fatal("unvalidated ended event exists")
	}
}

func FuzzAttachmentFrame(f *testing.F) {
	f.Add(attachmentFrame(8, nil))
	f.Add(attachmentReadyFrame(testAttachmentAttempt))
	f.Fuzz(func(t *testing.T, b []byte) { _, _, _, _ = readAttachmentFrame(bytes.NewReader(b)) })
}
