package mirror

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSessionRecoverySlotProcess(t *testing.T) {
	index := os.Getenv("REDEEM_TEST_SLOT")
	if index == "" {
		return
	}
	root := os.Getenv("REDEEM_TEST_RECOVERY")
	c := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root}
	release, err := c.acquirePendingSlot()
	if err != nil {
		os.Exit(2)
	}
	_ = os.WriteFile(filepath.Join(root, "slot-"+index), nil, 0600)
	for {
		if _, err := os.Stat(filepath.Join(root, "release-slots")); err == nil {
			release()
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestSessionRecoveryRestartPreservesProbeAndPendingBounds(t *testing.T) {
	root := recoveryRuntimeFixture(t)
	var holders []*exec.Cmd
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSessionRecoverySlotProcess$")
		cmd.Env = append(os.Environ(), "REDEEM_TEST_SLOT="+strconv.Itoa(i))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		holders = append(holders, cmd)
	}
	t.Cleanup(func() {
		for _, cmd := range holders {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	awaitSession(t, func() bool {
		for i := 0; i < 2; i++ {
			if _, err := os.Stat(filepath.Join(root, "slot-"+strconv.Itoa(i))); err != nil {
				return false
			}
		}
		return true
	})
	c := &SessionRecoveryClient{Remote: recoveryFixtureRemote(root), RuntimeDir: root, SelfCommand: filepath.Join(root, "self")}
	t.Cleanup(func() {
		c.Close()
		awaitSession(t, func() bool { return recoveryFileLines(root, "daemon-exits") == 1 })
	})
	exchange := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := c.Exchange(ctx, recoveryRequest(0))
		return err
	}
	if err := exchange(); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool { return recoveryFileLines(root, "probes") == 1 })
	data, err := os.ReadFile(filepath.Join(root, "probes"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	probe := os.NewFile(uintptr(fd), "original-test-probe")
	defer probe.Close()
	if err := unix.PidfdSendSignal(int(c.peer.Fd()), syscall.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool { return exchange() == nil && recoveryFileLines(root, "daemons") == 2 })
	awaitSession(t, func() bool { return recoveryFileLines(root, "probes") == 2 })
	if recoveryPeerAlive(probe) {
		t.Fatal("replacement began before the orphaned host probe stopped")
	}
	if release, err := c.acquirePendingSlot(); err == nil {
		release()
		t.Fatal("coordinator restart forgot live helpers' pending capacity")
	}
	if err := os.WriteFile(filepath.Join(root, "release-slots"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool {
		release, err := c.acquirePendingSlot()
		if err != nil {
			return false
		}
		release()
		return true
	})
}
