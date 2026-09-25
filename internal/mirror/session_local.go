package mirror

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// SessionInputOrigin binds asynchronous clipboard work to one helper instance
// and one admitted input generation, not merely a Kitty socket or session name.
type SessionInputOrigin struct {
	Client, Attempt string
	Generation      uint64
}

type SessionLocalState struct {
	Transport, Token, Session, SessionID, State string
	Origin                                      SessionInputOrigin
}

type sessionLocalWire struct {
	Version int
	Origin  *SessionInputOrigin `json:",omitempty"`
	Input   []byte              `json:",omitempty"`
	State   *SessionLocalState  `json:",omitempty"`
	Error   string              `json:",omitempty"`
}

type sessionLocalCall struct {
	ctx     context.Context
	peer    *os.File
	request sessionLocalWire
	reply   chan sessionLocalWire
}

// SessionLocalControl belongs to one terminal helper. It neither schedules
// recovery nor queues input for a future transport. The supervisor event loop
// serializes origin validation and the actual write to its current child PTY.
type SessionLocalControl struct {
	requests chan sessionLocalCall
	cancel   context.CancelFunc
	done     chan struct{}
}

func localSessionAddress(paths *recoveryPaths, token string) (string, string, error) {
	if !correlationTokenPattern.MatchString(token) {
		return "", "", fmt.Errorf("invalid projection token")
	}
	name := "view-" + token + ".sock"
	return fmt.Sprintf("/proc/self/fd/%d/%s", paths.dir.Fd(), name), name, nil
}

func StartSessionLocal(ctx context.Context, remote RemoteConfig, runtime, token string) (*SessionLocalControl, error) {
	if !correlationTokenPattern.MatchString(token) {
		return nil, fmt.Errorf("invalid projection token")
	}
	paths, err := openRecoveryPaths(remote, runtime)
	if err != nil {
		return nil, err
	}
	address, name, err := localSessionAddress(paths, token)
	if err != nil {
		paths.close()
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		paths.close()
		return nil, fmt.Errorf("bind terminal control: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	var identity unix.Stat_t
	if err := unix.Fstatat(int(paths.dir.Fd()), name, &identity, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		listener.Close()
		paths.close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	local := &SessionLocalControl{requests: make(chan sessionLocalCall), cancel: cancel, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	go func() {
		defer close(local.done)
		defer paths.close()
		defer func() {
			stop()
			listener.Close()
			var current unix.Stat_t
			if unix.Fstatat(int(paths.dir.Fd()), name, &current, unix.AT_SYMLINK_NOFOLLOW) == nil && current.Dev == identity.Dev && current.Ino == identity.Ino {
				_ = unix.Unlinkat(int(paths.dir.Fd()), name, 0)
			}
		}()
		var handlers sync.WaitGroup
		defer handlers.Wait()
		capacity := make(chan struct{}, 16)
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			select {
			case capacity <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			handlers.Add(1)
			go func() { defer handlers.Done(); defer func() { <-capacity }(); local.serve(ctx, conn) }()
		}
	}()
	return local, nil
}

func (local *SessionLocalControl) Close() { local.cancel(); <-local.done }

func (local *SessionLocalControl) serve(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	_, peer, err := recoveryPeer(conn)
	if err != nil {
		return
	}
	defer peer.Close()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	var request sessionLocalWire
	if readRecoveryFrame(bufio.NewReaderSize(conn, recoveryFrameLimit+1), &request) != nil {
		return
	}
	if request.Version != 1 || request.State != nil || request.Error != "" || len(request.Input) > 4096 ||
		(request.Origin == nil && len(request.Input) != 0) || !recoveryPeerAlive(peer) {
		return
	}
	call := sessionLocalCall{ctx: ctx, peer: peer, request: request, reply: make(chan sessionLocalWire, 1)}
	select {
	case local.requests <- call:
	case <-ctx.Done():
		return
	}
	select {
	case reply := <-call.reply:
		if sessionContextError(ctx) == nil && recoveryPeerAlive(peer) {
			_ = writeRecoveryFrame(conn, reply)
		}
	case <-ctx.Done():
	}
}

// BindSessionInput must run before any asynchronous clipboard acquisition or
// upload. Every completion, including Ctrl-V fallback, revalidates this origin
// inside the terminal event loop immediately before writing the original PTY.
func BindSessionInput(ctx context.Context, remote RemoteConfig, runtime, token string) (func(context.Context, []byte) error, error) {
	state, _, err := SessionLocalExchange(ctx, remote, runtime, token, nil, nil)
	if err != nil {
		return nil, err
	}
	if state.State != string(sessionReady) || state.Origin.Generation == 0 {
		return nil, fmt.Errorf("attachment is not ready; paste discarded")
	}
	origin := state.Origin
	return func(ctx context.Context, data []byte) error {
		_, _, err := SessionLocalExchange(ctx, remote, runtime, token, &origin, data)
		return err
	}, nil
}

// SessionLocalExchange does not start a helper/coordinator or create runtime
// directories. A nil origin observes presence/readiness; input requires the
// exact origin obtained before clipboard acquisition/upload began. The peer PID
// can be matched against independently verified process-tree evidence.
func SessionLocalExchange(ctx context.Context, remote RemoteConfig, runtime, token string, origin *SessionInputOrigin, input []byte) (SessionLocalState, int, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	paths, err := recoveryPathsAt(remote, runtime, false)
	if err != nil {
		return SessionLocalState{}, 0, err
	}
	defer paths.close()
	address, _, err := localSessionAddress(paths, token)
	if err != nil {
		return SessionLocalState{}, 0, err
	}
	if len(input) > 4096 || origin == nil && len(input) != 0 {
		return SessionLocalState{}, 0, fmt.Errorf("invalid terminal input request")
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", address)
	if err != nil {
		return SessionLocalState{}, 0, err
	}
	defer connection.Close()
	conn := connection.(*net.UnixConn)
	pid, peer, err := recoveryPeer(conn)
	if err != nil {
		return SessionLocalState{}, 0, err
	}
	defer peer.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if err := writeRecoveryFrame(conn, sessionLocalWire{Version: 1, Origin: origin, Input: input}); err != nil {
		return SessionLocalState{}, 0, err
	}
	var reply sessionLocalWire
	if err := readRecoveryFrame(bufio.NewReaderSize(conn, recoveryFrameLimit+1), &reply); err != nil {
		return SessionLocalState{}, 0, err
	}
	if err := sessionContextError(ctx); err != nil {
		return SessionLocalState{}, 0, err
	}
	if !recoveryPeerAlive(peer) || reply.Version != 1 || reply.Origin != nil || len(reply.Input) != 0 {
		return SessionLocalState{}, 0, fmt.Errorf("invalid terminal control response")
	}
	if reply.Error != "" {
		return SessionLocalState{}, 0, fmt.Errorf("terminal input: %s", reply.Error)
	}
	identity, err := recoveryIdentity(remote)
	if err != nil {
		return SessionLocalState{}, 0, err
	}
	if reply.State == nil || reply.State.Transport != identity || reply.State.Token != token {
		return SessionLocalState{}, 0, fmt.Errorf("terminal identity changed")
	}
	return *reply.State, pid, nil
}
