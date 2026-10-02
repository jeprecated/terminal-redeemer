package mirror

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"os"
)

func recoveryPeer(conn *net.UnixConn) (int, *os.File, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, nil, err
	}
	var cred *unix.Ucred
	pidfd := -1
	var opErr error
	err = raw.Control(func(fd uintptr) {
		cred, opErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if opErr == nil && cred.Uid != uint32(os.Getuid()) {
			opErr = fmt.Errorf("recovery peer UID differs")
		}
		if opErr == nil {
			pidfd, opErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, soPeerPIDFD)
		}
	})
	if err != nil || opErr != nil {
		if pidfd >= 0 {
			unix.Close(pidfd)
		}
		return 0, nil, fmt.Errorf("recovery peer verification requires Linux 6.5+ SO_PEERPIDFD: %w", errors.Join(err, opErr))
	}
	unix.CloseOnExec(pidfd)
	return int(cred.Pid), os.NewFile(uintptr(pidfd), "recovery-peer"), nil
}
func recoveryPeerAlive(fd *os.File) bool {
	if fd == nil {
		return false
	}
	value := fd.Fd()
	if value == ^uintptr(0) {
		return false
	}
	p := []unix.PollFd{{Fd: int32(value), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(p, 0)
		// Go's async-preemption signal can interrupt even a zero-time poll.
		// EINTR is not process-death evidence (nor a reason to free a slot).
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err == nil && n == 0
	}
}
