package mirror

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

func attachmentSocketFixture(t *testing.T, name, boot string) (string, string, *net.UnixListener) {
	t.Helper()
	base, err := os.MkdirTemp("", "redeem-att-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	path := filepath.Join(base, zellijlive.SocketContractDir, name)
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	t.Cleanup(func() { listener.Close() })
	id, err := zellijlive.ExactSocketIDAt(unix.AT_FDCWD, path, boot, name)
	if err != nil {
		t.Fatal(err)
	}
	return base, id, listener
}

func TestExactSocketPinSurvivesSameNameReplacement(t *testing.T) {
	for _, name := range []string{"Case Sensitive", "-Leading"} {
		t.Run(name, func(t *testing.T) {
			base, id, original := attachmentSocketFixture(t, name, "boot")
			first, err := pinSessionSocket(base, name, id, "boot")
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			second, err := pinSessionSocket(base, name, id, "boot")
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if first.name == second.name {
				t.Fatal("reused attempt namespace")
			}
			first.Close()
			first.Close()
			if _, err := os.Stat(second.endpoint); err != nil {
				t.Fatalf("old cleanup removed new pin: %v", err)
			}
			path := filepath.Join(base, zellijlive.SocketContractDir, name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer replacement.Close()
			if _, err := pinSessionSocket(base, name, id, "boot"); !errors.Is(err, errSessionReplaced) {
				t.Fatalf("accepted replacement: %v", err)
			}
			go func() {
				c, e := original.Accept()
				if e == nil {
					defer c.Close()
					c.Write([]byte("old"))
				}
			}()
			c, err := net.DialTimeout("unix", second.endpoint, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(time.Second))
			b := make([]byte, 3)
			if _, err := io.ReadFull(c, b); err != nil || string(b) != "old" {
				t.Fatalf("pin reached wrong endpoint: %q %v", b, err)
			}
			replacement.SetDeadline(time.Now().Add(10 * time.Millisecond))
			if c, err := replacement.Accept(); err == nil {
				c.Close()
				t.Fatal("replacement received connection")
			}
		})
	}
}

func TestExactSocketRejectsUnsafePathsAndBoot(t *testing.T) {
	base, id, _ := attachmentSocketFixture(t, "Session", "boot")
	for _, name := range []string{"../Session", ".", "..", "bad/name", " Session"} {
		if p, err := pinSessionSocket(base, name, id, "boot"); err == nil {
			p.Close()
			t.Fatalf("accepted %q", name)
		}
	}
	if _, err := pinSessionSocket(base, "Session", id, "another-boot"); !errors.Is(err, errSessionReplaced) {
		t.Fatalf("accepted another boot: %v", err)
	}
	if _, err := pinSessionSocket(base, "missing", id, "boot"); !errors.Is(err, errSessionMissing) {
		t.Fatalf("missing: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := pinSessionSocket(alias, "Session", id, "boot"); err == nil {
		t.Fatal("accepted symlink base")
	}
	// Also reject a symlink in an ancestor, not merely the final directory.
	if _, err := pinSessionSocket(filepath.Join(alias, zellijlive.SocketContractDir), "Session", id, "boot"); err == nil {
		t.Fatal("accepted symlink ancestor")
	}
	path := filepath.Join(base, zellijlive.SocketContractDir, "linked")
	if err := os.Symlink("Session", path); err != nil {
		t.Fatal(err)
	}
	if _, err := pinSessionSocket(base, "linked", id, "boot"); err == nil {
		t.Fatal("accepted symlink socket")
	}
	if err := os.Chmod(filepath.Join(base, zellijlive.SocketContractDir), 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := pinSessionSocket(base, "Session", id, "boot"); err == nil {
		t.Fatal("accepted writable socket directory")
	}
}

func TestPinCleanupDoesNotRemoveReplacedAttemptDirectory(t *testing.T) {
	base, id, _ := attachmentSocketFixture(t, "s", "boot")
	p, err := pinSessionSocket(base, "s", id, "boot")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, p.name)
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removed replacement directory: %v", err)
	}
}
