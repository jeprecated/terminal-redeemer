package safefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrTooLarge = errors.New("regular file exceeds read bound")

// ReadRegular opens the final path component without following a symlink,
// validates the opened descriptor (not a racy pathname stat), and reads at most
// max+1 bytes. modeMask/modeValue preserve each store's existing permission
// policy, for example 0o777/0o600 for exact private mode or 0o077/0 for no
// group/world permissions.
func ReadRegular(path string, max int, modeMask, modeValue os.FileMode) ([]byte, error) {
	if max < 0 {
		return nil, errors.New("negative regular-file read bound")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("create regular-file descriptor")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&modeMask != modeValue {
		return nil, fmt.Errorf("unsafe regular file %s", path)
	}
	if info.Size() > int64(max) {
		return nil, ErrTooLarge
	}
	payload, err := io.ReadAll(io.LimitReader(file, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > max {
		return nil, ErrTooLarge
	}
	return payload, nil
}
