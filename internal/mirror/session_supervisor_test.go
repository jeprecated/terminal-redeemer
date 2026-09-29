package mirror

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
	"golang.org/x/sys/unix"
)

// A real PTY-consuming process standing in for SSH. Files gate readiness and
// exit; recorded input is independent of terminal rendering/echo.
func TestSessionTransportProcess(t *testing.T) {
	root := os.Getenv("REDEEM_TEST_SESSION_TRANSPORT")
	if root == "" {
		return
	}
	parts := strings.Fields(os.Args[len(os.Args)-1])
	attempt := strings.Trim(parts[len(parts)-1], "'")
	if !validAttachmentAttempt(attempt) {
		os.Exit(2)
	}
	fd := int(os.Stdin.Fd())
	_, err := term.MakeRaw(uintptr(fd))
	if err != nil {
		os.Exit(3)
	}
	path := filepath.Join(root, attempt)
	_ = os.WriteFile(path+".pid", []byte(fmt.Sprint(os.Getpid())), 0600)
	_ = unix.SetNonblock(fd, true)
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	ready := false
	buf := make([]byte, 4096)
	for {
		if _, err := os.Stat(path + ".exit"); err == nil {
			os.Exit(0)
		} // Zero is deliberately NOT detach.
		if _, err := os.Stat(path + ".detach"); err == nil {
			fmt.Print(AttachmentMarker(attempt, "detached"))
			os.Exit(0)
		}
		if !ready {
			if b, err := os.ReadFile(path + ".stale"); err == nil {
				stale := string(b)
				if stale == "" {
					stale = strings.Repeat("f", 32)
				}
				fmt.Print(AttachmentMarker(stale, "ready"))
				_ = os.Remove(path + ".stale")
			}
			if _, err := os.Stat(path + ".ready"); err == nil {
				if b, err := os.ReadFile(path + ".prelude"); err == nil {
					fmt.Print(string(b)) // pre-ready output, e.g. a stale terminal query
				}
				fmt.Print(AttachmentMarker(attempt, "ready"))
				ready = true
			}
		} else if b, err := os.ReadFile(path + ".live"); err == nil {
			fmt.Print(string(b))
			_ = os.Remove(path + ".live")
		}
		select {
		case <-resize:
			if s, e := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); e == nil {
				_ = os.WriteFile(path+".size", []byte(fmt.Sprintf("%dx%d", s.Col, s.Row)), 0600)
			}
		default:
		}
		if _, err := os.Stat(path + ".stall"); err == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		n, err := unix.Read(fd, buf)
		if n > 0 {
			f, e := os.OpenFile(path+".input", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if e != nil {
				os.Exit(4)
			}
			_, _ = f.Write(buf[:n])
			_ = f.Close()
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			os.Exit(5)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type testSessionControl struct {
	mu          sync.Mutex
	permits     int
	attempts    []string
	ready, lost []string
	block       bool
	late, reset bool
	ended       bool
	expired     bool
	stale       string
	active      SessionGrant
	slots       int
	denySlot    bool
	calls       int
	idle        bool // host available, no retry time: plain admission wait
}

func (c *testSessionControl) acquirePendingSlot() (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.denySlot {
		return nil, fmt.Errorf("pending capacity occupied")
	}
	if c.slots != 0 {
		panic("overlapping pending transports")
	}
	c.slots++
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.slots--
		if c.slots < 0 {
			panic("pending slot released twice")
		}
	}, nil
}

func (c *testSessionControl) Exchange(ctx context.Context, r SessionControlRequest) (SessionControlReply, error) {
	c.mu.Lock()
	c.calls++
	blocked, late := c.block, c.late
	if r.Event == "ready" {
		c.ready = append(c.ready, r.Attempt)
	}
	if r.Event == "lost" {
		c.lost = append(c.lost, r.Attempt)
	}
	reply := SessionControlReply{Checking: true, Reason: "Host unavailable", Ended: c.ended, Reset: c.reset}
	if c.idle {
		reply.Checking, reply.Reason = false, "Waiting for attachment admission"
	}
	if c.stale != "" {
		reply.Grant = &SessionGrant{c.stale, time.Now().Add(time.Second)}
	} else if r.Event == "" {
		if c.active.Attempt != r.Attempt && len(c.attempts) < c.permits {
			c.active = SessionGrant{r.Attempt, time.Now().Add(8 * time.Second)}
			if c.expired {
				c.active.Deadline = time.Now().Add(-time.Second)
			}
			c.attempts = append(c.attempts, r.Attempt)
		}
		if c.active.Attempt == r.Attempt {
			g := c.active
			reply.Grant = &g
		}
	}
	c.mu.Unlock()
	if late {
		time.Sleep(1100 * time.Millisecond)
		return SessionControlReply{Ended: true}, nil
	}
	if blocked {
		<-ctx.Done()
		return SessionControlReply{}, ctx.Err()
	}
	return reply, nil
}
func (c *testSessionControl) change(f func()) { c.mu.Lock(); defer c.mu.Unlock(); f() }
func (c *testSessionControl) attempt(t *testing.T, n int) string {
	t.Helper()
	var s string
	awaitSession(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.attempts) >= n {
			s = c.attempts[n-1]
			return true
		}
		return false
	})
	return s
}
func awaitSession(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("session condition did not become true")
}

type testSessionTerminal struct {
	master, slave *os.File
	fd            int
	root          string
	remote        RemoteConfig
	control       *testSessionControl
	cancel        context.CancelFunc
	done          chan error
	mu            sync.Mutex
	text          bytes.Buffer
}

func sessionTerminalFixture(t *testing.T) *testSessionTerminal {
	t.Helper()
	return sessionTerminalFixtureWith(t, nil)
}

// sessionTerminalFixtureWith replaces the scripted control with another seam.
func sessionTerminalFixtureWith(t *testing.T, control SessionControl) *testSessionTerminal {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REDEEM_TEST_SESSION_TRANSPORT", root)
	ssh := filepath.Join(root, "ssh")
	script := "#!/bin/sh\nexec " + ShellQuote(os.Args[0]) + " -test.run '^TestSessionTransportProcess$' -- \"$@\"\n"
	if err := os.WriteFile(ssh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &testSessionTerminal{master: master, slave: slave, fd: int(master.Fd()), root: root, control: &testSessionControl{}, cancel: cancel, done: make(chan error, 1)}
	original, _ := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	originalFlags, _ := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	readerDone := make(chan struct{})
	readCtx, stopRead := context.WithCancel(context.Background())
	_ = unix.SetNonblock(h.fd, true)
	go func() {
		defer close(readerDone)
		buf := make([]byte, 4096)
		for readCtx.Err() == nil {
			n, err := unix.Read(h.fd, buf)
			if n > 0 {
				h.mu.Lock()
				h.text.Write(buf[:n])
				h.mu.Unlock()
			}
			if err != nil && err != unix.EAGAIN && err != unix.EINTR {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	h.remote = RemoteConfig{Host: "isolated", SSHCommand: ssh, SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
	local, err := StartSessionLocal(ctx, h.remote, root, "0123456789abcdef0123456789abcdef")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cfg := SessionSupervisorConfig{Remote: h.remote, Session: "original", SessionID: zellijlive.SessionID("boot", "original", 1, 1), Token: "0123456789abcdef0123456789abcdef", Input: slave, Output: slave, Control: h.control, Local: local}
	if control != nil {
		cfg.Control = control
	}
	go func() { h.done <- RunSessionSupervisor(ctx, cfg); close(h.done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(3 * time.Second):
			t.Error("supervisor did not stop")
		}
		local.Close()
		current, _ := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		currentFlags, _ := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
		if !reflect.DeepEqual(original, current) || originalFlags != currentFlags {
			t.Errorf("terminal not restored: termios=%v flags=%v", reflect.DeepEqual(original, current), originalFlags == currentFlags)
		}
		stopRead()
		<-readerDone
		master.Close()
		slave.Close()
	})
	h.waitText(t, "Enter to retry now")
	return h
}
func (h *testSessionTerminal) waitText(t *testing.T, s string) {
	t.Helper()
	awaitSession(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return strings.Contains(h.text.String(), s) })
}
func (h *testSessionTerminal) write(t *testing.T, s string) {
	t.Helper()
	if err := writeSessionTerminal(context.Background(), h.fd, []byte(s)); err != nil {
		t.Fatal(err)
	}
}
func (h *testSessionTerminal) mark(t *testing.T, attempt, suffix string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, attempt+suffix), nil, 0600); err != nil {
		t.Fatal(err)
	}
}
func (h *testSessionTerminal) received(attempt string) string {
	b, _ := os.ReadFile(filepath.Join(h.root, attempt+".input"))
	return string(b)
}
func (h *testSessionTerminal) ready(t *testing.T, attempt string) {
	t.Helper()
	h.mark(t, attempt, ".ready")
	awaitSession(t, func() bool {
		h.control.mu.Lock()
		defer h.control.mu.Unlock()
		for _, s := range h.control.ready {
			if s == attempt {
				return true
			}
		}
		return false
	})
}

func TestSessionSupervisorPTYDiscardAndRepeatedRecovery(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.block = true })
	h.write(t, "offline command\r"+pasteStart+strings.Repeat("offline-paste\n", 400)+pasteEnd)
	h.waitText(t, "Retry requested") // no grant and deliberately blocked control
	h.control.change(func() { h.control.block = false; h.control.permits = 1 })
	first := h.control.attempt(t, 1)
	awaitSession(t, func() bool { _, e := os.Stat(filepath.Join(h.root, first+".pid")); return e == nil })
	h.write(t, "connecting command\r")
	h.mark(t, first, ".stale")
	h.write(t, pasteStart+"paste-started-offline")
	h.mark(t, first, ".ready")
	time.Sleep(150 * time.Millisecond)
	if got := h.received(first); got != "" {
		t.Fatalf("pre-ready input escaped: %q", got)
	}
	h.write(t, strings.Repeat("late-paste-tail\n", 100)+pasteEnd)
	h.ready(t, first)
	h.write(t, "FRESH-ONE\n")
	awaitSession(t, func() bool { return strings.Contains(h.received(first), "FRESH-ONE\n") })
	if got := h.received(first); got != "FRESH-ONE\n" {
		t.Fatalf("discarded input replayed: %q", got)
	}
	// Resize the physical terminal and notify this in-process supervisor.
	if err := pty.Setsize(h.slave, &pty.Winsize{Rows: 37, Cols: 113}); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	awaitSession(t, func() bool { b, _ := os.ReadFile(filepath.Join(h.root, first+".size")); return string(b) == "113x37" })
	h.mark(t, first, ".exit") // success exit without detach is still loss
	h.waitText(t, "Connection lost")
	h.write(t, "second outage\r"+pasteStart+"no replay"+pasteEnd)
	h.control.change(func() { h.control.permits = 2 })
	second := h.control.attempt(t, 2)
	if first == second {
		t.Fatal("attempt reused")
	}
	h.write(t, "second connecting\r")
	if err := os.WriteFile(filepath.Join(h.root, second+".stale"), []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	h.write(t, "old proof must not open the new gate\r")
	if got := h.received(second); got != "" {
		t.Fatalf("stale readiness enabled input: %q", got)
	}
	h.ready(t, second)
	h.write(t, "FRESH-TWO\n")
	awaitSession(t, func() bool { return strings.Contains(h.received(second), "FRESH-TWO\n") })
	if got := h.received(second); got != "FRESH-TWO\n" {
		t.Fatalf("replacement received stale input: %q", got)
	}
	h.mark(t, second, ".detach")
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("verified detach did not stop helper")
	}
}

func TestSessionSupervisorRejectsExpiredAndStaleGrants(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.permits = 1; h.control.expired = true })
	expired := h.control.attempt(t, 1)
	awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return len(h.control.lost) > 0 })
	h.control.change(func() { h.control.stale = expired })
	time.Sleep(250 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(h.root, expired+".pid")); !os.IsNotExist(err) {
		t.Fatal("expired/replayed grant spawned a transport")
	}
	h.control.change(func() { h.control.stale = ""; h.control.expired = false; h.control.permits = 2 })
	current := h.control.attempt(t, 2)
	h.ready(t, current)
	h.write(t, "fresh\n")
	awaitSession(t, func() bool { return h.received(current) == "fresh\n" })
}

func TestSessionSupervisorIgnoresLateControlAndPreservesReadyOnReset(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.late = true })
	time.Sleep(1500 * time.Millisecond)
	select {
	case err := <-h.done:
		t.Fatalf("late control reply closed helper: %v", err)
	default:
	}
	h.control.change(func() { h.control.late = false; h.control.permits = 1 })
	attempt := h.control.attempt(t, 1)
	h.ready(t, attempt)
	h.control.change(func() { h.control.reset = true })
	time.Sleep(350 * time.Millisecond)
	h.write(t, pasteStart+"fresh paste"+pasteEnd+"fresh typing\n")
	awaitSession(t, func() bool { return h.received(attempt) == pasteStart+"fresh paste"+pasteEnd+"fresh typing\n" })
}

func TestSessionTerminalWriteBackpressureIsBounded(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	_, err = term.MakeRaw(slave.Fd())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = writeSessionTerminal(context.Background(), int(master.Fd()), bytes.Repeat([]byte("x"), 1<<20))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("unbounded write: %v %v", err, time.Since(start))
	}
}

func TestSessionSupervisorStalledPasteStillAcknowledgesRetry(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.permits = 1 })
	attempt := h.control.attempt(t, 1)
	h.mark(t, attempt, ".stall")
	h.ready(t, attempt)
	// Saturate the child's input without holding the physical-terminal reader.
	// Its dedicated writer is bounded, so the helper must remain usable.
	payload := pasteStart + strings.Repeat("x", 128*1024) + pasteEnd
	deadline := time.Now().Add(2 * time.Second)
	for len(payload) > 0 {
		n, err := unix.Write(h.fd, []byte(payload))
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatal("paste producer remained blocked")
		}
		time.Sleep(time.Millisecond)
	}
	h.waitText(t, "Connection lost")
	h.write(t, "\r")
	h.waitText(t, "Retry requested")
	h.control.change(func() { h.control.permits = 2 })
	next := h.control.attempt(t, 2)
	h.ready(t, next)
	h.write(t, "only-fresh\n")
	awaitSession(t, func() bool { return h.received(next) == "only-fresh\n" })
}

func TestSessionOutputHolderProcess(t *testing.T) {
	switch os.Getenv("REDEEM_TEST_OUTPUT_HOLDER") {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestSessionOutputHolderProcess$")
		child.Env = append(os.Environ(), "REDEEM_TEST_OUTPUT_HOLDER=child")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if child.Start() != nil {
			os.Exit(2)
		}
		os.Exit(0)
	case "child":
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}
}

func TestSessionTransportBoundsDescendantOutputDrain(t *testing.T) {
	t.Setenv("REDEEM_TEST_OUTPUT_HOLDER", "parent")
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	start := time.Now()
	child, err := startSessionTransport(context.Background(), Command{Name: os.Args[0], Args: []string{"-test.run=^TestSessionOutputHolderProcess$"}}, testAttachmentAttempt, int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer child.input.Close()
	defer child.stop()
	select {
	case result := <-child.done:
		if result.err == nil || !strings.Contains(result.err.Error(), "drain timed out") {
			t.Fatalf("unbounded/misclassified drain: %+v", result)
		}
		if time.Since(start) > 1800*time.Millisecond {
			t.Fatal("waited for escaped descendant")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("descriptor holder stranded helper")
	}
}

func TestSessionInputPasteKeepsOriginAcrossEverySplit(t *testing.T) {
	paste := pasteStart + "old\r\nbytes" + pasteEnd
	for split := 1; split <= len(paste); split++ {
		g := sessionInputGate{}
		items := g.decode([]byte(paste[:split]))
		g.generation = 1
		items = append(items, g.decode([]byte(paste[split:]+"fresh"))...)
		var old, fresh strings.Builder
		for _, item := range items {
			if item.generation == 0 {
				old.Write(item.data)
			} else {
				fresh.Write(item.data)
			}
		}
		if old.String() != paste || fresh.String() != "fresh" {
			t.Fatalf("split %d: old=%q fresh=%q", split, old.String(), fresh.String())
		}
	}
}

func TestSessionSupervisorForwardsLoneEscapeWhenReady(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.write(t, "\x1b")
	h.control.change(func() { h.control.permits = 1 })
	attempt := h.control.attempt(t, 1)
	h.ready(t, attempt)
	h.write(t, "\x1b")
	awaitSession(t, func() bool { return h.received(attempt) == "\x1b" })
	time.Sleep(100 * time.Millisecond)
	h.write(t, "j")
	awaitSession(t, func() bool { return h.received(attempt) == "\x1bj" })
}

func TestSessionSupervisorStatusScreenRedrawsOnlyOnChange(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.block = true })
	h.write(t, "\r")
	h.waitText(t, "Retry requested")
	h.mu.Lock()
	before := strings.Count(h.text.String(), "Enter to retry now")
	h.mu.Unlock()
	time.Sleep(700 * time.Millisecond) // several 250ms ticks, unchanged status
	h.mu.Lock()
	text := h.text.String()
	h.mu.Unlock()
	if after := strings.Count(text, "Enter to retry now"); after != before {
		t.Fatalf("unchanged status redrawn %d times", after-before)
	}
	if !strings.Contains(text, "\x1b[?1003l\x1b[?1006l") || !strings.Contains(text, "\x1b[<u") || !strings.Contains(text, "\x1b[?25h") {
		t.Fatalf("status screen does not reset leftover reporting modes: %q", text)
	}
}

func TestSessionSupervisorPollsAdmissionFasterThanTick(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.calls = 0 })
	time.Sleep(400 * time.Millisecond)
	var calls int
	h.control.change(func() { calls = h.control.calls })
	if calls < 3 {
		t.Fatalf("checking host polled %d times in 400ms", calls)
	}
	h.control.change(func() { h.control.permits = 1 })
	first := h.control.attempt(t, 1)
	h.ready(t, first)
	h.control.change(func() { h.control.calls = 0 })
	time.Sleep(400 * time.Millisecond)
	h.control.change(func() { calls = h.control.calls })
	if calls > 3 {
		t.Fatalf("ready attachment kept fast polling: %d calls in 400ms", calls)
	}
}

func TestSessionSupervisorPlainAdmissionWaitKeepsTickRate(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.idle = true })
	time.Sleep(300 * time.Millisecond) // let an idle reply become current
	var calls int
	h.control.change(func() { h.control.calls = 0 })
	time.Sleep(400 * time.Millisecond)
	h.control.change(func() { calls = h.control.calls })
	if calls > 3 {
		t.Fatalf("plain admission wait polled %d times in 400ms", calls)
	}
}

func TestSessionSupervisorStripsPreReadyQueriesOnly(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.permits = 1 })
	attempt := h.control.attempt(t, 1)
	if err := os.WriteFile(filepath.Join(h.root, attempt+".prelude"), []byte("\x1b]11;?\x1b\\\x1b[6nprelude-frame"), 0600); err != nil {
		t.Fatal(err)
	}
	h.ready(t, attempt)
	h.waitText(t, "prelude-frame")
	if err := os.WriteFile(filepath.Join(h.root, attempt+".live"), []byte("\x1b[6nlive-frame"), 0600); err != nil {
		t.Fatal(err)
	}
	h.waitText(t, "live-frame")
	h.mu.Lock()
	text := h.text.String()
	h.mu.Unlock()
	if strings.Contains(text, "\x1b]11;?") || strings.Contains(text, "\x1b[6nprelude-frame") {
		t.Fatalf("pre-ready query reached the physical terminal: %q", text)
	}
	if !strings.Contains(text, "\x1b[6nlive-frame") {
		t.Fatalf("live post-ready output was filtered: %q", text)
	}
}
