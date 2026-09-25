package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func recoveryTestArg(name string) string {
	for i, arg := range os.Args {
		if arg == name && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}
func recoveryTestAppend(path, text string) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		_, _ = f.WriteString(text + "\n")
		f.Close()
	}
}
func TestSessionRecoveryDaemonProcess(t *testing.T) {
	root := os.Getenv("REDEEM_TEST_RECOVERY")
	if root == "" || recoveryTestArg("--lock-fd") == "" {
		return
	}
	var remote RemoteConfig
	if json.Unmarshal([]byte(recoveryTestArg("--remote-json")), &remote) != nil {
		os.Exit(2)
	}
	if recoveryTestArg("--probe-parent-fd") != "" {
		err := RunSessionRecoveryProbe(context.Background(), remote, recoveryTestArg("--runtime-dir"), os.NewFile(3, "lock"), os.NewFile(4, "parent"), os.NewFile(5, "cancel"), os.Stdout)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	recoveryTestAppend(filepath.Join(root, "daemons"), strconv.Itoa(os.Getpid()))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	err := RunSessionRecovery(ctx, remote, recoveryTestArg("--runtime-dir"), filepath.Join(root, "self"), os.NewFile(3, "lock"), os.NewFile(4, "ready"))
	if err != nil && ctx.Err() == nil {
		recoveryTestAppend(filepath.Join(root, "daemon-errors"), err.Error())
		os.Exit(1)
	}
	recoveryTestAppend(filepath.Join(root, "daemon-exits"), strconv.Itoa(os.Getpid()))
	os.Exit(0)
}
func TestSessionRecoveryProbeProcess(t *testing.T) {
	root := os.Getenv("REDEEM_TEST_RECOVERY")
	if root == "" || os.Getenv("REDEEM_TEST_PROBE") != "1" {
		return
	}
	recoveryTestAppend(filepath.Join(root, "probes"), strconv.Itoa(os.Getpid()))
	for {
		if _, e := os.Stat(filepath.Join(root, "release-probe")); e == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var requests []SessionControlRequest
	if _, e := os.Stat(filepath.Join(root, "empty-inventory")); os.IsNotExist(e) {
		for i := 0; i < 30; i++ {
			requests = append(requests, recoveryRequest(i))
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(recoveryInventory(time.Now(), requests...))
	os.Exit(0)
}
func TestSessionRecoveryClientProcess(t *testing.T) {
	root := os.Getenv("REDEEM_TEST_RECOVERY")
	index, err := strconv.Atoi(os.Getenv("REDEEM_TEST_CLIENT"))
	if root == "" || err != nil {
		return
	}
	client := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root, SelfCommand: filepath.Join(root, "self")}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	r := recoveryRequest(index)
	retrySent := false
	for ctx.Err() == nil {
		if _, e := os.Stat(filepath.Join(root, "retry")); e == nil && !retrySent {
			r.Retry = true
			retrySent = true
		}
		call, cancel := context.WithTimeout(ctx, time.Second)
		reply, err := client.Exchange(call, r)
		cancel()
		if err == nil {
			_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("client-%d", index)), []byte("responsive"), 0600)
			if r.Retry {
				_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("retried-%d", index)), nil, 0600)
			}
			r.Retry = false
			if reply.Ended {
				client.Close()
				os.Exit(0)
			}
			if r.Event == "lost" {
				r.Event = ""
				r.Attempt = fmt.Sprintf("%032x", index+10001)
			}
			if reply.Reset {
				r.State = string(sessionOffline)
				r.Event = "lost"
			}
			if reply.Grant != nil {
				recoveryTestAppend(filepath.Join(root, "grants"), strconv.Itoa(index))
				r.State = string(sessionConnecting)
				if _, e := os.Stat(filepath.Join(root, "ready")); e == nil {
					r.State = string(sessionReady)
					r.Event = "ready"
				}
			} else if r.State == string(sessionReady) {
				r.Event = ""
			}
		} else {
			_ = os.WriteFile(filepath.Join(root, fmt.Sprintf("client-error-%d", index)), []byte(err.Error()), 0600)
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
	client.Close()
	os.Exit(0)
}
func recoveryFixtureRemote(root string) RemoteConfig {
	return RemoteConfig{Host: "hermetic-host", SSHCommand: filepath.Join(root, "ssh"), SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
}
func recoveryRuntimeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REDEEM_TEST_RECOVERY", root)
	t.Setenv("XDG_RUNTIME_DIR", root)
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	self := "#!/bin/sh\nexec " + ShellQuote(os.Args[0]) + " -test.run '^TestSessionRecoveryDaemonProcess$' -- \"$@\" 2>>" + ShellQuote(filepath.Join(root, "daemon.stderr")) + "\n"
	ssh := "#!/bin/sh\nexport REDEEM_TEST_PROBE=1\nexec " + ShellQuote(os.Args[0]) + " -test.run '^TestSessionRecoveryProbeProcess$' -- \"$@\"\n"
	for name, text := range map[string]string{"self": self, "ssh": ssh} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(text), 0700); e != nil {
			t.Fatal(e)
		}
	}
	return root
}
func recoveryFileLines(root, name string) int {
	b, _ := os.ReadFile(filepath.Join(root, name))
	return len(strings.Fields(string(b)))
}
func recoveryStatus(t *testing.T, c *SessionRecoveryClient) (SessionRecoveryStatus, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, found, err := c.ExistingStatus(ctx)
	if err != nil {
		t.Logf("status during transition: %v", err)
	}
	return status, found
}
func TestSessionRecoveryThirtyProcessesShareBlockedProbe(t *testing.T) {
	root := recoveryRuntimeFixture(t)
	type child struct {
		cmd  *exec.Cmd
		done chan error
	}
	var children []child
	t.Cleanup(func() {
		for _, c := range children {
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
		}
		for _, c := range children {
			select {
			case err := <-c.done:
				if err != nil {
					t.Errorf("client exited: %v", err)
				}
			case <-time.After(3 * time.Second):
				_ = c.cmd.Process.Kill()
				t.Error("client did not stop")
			}
		}
	})
	for i := 0; i < 30; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSessionRecoveryClientProcess$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("REDEEM_TEST_CLIENT=%d", i))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait(); close(done) }()
		children = append(children, child{cmd, done})
	}
	statusClient := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root}
	defer statusClient.Close()
	awaitSession(t, func() bool { s, ok := recoveryStatus(t, statusClient); return ok && s.Members == 30 && s.Checking })
	if got := recoveryFileLines(root, "daemons"); got != 1 {
		t.Fatalf("startup created %d daemons", got)
	}
	foreign := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root}
	attack, stopAttack := context.WithTimeout(context.Background(), time.Second)
	_, attackErr := foreign.Exchange(attack, recoveryRequest(0))
	stopAttack()
	foreign.Close()
	if attackErr == nil {
		t.Fatal("unrelated process adopted another client's identity")
	}
	awaitSession(t, func() bool { return recoveryFileLines(root, "probes") == 1 })
	if err := os.WriteFile(filepath.Join(root, "retry"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool {
		for i := 0; i < 30; i++ {
			if _, e := os.Stat(filepath.Join(root, fmt.Sprintf("retried-%d", i))); e != nil {
				return false
			}
		}
		return true
	})
	if got := recoveryFileLines(root, "probes"); got != 1 {
		t.Fatalf("blocked check multiplied into %d probes", got)
	}
	// The common discovery seam used by follow must not add a snapshot SSH
	// command while the shared catalog check is blocked/offline.
	query, cancel := context.WithTimeout(context.Background(), time.Second)
	_, discoveryErr := AcquireRemote(query, ExecRunner{}, recoveryFixtureRemote(root))
	cancel()
	if discoveryErr == nil || !strings.Contains(discoveryErr.Error(), "shared host recovery") {
		t.Fatalf("discovery bypassed shared outage: %v", discoveryErr)
	}
	if got := recoveryFileLines(root, "probes"); got != 1 {
		t.Fatalf("discovery launched a competing SSH check: %d", got)
	}
	if err := os.WriteFile(filepath.Join(root, "release-probe"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool { s, ok := recoveryStatus(t, statusClient); return ok && s.Available && s.Pending == 2 })
	time.Sleep(200 * time.Millisecond)
	awaitSession(t, func() bool {
		s, ok := recoveryStatus(t, statusClient)
		if !ok {
			return false
		} // a dropped status connection is not a zero-valued observation
		if s.Pending != 2 || s.Ready != 0 {
			t.Fatalf("unexpected attachment admission: %+v", s)
		}
		return true
	})
	if err := os.WriteFile(filepath.Join(root, "ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool {
		s, ok := recoveryStatus(t, statusClient)
		if s.Pending > 2 {
			t.Errorf("pending=%d", s.Pending)
		}
		return ok && s.Ready == 30
	})
	grantsBefore := recoveryFileLines(root, "grants")
	// Kill the exact test daemon via its verified pidfd, never a reusable PID.
	if err := unix.PidfdSendSignal(int(statusClient.peer.Fd()), syscall.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool {
		s, ok := recoveryStatus(t, statusClient)
		return ok && recoveryFileLines(root, "daemons") == 2 && s.Ready == 30 && s.Available
	})
	if got := recoveryFileLines(root, "grants"); got != grantsBefore {
		t.Fatalf("restart re-granted healthy transports: %d -> %d", grantsBefore, got)
	}
	for _, c := range children {
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
	}
	awaitSession(t, func() bool { return recoveryFileLines(root, "daemon-exits") == 1 })
	if b, _ := os.ReadFile(filepath.Join(root, "daemon.stderr")); strings.Contains(string(b), "DATA RACE") {
		t.Fatalf("daemon race: %s", b)
	}
}
func TestSessionRecoveryStaleSocketAndIdentityIsolation(t *testing.T) {
	root := recoveryRuntimeFixture(t)
	remote := recoveryFixtureRemote(root)
	paths, err := openRecoveryPaths(remote, root)
	if err != nil {
		t.Fatal(err)
	}
	defer paths.close()
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: paths.socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	client := &SessionRecoveryClient{Remote: remote, SelfCommand: filepath.Join(root, "self"), RuntimeDir: root}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Exchange(ctx, recoveryRequest(0)); err != nil {
		t.Fatal(err)
	}
	client.Close()
	awaitSession(t, func() bool { return recoveryFileLines(root, "daemon-exits") == 1 })
	for _, change := range []func(*RemoteConfig){func(c *RemoteConfig) { c.Host = "other-alias" }, func(c *RemoteConfig) { c.SSHCommand += "-other" }, func(c *RemoteConfig) { c.SSHOptions = []string{"-p", "2222"} }, func(c *RemoteConfig) {
		c.SnapshotCommand = append([]string{"env", "PROFILE=other"}, c.SnapshotCommand...)
	}} {
		other := remote
		change(&other)
		key, err := recoveryIdentity(other)
		if err != nil || key == paths.key {
			t.Fatalf("transport identities merged: %s %v", key, err)
		}
	}
}
