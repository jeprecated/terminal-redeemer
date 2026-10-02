package mirror

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
	"golang.org/x/sys/unix"
)

type sessionControlFunc func(context.Context, SessionControlRequest) (SessionControlReply, error)

func (f sessionControlFunc) Exchange(ctx context.Context, r SessionControlRequest) (SessionControlReply, error) {
	return f(ctx, r)
}

func TestSessionSupervisorSignalProcess(t *testing.T) {
	root := os.Getenv("REDEEM_TEST_SESSION_SIGNAL")
	if root == "" {
		return
	}
	fd := int(os.Stdin.Fd())
	original, _ := unix.IoctlGetTermios(fd, terminalGetState)
	flags, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	control := &testSessionControl{permits: 1}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	cfg := SessionSupervisorConfig{Remote: RemoteConfig{Host: "isolated", SSHCommand: filepath.Join(root, "ssh"), SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}, Session: "original", SessionID: zellijlive.SessionID("boot", "original", 1, 1), Token: "projection", Input: os.Stdin, Output: os.Stdout}
	cfg.Control = sessionControlFunc(func(ctx context.Context, r SessionControlRequest) (SessionControlReply, error) {
		reply, err := control.Exchange(ctx, r)
		if reply.Grant != nil {
			_ = os.WriteFile(filepath.Join(root, reply.Grant.Attempt+".ready"), nil, 0600)
		}
		if r.Event == "ready" {
			_ = os.WriteFile(filepath.Join(root, "supervisor-ready"), []byte(r.Attempt), 0600)
		}
		return reply, err
	})
	_ = RunSessionSupervisor(ctx, cfg)
	current, termErr := unix.IoctlGetTermios(fd, terminalGetState)
	currentFlags, _ := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if (termErr == nil && !terminalStateEqual(original, current)) || !terminalFlagsEqual(flags, currentFlags) {
		_ = os.WriteFile(filepath.Join(root, "restore-error"), []byte(fmt.Sprintf("termios: %+v -> %+v; flags: %#x -> %#x", original, current, flags, currentFlags)), 0600)
		os.Exit(8)
	}
	os.Exit(0)
}

func TestSessionSupervisorSignalAndTerminalCloseReapChild(t *testing.T) {
	for _, how := range []string{"signal", "terminal-close"} {
		t.Run(how, func(t *testing.T) {
			root := t.TempDir()
			wrapper := "#!/bin/sh\nexec " + ShellQuote(os.Args[0]) + " -test.run '^TestSessionTransportProcess$' -- \"$@\"\n"
			if err := os.WriteFile(filepath.Join(root, "ssh"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestSessionSupervisorSignalProcess$")
			cmd.Env = append(os.Environ(), "REDEEM_TEST_SESSION_SIGNAL="+root, "REDEEM_TEST_SESSION_TRANSPORT="+root, "GORACE=atexit_sleep_ms=0")
			terminal, err := pty.Start(cmd)
			if err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait(); close(exited) }()
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				terminal.Close()
				select {
				case <-exited:
				case <-time.After(3 * time.Second):
					t.Error("signal fixture did not exit")
				}
			})
			var attempt string
			awaitSession(t, func() bool {
				b, e := os.ReadFile(filepath.Join(root, "supervisor-ready"))
				attempt = string(b)
				return e == nil && validAttachmentAttempt(attempt)
			})
			b, err := os.ReadFile(filepath.Join(root, attempt+".pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			if how == "signal" {
				err = cmd.Process.Signal(syscall.SIGTERM)
			} else {
				err = terminal.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-exited:
				if err != nil {
					message, _ := os.ReadFile(filepath.Join(root, "restore-error"))
					t.Fatalf("helper cleanup/restore: %v: %s", err, message)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("helper ignored terminal close/signal")
			}
			if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
				t.Fatal(fmt.Sprintf("transport PID %d not reaped: %v", pid, err))
			}
		})
	}
}
