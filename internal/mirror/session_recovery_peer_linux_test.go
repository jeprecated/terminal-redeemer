package mirror

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSessionRecoveryClosedPIDFDIsNotLive(t *testing.T) {
	fd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "self-pidfd")
	defer file.Close()
	if !recoveryPeerAlive(file) {
		t.Fatal("live self pidfd rejected")
	}
	file.Close()
	if recoveryPeerAlive(file) || recoveryPeerAlive(nil) {
		t.Fatal("closed descriptor treated as live process")
	}
}

func TestSessionRecoverySocketOwnerProcess(t *testing.T) {
	root := os.Getenv("REDEEM_TEST_RECOVERY")
	switch os.Getenv("REDEEM_TEST_RETAINED") {
	case "child":
		time.Sleep(3 * time.Second)
		os.Exit(0)
	case "parent":
		c := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), SelfCommand: filepath.Join(root, "self"), RuntimeDir: root}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := c.Exchange(ctx, recoveryRequest(0)); err != nil {
			os.Exit(2)
		}
		file, err := c.conn.File()
		if err != nil {
			os.Exit(3)
		}
		holder := exec.Command(os.Args[0], "-test.run=^TestSessionRecoverySocketOwnerProcess$")
		holder.Env = append(os.Environ(), "REDEEM_TEST_RETAINED=child")
		holder.ExtraFiles = []*os.File{file}
		holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if holder.Start() != nil {
			os.Exit(4)
		}
		for {
			if _, err := os.Stat(filepath.Join(root, "release-owner")); err == nil {
				os.Exit(0)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
func TestSessionRecoveryRejectsDeadPeerWithRetainedSocket(t *testing.T) {
	root := recoveryRuntimeFixture(t)
	owner := exec.Command(os.Args[0], "-test.run=^TestSessionRecoverySocketOwnerProcess$")
	owner.Env = append(os.Environ(), "REDEEM_TEST_RETAINED=parent")
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- owner.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = owner.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("socket owner not reaped")
		}
	})
	status := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root}
	defer status.Close()
	awaitSession(t, func() bool { s, ok := recoveryStatus(t, status); return ok && s.Members == 1 })
	if err := os.WriteFile(filepath.Join(root, "release-owner"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not exit")
	}
	deadline := time.Now().Add(time.Second)
	for {
		s, ok := recoveryStatus(t, status)
		if ok && s.Members == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dead process retained authority through inherited socket")
		}
		time.Sleep(5 * time.Millisecond)
	}
	awaitSession(t, func() bool { return recoveryFileLines(root, "daemon-exits") == 1 })
}
