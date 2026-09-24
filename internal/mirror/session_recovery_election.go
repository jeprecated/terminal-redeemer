package mirror

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func (c *SessionRecoveryClient) recoveryElectionHeld() (bool, error) {
	p, err := recoveryPathsAt(c.Remote, c.RuntimeDir, false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer p.close()
	fd, err := unix.Openat(int(p.dir.Fd()), "lock", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	f := os.NewFile(uintptr(fd), "recovery-election-status")
	defer f.Close()
	if err := p.verifyLock(f); err != nil {
		return false, err
	}
	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}
