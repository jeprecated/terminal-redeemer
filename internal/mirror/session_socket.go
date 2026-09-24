package mirror

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
	"golang.org/x/sys/unix"
)

// These are attempt observations, NOT authoritative evidence that a session
// ended. Only a subsequent complete host catalog can establish that.
var errSessionMissing = errors.New("session socket missing")
var errSessionReplaced = errors.New("session incarnation changed")

type pinnedSessionSocket struct {
	base     *os.File
	dir      *os.File
	name     string
	endpoint string
	view     string
	once     sync.Once
}

// Open every component without following symlinks, then operate relative to
// held directory descriptors. A path rename cannot redirect the pin operation.
func openSocketDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("socket base must be a clean absolute path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), path)
	if err := checkSocketDirectory(fd); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func checkSocketDirectory(fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Getuid()) || st.Mode&0022 != 0 {
		return fmt.Errorf("unsafe socket directory ownership or permissions")
	}
	return nil
}

func pinSessionSocket(base, session, expected, boot string) (_ *pinnedSessionSocket, err error) {
	if !zellijlive.SafeSessionName(session) || !validSessionID(expected) || boot == "" {
		return nil, fmt.Errorf("invalid exact attachment identity")
	}
	b, err := openSocketDirectory(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errSessionMissing
	}
	if err != nil {
		return nil, err
	}
	p := &pinnedSessionSocket{base: b}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	contract, err := unix.Openat(int(b.Fd()), zellijlive.SocketContractDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errSessionMissing
	}
	if err != nil {
		return nil, err
	}
	defer unix.Close(contract)
	if err = checkSocketDirectory(contract); err != nil {
		return nil, err
	}
	check := func(fd int, name string) error {
		var st unix.Stat_t
		if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errSessionMissing
			}
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != uint32(os.Getuid()) {
			return fmt.Errorf("unsafe session socket")
		}
		id, err := zellijlive.ExactSocketIDAt(fd, name, boot, session)
		if err != nil {
			return err
		}
		if id != expected {
			return errSessionReplaced
		}
		return nil
	}
	if err = check(contract, session); err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	name := ".redeem-attach-" + hex.EncodeToString(nonce[:])
	if err = unix.Mkdirat(int(b.Fd()), name, 0700); err != nil {
		return nil, err
	}
	p.name = name
	dir, err := unix.Openat(int(b.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	p.dir = os.NewFile(uintptr(dir), name)
	if err = unix.Linkat(contract, session, dir, "pinned", 0); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = errSessionMissing
		}
		return nil, err
	}
	// Link first, then verify: a replacement between inspection and link is
	// rejected; a replacement after this point cannot change this pinned inode.
	if err = check(dir, "pinned"); err != nil {
		return nil, err
	}
	p.endpoint = fmt.Sprintf("/proc/self/fd/%d/pinned", dir)
	// The child's namespace uses the helper's held fd (not its own fd table).
	// This also avoids the Unix socket pathname limit for long configured bases.
	p.view = fmt.Sprintf("/proc/%d/fd/%d/view", os.Getpid(), dir)
	if err = os.MkdirAll(filepath.Join(p.view, zellijlive.SocketContractDir), 0700); err != nil {
		return nil, err
	}
	if err = unix.Mkdirat(dir, "cache", 0700); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *pinnedSessionSocket) Close() {
	p.once.Do(func() {
		if p.base != nil {
			if p.name != "" {
				// Never adopt/reuse a prior attempt's directory or remove a replacement.
				var held, named unix.Stat_t
				same := p.dir != nil && unix.Fstat(int(p.dir.Fd()), &held) == nil && unix.Fstatat(int(p.base.Fd()), p.name, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && held.Dev == named.Dev && held.Ino == named.Ino
				if same {
					_ = os.RemoveAll(fmt.Sprintf("/proc/self/fd/%d/%s", p.base.Fd(), p.name))
				}
			}
			if p.dir != nil {
				_ = p.dir.Close()
			}
			_ = p.base.Close()
		}
	})
}
