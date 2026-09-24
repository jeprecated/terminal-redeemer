package mirror

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

type recoveryConnection struct {
	conn   *net.UnixConn
	pid    int
	pidfd  *os.File
	client string
}
type recoveryServer struct {
	mu          sync.Mutex
	model       *sessionRecovery
	connections map[*net.UnixConn]*recoveryConnection
	owners      map[string]*recoveryConnection
	key         string
}

func recoveryDisplayReason(reason string) string {
	text := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, reason))
	if len(text) > 512 {
		text = text[:512]
	}
	return string(text)
}
func (s *recoveryServer) status() SessionRecoveryStatus {
	status := SessionRecoveryStatus{Available: s.model.available, Checking: s.model.checking, RetryAt: s.model.nextProbe, Reason: recoveryDisplayReason(s.model.reason), Members: len(s.model.members)}
	for _, m := range s.model.members {
		if m.ready {
			status.Ready++
		} else if m.active != "" {
			status.Pending++
		}
	}
	return status
}
func (s *recoveryServer) serve(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	pid, pidfd, err := recoveryPeer(conn)
	if err != nil {
		return
	}
	retained := false
	defer func() {
		if !retained {
			pidfd.Close()
		}
	}()
	peer := &recoveryConnection{conn: conn, pid: pid, pidfd: pidfd}
	s.mu.Lock()
	s.connections[conn] = peer
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.connections, conn)
		if peer.client != "" && s.owners[peer.client] == peer {
			m := s.model.members[peer.client]
			if ctx.Err() == nil && m != nil && m.active != "" && !m.ready && recoveryPeerAlive(pidfd) {
				// A broken control connection is NOT evidence that its SSH child
				// was reaped. Retain the slot and process proof for reconnect.
				peer.conn = nil
				retained = true
			} else {
				delete(s.owners, peer.client)
				s.model.remove(peer.client)
			}
		}
	}()
	reader := bufio.NewReaderSize(conn, recoveryFrameLimit)
	for {
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		var message recoveryWireRequest
		if readRecoveryFrame(reader, &message) != nil || !recoveryPeerAlive(pidfd) {
			return
		}
		reply := recoveryWireReply{Version: 1, Transport: s.key}
		s.mu.Lock()
		switch {
		case message.Version != 1 || message.Transport != s.key:
			reply.Error = "recovery protocol/transport mismatch"
		case message.Request == nil:
			reply.Status = s.status()
		default:
			r := *message.Request
			validation := r
			if validation.Event == "leave" {
				validation.Event = ""
			}
			owner := s.owners[r.Client]
			m := s.model.members[r.Client]
			switch {
			case !validRecoveryRequest(validation):
				reply.Error = "invalid recovery identity"
			case peer.client != "" && peer.client != r.Client:
				reply.Error = "connection client changed"
			case m != nil && (m.identity.Token != r.Token || m.identity.Session != r.Session || m.identity.SessionID != r.SessionID):
				reply.Error = "recovery client identity changed"
			case owner != nil && owner != peer && (owner.conn != nil || owner.pid != peer.pid || !recoveryPeerAlive(owner.pidfd)):
				reply.Error = "client belongs to another live process/connection"
			default:
				if owner != nil && owner != peer {
					owner.pidfd.Close()
					s.owners[r.Client] = peer
					peer.client = r.Client
				}
				if r.Event == "leave" {
					s.model.remove(r.Client)
					delete(s.owners, r.Client)
					peer.client = ""
				} else {
					reply.Reply, err = s.model.exchange(r, time.Now())
					if err != nil {
						reply.Error = err.Error()
					} else {
						peer.client = r.Client
						s.owners[r.Client] = peer
					}
				}
			}
		}
		reply.Reply.Reason = recoveryDisplayReason(reply.Reply.Reason)
		s.mu.Unlock()
		if writeRecoveryFrame(conn, reply) != nil || reply.Error != "" {
			return
		}
	}
}

// RunSessionRecovery owns an inherited, lifetime-held startup lock. Only this
// process runs host probes; no client status call waits for those probes.
// The service exits after its last process-bound member leaves. ready is a
// startup receipt pipe, not an attachment/readiness signal.
func RunSessionRecovery(ctx context.Context, remote RemoteConfig, runtime, self string, lock, ready *os.File) (result error) {
	if lock != nil {
		defer lock.Close()
	}
	if ready != nil {
		info, err := ready.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			ready.Close()
			return fmt.Errorf("recovery startup receipt must be a pipe")
		}
		unix.CloseOnExec(int(ready.Fd()))
	}
	announced := false
	announce := func(err error) {
		if announced {
			return
		}
		announced = true
		if ready == nil {
			return
		}
		status := struct{ Error string }{}
		if err != nil {
			status.Error = recoveryDisplayReason(err.Error())
		}
		_ = ready.SetWriteDeadline(time.Now().Add(time.Second))
		_ = json.NewEncoder(ready).Encode(status)
		_ = ready.Close()
	}
	defer func() { announce(result) }()
	paths, err := openRecoveryPaths(remote, runtime)
	if err != nil {
		return err
	}
	defer paths.close()
	if _, err := PlanSessionCatalog(remote); err != nil {
		return err
	}
	if err := paths.verifyLock(lock); err != nil {
		return err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	var previous unix.Stat_t
	if err := unix.Fstatat(int(paths.dir.Fd()), "control.sock", &previous, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if previous.Mode&unix.S_IFMT != unix.S_IFSOCK || previous.Uid != uint32(os.Getuid()) {
			return fmt.Errorf("refusing non-owned recovery socket")
		}
		conn, err := net.DialTimeout("unix", paths.socket, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return fmt.Errorf("refusing to replace a listening recovery socket")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := unix.Unlinkat(int(paths.dir.Fd()), "control.sock", 0); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: paths.socket, Net: "unix"})
	if err != nil {
		return err
	}
	listener.SetUnlinkOnClose(false)
	var bound unix.Stat_t
	_ = unix.Fstatat(int(paths.dir.Fd()), "control.sock", &bound, unix.AT_SYMLINK_NOFOLLOW)
	defer func() {
		listener.Close()
		var current unix.Stat_t
		if unix.Fstatat(int(paths.dir.Fd()), "control.sock", &current, unix.AT_SYMLINK_NOFOLLOW) == nil && bound.Dev == current.Dev && bound.Ino == current.Ino {
			_ = unix.Unlinkat(int(paths.dir.Fd()), "control.sock", 0)
		}
	}()
	if err := os.Chmod(paths.socket, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &recoveryServer{model: newSessionRecovery(), connections: map[*net.UnixConn]*recoveryConnection{}, owners: map[string]*recoveryConnection{}, key: paths.key}
	var handlers, probes sync.WaitGroup
	acceptDone := make(chan struct{})
	defer func() {
		cancel()
		listener.Close()
		<-acceptDone
		s.mu.Lock()
		for conn := range s.connections {
			conn.Close()
		}
		s.mu.Unlock()
		handlers.Wait()
		for _, peer := range s.owners {
			peer.pidfd.Close()
		}
		probes.Wait() // retain the election lock until probes stop
	}()
	accepted := make(chan error, 1)
	slots := make(chan struct{}, 128)
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				accepted <- err
				return
			}
			select {
			case slots <- struct{}{}:
				handlers.Add(1)
				go func() { defer handlers.Done(); defer func() { <-slots }(); s.serve(ctx, conn) }()
			default:
				conn.Close()
			}
		}
	}()
	announce(nil)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	idleSince := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-accepted:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case now := <-ticker.C:
			s.mu.Lock()
			for _, peer := range s.connections {
				if !recoveryPeerAlive(peer.pidfd) {
					peer.conn.Close()
				}
			}
			for client, peer := range s.owners {
				if peer.conn == nil && !recoveryPeerAlive(peer.pidfd) {
					peer.pidfd.Close()
					delete(s.owners, client)
					s.model.remove(client)
				}
			}
			s.model.admit(now)
			sequence, probe := s.model.beginProbe(now)
			if len(s.model.members) > 0 {
				idleSince = time.Time{}
			} else if idleSince.IsZero() {
				idleSince = now
			}
			idle := !idleSince.IsZero() && now.Sub(idleSince) >= 2*time.Second
			s.mu.Unlock()
			if idle {
				return nil
			}
			if probe {
				probes.Add(1)
				go func(sequence uint64) {
					defer probes.Done()
					probeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
					defer stop()
					inventory, err := runRecoveryProbe(probeCtx, remote, paths.runtime, self, lock)
					if probeCtx.Err() != nil {
						err = probeCtx.Err()
					}
					s.mu.Lock()
					s.model.finishProbe(sequence, time.Now(), inventory, err)
					s.mu.Unlock()
				}(sequence)
			}
		}
	}
}
