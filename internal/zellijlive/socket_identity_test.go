package zellijlive

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestExactSocketIdentityDistinguishesRecycledInode(t *testing.T) {
	st := unix.Statx_t{Mask: unix.STATX_BASIC_STATS | unix.STATX_BTIME, Mode: unix.S_IFSOCK | 0600, Uid: uint32(os.Getuid()), Ino: 123, Dev_major: 1, Dev_minor: 2, Btime: unix.StatxTimestamp{Sec: 100, Nsec: 20}}
	first, err := exactSocketID("boot", "Session", st)
	if err != nil {
		t.Fatal(err)
	}
	st.Btime.Nsec++
	second, err := exactSocketID("boot", "Session", st)
	if err != nil || first == second {
		t.Fatalf("recycled inode identity: %q %q %v", first, second, err)
	}
	st.Mask &^= unix.STATX_BTIME
	if _, err := exactSocketID("boot", "Session", st); err == nil {
		t.Fatal("missing birth time accepted")
	}
}

func TestExactSocketIdentityStableAcrossHardLinks(t *testing.T) {
	root, err := os.MkdirTemp("", "ra-id-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "source")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	before, err := ExactSocketIDAt(unix.AT_FDCWD, path, "boot", "Session")
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "pin")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	after, err := ExactSocketIDAt(unix.AT_FDCWD, link, "boot", "Session")
	if err != nil || before != after {
		t.Fatalf("pin changed incarnation: %q %q %v", before, after, err)
	}
	rebooted, err := ExactSocketIDAt(unix.AT_FDCWD, link, "other-boot", "Session")
	if err != nil || rebooted == before {
		t.Fatal("boot identity lost")
	}
}
