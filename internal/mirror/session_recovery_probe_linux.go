package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/unix"
)

// A tiny watchdog owns the catalog process group and a reference to the
// coordinator's election lock. Coordinator SIGKILL therefore cannot overlap an
// orphaned probe with a replacement daemon. No persistent lease registry is
// needed: parent pidfd / cancellation-pipe EOF trigger cancellation and reap.
func runRecoveryProbe(ctx context.Context, remote RemoteConfig, runtime, self string, lock *os.File) (SessionInventory, error) {
	parentFD, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return SessionInventory{}, err
	}
	parent := os.NewFile(uintptr(parentFD), "probe-parent")
	defer parent.Close()
	cancelReader, cancelWriter, err := os.Pipe()
	if err != nil {
		return SessionInventory{}, err
	}
	defer cancelReader.Close()
	defer cancelWriter.Close()
	stop := context.AfterFunc(ctx, func() { cancelWriter.Close() })
	defer stop()
	payload, err := json.Marshal(remote)
	if err != nil {
		return SessionInventory{}, err
	}
	cmd := exec.Command(self, "mirror", "session-recovery", "--remote-json", string(payload), "--runtime-dir", runtime, "--lock-fd", "3", "--probe-parent-fd", "4", "--probe-cancel-fd", "5")
	cmd.ExtraFiles = []*os.File{lock, parent, cancelReader}
	cancelProbe := func() { cancelWriter.Close() }
	stdout := &sessionCatalogOutput{max: 1 << 20, cancel: cancelProbe}
	stderr := &sessionCatalogOutput{max: 64 << 10, cancel: cancelProbe}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if late := sessionContextError(ctx); late != nil {
		return SessionInventory{}, late
	}
	if stdout.overflow || stderr.overflow {
		return SessionInventory{}, fmt.Errorf("recovery probe output exceeds bound")
	}
	if err != nil {
		return SessionInventory{}, fmt.Errorf("recovery probe: %w: %s", err, stderr.buffer.String())
	}
	return DecodeSessionInventory(stdout.buffer.Bytes())
}

func RunSessionRecoveryProbe(ctx context.Context, remote RemoteConfig, runtime string, lock, parent, cancellation *os.File, output io.Writer) error {
	defer lock.Close()
	defer parent.Close()
	defer cancellation.Close()
	paths, err := openRecoveryPaths(remote, runtime)
	if err != nil {
		return err
	}
	defer paths.close()
	if err := paths.verifyLock(lock); err != nil {
		return err
	}
	unix.CloseOnExec(int(parent.Fd()))
	unix.CloseOnExec(int(cancellation.Fd()))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		polls := []unix.PollFd{{Fd: int32(parent.Fd()), Events: unix.POLLIN}, {Fd: int32(cancellation.Fd()), Events: unix.POLLIN}}
		for ctx.Err() == nil {
			n, err := unix.Poll(polls, 50)
			if err == unix.EINTR {
				continue
			}
			if err != nil || n > 0 {
				cancel()
				return
			}
		}
	}()
	defer func() { cancel(); <-done }()
	inventory, err := AcquireSessionInventory(ctx, nil, remote)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(inventory)
}
