package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jmo/terminal-redeemer/internal/bootid"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
	"golang.org/x/sys/unix"
)

// Runs in its own controlling PTY, exactly like the source helper under SSH.
func TestSessionAttachProcessHelper(t *testing.T) {
	value := os.Getenv("REDEEM_TEST_EXACT_ATTACH")
	if value == "" {
		return
	}
	var cfg SessionAttachConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		panic(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	status, err := RunSessionAttachment(ctx, cfg)
	fmt.Print(AttachmentMarker(cfg.Attempt, status))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type attachFixture struct {
	root, base, command string
	env                 []string
}

func realAttachmentFixture(t *testing.T) attachFixture {
	t.Helper()
	command, err := exec.LookPath("zellij")
	if err != nil {
		t.Skip("pinned Zellij unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := zellijlive.VerifyVersion(ctx, command); err != nil {
		t.Skip(err)
	}
	root, err := os.MkdirTemp("", "ra-real-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	config := filepath.Join(root, ".config", "zellij")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := `default_shell "/bin/sh"
session_serialization false
support_kitty_keyboard_protocol false
show_startup_tips false
show_release_notes false
on_force_close "quit"
`
	if err := os.WriteFile(filepath.Join(config, "config.kdl"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	layout := `layout { pane command="/bin/sh"; }`
	if err := os.WriteFile(filepath.Join(root, "layout.kdl"), []byte(layout), 0600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "sockets")
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key == "HOME" || strings.HasPrefix(key, "XDG_") || key == "TERM" || key == "ZELLIJ" || strings.HasPrefix(key, "ZELLIJ_") || key == "REDEEM_TEST_EXACT_ATTACH" {
			continue
		}
		env = append(env, value)
	}
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	env = append(env, "HOME="+root, "XDG_RUNTIME_DIR="+runtime, "XDG_CONFIG_HOME="+filepath.Join(root, ".config"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"), "XDG_DATA_HOME="+filepath.Join(root, "data"), "ZELLIJ_SOCKET_DIR="+base, "TERM=xterm-256color")
	return attachFixture{root: root, base: base, command: command, env: env}
}
func (f attachFixture) run(t *testing.T, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.command, args...)
	cmd.Env = f.env
	cmd.WaitDelay = 200 * time.Millisecond
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated Zellij %q: %v\n%s", args, err, output)
	}
	return output
}
func (f attachFixture) create(t *testing.T, name string) string {
	t.Helper()
	f.run(t, "--layout", filepath.Join(f.root, "layout.kdl"), "attach", "--create-background", "--", name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, f.command, "kill-session", "--", name)
		cmd.Env = f.env
		cmd.WaitDelay = 200 * time.Millisecond
		_ = cmd.Run()
	})
	boot, err := bootid.Current()
	if err != nil {
		t.Fatal(err)
	}
	id, err := zellijlive.ExactSocketIDAt(unix.AT_FDCWD, filepath.Join(f.base, zellijlive.SocketContractDir, name), boot, name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type attachmentPTY struct {
	file    *os.File
	mu      sync.Mutex
	output  bytes.Buffer
	changed chan struct{}
	exited  chan error
	done    chan struct{}
	process *os.Process
}

func (f attachFixture) attach(t *testing.T, name, id string) *attachmentPTY {
	t.Helper()
	config := SessionAttachConfig{Command: f.command, SocketBase: f.base, Session: name, SessionID: id, Attempt: testAttachmentAttempt, StartupTimeout: 8 * time.Second}
	payload, _ := json.Marshal(config)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSessionAttachProcessHelper$")
	cmd.Env = append(append([]string{}, f.env...), "REDEEM_TEST_EXACT_ATTACH="+string(payload), "ZELLIJ=1", "ZELLIJ_SESSION_NAME=wrong", "ZELLIJ_PANE_ID=99")
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetNonblock(int(file.Fd()), true); err != nil {
		t.Fatal(err)
	}
	p := &attachmentPTY{file: file, changed: make(chan struct{}, 1), exited: make(chan error, 1), done: make(chan struct{}), process: cmd.Process}
	go func() { p.exited <- cmd.Wait(); close(p.done) }()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 8192)
		for {
			n, err := file.Read(buf)
			if n > 0 {
				p.mu.Lock()
				if p.output.Len() < 2<<20 {
					p.output.Write(buf[:n])
				}
				p.mu.Unlock()
				select {
				case p.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
		}
		file.Close()
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			t.Error("attachment child was not reaped")
		}
		select {
		case <-readDone:
		case <-time.After(2 * time.Second):
			t.Error("PTY reader failed to stop")
		}
	})
	return p
}
func (p *attachmentPTY) text() string { p.mu.Lock(); defer p.mu.Unlock(); return p.output.String() }
func (p *attachmentPTY) waitText(t *testing.T, text string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		if strings.Contains(p.text(), text) {
			return
		}
		select {
		case <-p.changed:
		case <-deadline.C:
			t.Fatalf("missing %q in PTY output:\n%q", text, p.text())
		}
	}
}

func TestRealPinnedZellijExactAttachmentReadiness(t *testing.T) {
	f := realAttachmentFixture(t)
	for _, name := range []string{"Case Sensitive", "-Leading"} {
		t.Run(name, func(t *testing.T) {
			id := f.create(t, name)
			p := f.attach(t, name, id)
			p.waitText(t, AttachmentMarker(testAttachmentAttempt, "ready"))
			// Real input after the client-rendered marker reaches the bound session.
			target := filepath.Join(f.root, strings.ReplaceAll(name, " ", "_")+"-input")
			io.WriteString(p.file, "printf 'verified-input' > "+QuoteCommand([]string{target})+"\r")
			// A shell-produced marker is a deterministic barrier after that write.
			io.WriteString(p.file, "printf '\\166\\145\\162\\151\\146\\151\\145\\144\\055\\142\\141\\162\\162\\151\\145\\162\\n'\r")
			p.waitText(t, "verified-barrier")
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "verified-input" {
				t.Fatalf("input missing from bound shell: %q %v", data, err)
			}
			// Default Zellij Ctrl-o, d deliberately detaches this client.
			p.file.Write([]byte{15, 'd'})
			p.waitText(t, AttachmentMarker(testAttachmentAttempt, "detached"))
			select {
			case err := <-p.exited:
				if err != nil {
					t.Fatalf("helper exit: %v\n%q", err, p.text())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("detach did not reap helper")
			}
			listed := f.run(t, "list-sessions", "--short", "--no-formatting")
			if !strings.Contains(string(listed), name) {
				t.Fatalf("detach destroyed source session: %q", listed)
			}
		})
	}
}

func TestRealPinnedZellijCancelledAttachmentPreservesSession(t *testing.T) {
	f := realAttachmentFixture(t)
	id := f.create(t, "cancelled")
	p := f.attach(t, "cancelled", id)
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "ready"))
	if err := p.process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "cancelled"))
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled helper was not reaped")
	}
	listed := f.run(t, "list-sessions", "--short", "--no-formatting")
	if !strings.Contains(string(listed), "cancelled") {
		t.Fatalf("cancellation killed source: %q", listed)
	}
	entries, err := os.ReadDir(f.base)
	if err != nil || len(entries) != 1 {
		t.Fatalf("attempt cleanup failed: %v %v", entries, err)
	}
}

func TestRealPinnedZellijReplacementDuringAttachmentStaysPinned(t *testing.T) {
	f := realAttachmentFixture(t)
	f.env = append(f.env, "REDEEM_TEST_GENERATION=original")
	id := f.create(t, "same")
	// Keep the original server alive under a different filesystem name. Its
	// pinned inode must remain the target when a new same-name server appears.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, f.command, "kill-session", "retired")
		cmd.Env = f.env
		cmd.WaitDelay = 200 * time.Millisecond
		_ = cmd.Run()
	})
	gate := filepath.Join(f.root, "release")
	wrapper := filepath.Join(f.root, "gated-zellij")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then exec " + QuoteCommand([]string{f.command}) + " \"$@\"; fi\nprintf 'fixture-attachment-paused\\n'\nwhile [ ! -f " + QuoteCommand([]string{gate}) + " ]; do sleep 0.01; done\nexec " + QuoteCommand([]string{f.command}) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	clientFixture := f
	clientFixture.command = wrapper
	p := clientFixture.attach(t, "same", id)
	p.waitText(t, "fixture-attachment-paused")
	if strings.Contains(p.text(), AttachmentMarker(testAttachmentAttempt, "ready")) {
		t.Fatal("startup output marked ready")
	}
	if err := os.Rename(filepath.Join(f.base, zellijlive.SocketContractDir, "same"), filepath.Join(f.base, zellijlive.SocketContractDir, "retired")); err != nil {
		t.Fatal(err)
	}
	f.env = append(f.env, "REDEEM_TEST_GENERATION=replacement")
	f.create(t, "same")
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "ready"))
	target := filepath.Join(f.root, "received-generation")
	io.WriteString(p.file, "printf '%s' \"$REDEEM_TEST_GENERATION\" > "+QuoteCommand([]string{target})+"\rprintf '\\160\\151\\156\\156\\145\\144\\055\\142\\141\\162\\162\\151\\145\\162\\n'\r")
	p.waitText(t, "pinned-barrier")
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("replacement received attachment input: %q %v", data, err)
	}
	p.file.Write([]byte{15, 'd'})
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "detached"))
}

func TestRealPinnedZellijReplacementNeverReceivesInput(t *testing.T) {
	f := realAttachmentFixture(t)
	oldID := f.create(t, "replace")
	f.run(t, "kill-session", "replace")
	// kill-session returns before the server has removed its socket. Wait for
	// observed removal, not a guessed sleep, before asking Zellij to create.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(f.base, zellijlive.SocketContractDir, "replace")); os.IsNotExist(err) {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("isolated source socket was not removed")
		}
	}
	newID := f.create(t, "replace")
	if oldID == newID {
		t.Fatal("replacement reused identity")
	}
	p := f.attach(t, "replace", oldID)
	io.WriteString(p.file, "echo MUST_NOT_REACH_SHELL\r")
	p.waitText(t, AttachmentMarker(testAttachmentAttempt, "replaced"))
	if strings.Contains(p.text(), AttachmentMarker(testAttachmentAttempt, "ready")) {
		t.Fatal("replacement marked ready")
	}
}
