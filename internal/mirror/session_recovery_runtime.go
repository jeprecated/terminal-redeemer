package mirror

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const recoveryFrameLimit = 16 << 10

// Linux 6.5+ asm-generic/socket.h. x/sys v0.27 predates this constant.
const soPeerPIDFD = 77

type SessionRecoveryStatus struct {
	Available, Checking     bool
	RetryAt                 time.Time
	Reason                  string
	Members, Pending, Ready int
}
type recoveryWireRequest struct {
	Version   int
	Transport string
	Request   *SessionControlRequest
}
type recoveryWireReply struct {
	Version   int
	Transport string
	Reply     SessionControlReply
	Status    SessionRecoveryStatus
	Error     string
}

type recoveryPaths struct {
	dir                        *os.File
	socket, lock, key, runtime string
}

func (p *recoveryPaths) close() { p.dir.Close() }
func recoveryIdentity(remote RemoteConfig) (string, error) {
	if err := ValidateDestination(remote.Host); err != nil {
		return "", err
	}
	if strings.TrimSpace(remote.SSHCommand) == "" {
		return "", fmt.Errorf("missing SSH executable")
	}
	remote.SSHOptions = append([]string{}, remote.SSHOptions...)
	data, err := json.Marshal(remote)
	if err != nil {
		return "", err
	}
	if len(data) > 64<<10 {
		return "", fmt.Errorf("recovery transport configuration too large")
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
func openRecoveryPaths(remote RemoteConfig, runtime string) (*recoveryPaths, error) {
	return recoveryPathsAt(remote, runtime, true)
}
func recoveryPathsAt(remote RemoteConfig, runtime string, create bool) (*recoveryPaths, error) {
	key, err := recoveryIdentity(remote)
	if err != nil {
		return nil, err
	}
	if runtime == "" {
		runtime = os.Getenv("XDG_RUNTIME_DIR")
		if runtime == "" {
			runtime = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
		}
	}
	base, err := openSocketDirectory(runtime)
	if err != nil {
		return nil, fmt.Errorf("private recovery runtime: %w", err)
	}
	defer base.Close()
	name := "redeem-recovery-" + key[:32]
	if create {
		if err := unix.Mkdirat(int(base.Fd()), name, 0700); err != nil && err != unix.EEXIST {
			return nil, err
		}
	}
	fd, err := unix.Openat(int(base.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), name)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 {
		dir.Close()
		return nil, fmt.Errorf("unsafe recovery directory")
	}
	root := filepath.Join(runtime, name)
	// Both bind and connect resolve through their own held directory descriptor.
	// Renaming a parent cannot redirect them, and long runtime paths still work.
	socket := fmt.Sprintf("/proc/self/fd/%d/control.sock", fd)
	return &recoveryPaths{dir: dir, socket: socket, lock: filepath.Join(root, "lock"), key: key, runtime: runtime}, nil
}
func (p *recoveryPaths) acquireLock() (*os.File, error) {
	fd, err := unix.Openat(int(p.dir.Fd()), "lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), p.lock)
	if err := p.verifyLock(file); err != nil {
		file.Close()
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func (p *recoveryPaths) verifyLock(file *os.File) error {
	var held, named unix.Stat_t
	if file == nil {
		return fmt.Errorf("missing inherited recovery lock")
	}
	fd := int(file.Fd())
	unix.CloseOnExec(fd)
	if unix.Fstat(fd, &held) != nil || unix.Fstatat(int(p.dir.Fd()), "lock", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || held.Mode&unix.S_IFMT != unix.S_IFREG || held.Uid != uint32(os.Getuid()) || held.Mode&0077 != 0 || held.Dev != named.Dev || held.Ino != named.Ino {
		return fmt.Errorf("invalid inherited recovery lock")
	}
	return nil
}

func readRecoveryFrame(reader *bufio.Reader, target any) error {
	data, err := reader.ReadSlice('\n')
	if err != nil {
		return err
	}
	if len(data) > recoveryFrameLimit {
		return fmt.Errorf("recovery frame exceeds bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing recovery data")
	}
	return nil
}
func writeRecoveryFrame(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data)+1 > recoveryFrameLimit {
		return fmt.Errorf("recovery frame exceeds bound")
	}
	data = append(data, '\n')
	n, err := writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

// SessionRecoveryClient keeps one same-user connection. Closing it retires this
// runtime membership; no desired windows or session identities are persisted.
type SessionRecoveryClient struct {
	Remote                  RemoteConfig
	SelfCommand, RuntimeDir string
	mu                      sync.Mutex
	conn                    *net.UnixConn
	reader                  *bufio.Reader
	peer                    *os.File
	nextStart               time.Time
	member                  *SessionControlRequest
}

// Close retires membership after the caller has reaped its transport. An IO
// timeout only disconnects: pending admission remains reserved until release,
// a reconnect/reap report, or verified process death.
func (c *SessionRecoveryClient) Close() error {
	c.mu.Lock()
	member := c.member
	c.mu.Unlock()
	if member != nil {
		r := *member
		r.Event = "leave"
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _ = c.call(ctx, &r, false)
		cancel()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnect()
	c.member = nil
	return nil
}
func (c *SessionRecoveryClient) disconnect() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	if c.peer != nil {
		c.peer.Close()
		c.peer = nil
	}
}
func (c *SessionRecoveryClient) Exchange(ctx context.Context, r SessionControlRequest) (SessionControlReply, error) {
	reply, err := c.call(ctx, &r, true)
	return reply.Reply, err
}

// ExistingStatus never starts a daemon or runs another host probe. Discovery
// callers can respect an existing outage without creating a second retry loop.
func (c *SessionRecoveryClient) ExistingStatus(ctx context.Context) (SessionRecoveryStatus, bool, error) {
	reply, err := c.call(ctx, nil, false)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		// A starter or orphan-probe watchdog may hold election while no
		// endpoint exists. Discovery must not race its still-owned host work.
		held, lockErr := c.recoveryElectionHeld()
		if held {
			return SessionRecoveryStatus{Checking: true, Reason: "Shared recovery starting or draining a previous check"}, true, nil
		}
		return SessionRecoveryStatus{}, false, lockErr
	}
	return reply.Status, err == nil, err
}
func (c *SessionRecoveryClient) call(ctx context.Context, r *SessionControlRequest, start bool) (recoveryWireReply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var reply recoveryWireReply
	if err := sessionContextError(ctx); err != nil {
		return reply, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return reply, fmt.Errorf("recovery call requires deadline")
	}
	paths, err := recoveryPathsAt(c.Remote, c.RuntimeDir, start)
	if err != nil {
		return reply, err
	}
	defer paths.close()
	if c.conn == nil {
		conn, err := c.dial(ctx, paths.socket)
		if err != nil && start && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)) {
			if time.Now().Before(c.nextStart) {
				return reply, fmt.Errorf("waiting for recovery startup")
			}
			err = c.start(ctx, paths)
			if err == nil {
				conn, err = c.dial(ctx, paths.socket)
			} else {
				c.nextStart = time.Now().Add(time.Second)
			}
		}
		if err != nil {
			return reply, err
		}
		_, peer, err := recoveryPeer(conn)
		if err != nil {
			conn.Close()
			return reply, err
		}
		c.conn, c.peer = conn, peer
		c.reader = bufio.NewReaderSize(conn, recoveryFrameLimit)
	}
	if !recoveryPeerAlive(c.peer) {
		c.disconnect()
		return reply, fmt.Errorf("recovery process exited")
	}
	deadline, _ := ctx.Deadline()
	_ = c.conn.SetDeadline(deadline)
	conn := c.conn
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()); close(interrupted) })
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	if r != nil && r.Event != "leave" {
		copy := *r
		c.member = &copy
	}
	err = writeRecoveryFrame(conn, recoveryWireRequest{Version: 1, Transport: paths.key, Request: r})
	if err == nil {
		err = readRecoveryFrame(c.reader, &reply)
	}
	if late := sessionContextError(ctx); late != nil {
		err = late
	}
	if err == nil && (reply.Version != 1 || reply.Transport != paths.key) {
		err = fmt.Errorf("recovery protocol/transport mismatch")
	}
	if err == nil && reply.Error != "" {
		err = fmt.Errorf("recovery: %s", reply.Error)
	}
	if err != nil {
		c.disconnect()
	}
	return reply, err
}
func (c *SessionRecoveryClient) dial(ctx context.Context, path string) (*net.UnixConn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return conn.(*net.UnixConn), nil
}
func (c *SessionRecoveryClient) start(ctx context.Context, p *recoveryPaths) error {
	lock, err := p.acquireLock()
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
		// Another starter/daemon owns the lock. Wait only for its local endpoint;
		// never spawn a contender or run a remote health check from this process.
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				conn, e := c.dial(ctx, p.socket)
				if e == nil {
					conn.Close()
					return nil
				}
			}
		}
	}
	if err != nil {
		return err
	}
	defer lock.Close() // no LOCK_UN: the child inherits ownership
	if c.SelfCommand == "" {
		return fmt.Errorf("recovery requires configured self executable")
	}
	ready, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer ready.Close()
	defer writer.Close()
	payload, _ := json.Marshal(c.Remote)
	cmd := exec.Command(c.SelfCommand, "mirror", "session-recovery", "--remote-json", string(payload), "--runtime-dir", p.runtime, "--lock-fd", "3", "--ready-fd", "4")
	cmd.ExtraFiles = []*os.File{lock, writer}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	writer.Close()
	go func() { _ = cmd.Wait() }()
	deadline, _ := ctx.Deadline()
	_ = ready.SetReadDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { ready.Close() })
	defer stop()
	var status struct{ Error string }
	if err := json.NewDecoder(io.LimitReader(ready, recoveryFrameLimit)).Decode(&status); err != nil {
		return fmt.Errorf("recovery startup: %w", err)
	}
	if status.Error != "" {
		return fmt.Errorf("recovery startup: %s", status.Error)
	}
	return ctx.Err()
}
