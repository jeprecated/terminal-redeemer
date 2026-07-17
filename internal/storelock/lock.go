package storelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var ErrLocked = errors.New("state store is locked")

type Lock struct {
	file *os.File
}

func Acquire(root string) (*Lock, error) {
	metaDir := filepath.Join(root, "meta")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return nil, fmt.Errorf("create meta dir: %w", err)
	}

	// The file is deliberately persistent. flock is attached to the open file
	// description and is released by the kernel when a process exits, so stale
	// file contents after a crash or reboot cannot retain the lock.
	file, err := os.OpenFile(filepath.Join(metaDir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}

	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}
