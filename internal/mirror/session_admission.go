package mirror

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// The coordinator schedules fairly; these two helper-held kernel locks also
// enforce pending capacity across coordinator death. They carry no desired
// window/session registry. Never unlink/recreate a live semaphore inode.
func (c *SessionRecoveryClient) acquirePendingSlot() (func(), error) {
	p, err := openRecoveryPaths(c.Remote, c.RuntimeDir)
	if err != nil {
		return nil, err
	}
	defer p.close()
	for i := 0; i < 2; i++ {
		fd, err := unix.Openat(int(p.dir.Fd()), fmt.Sprintf("pending-%d", i), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), "pending-attachment")
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 {
			f.Close()
			return nil, fmt.Errorf("unsafe pending admission lock")
		}
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		f.Close()
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("waiting for previous pending attachments to become ready or be reaped")
}
