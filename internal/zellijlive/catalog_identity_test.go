package zellijlive

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestCatalogPreservesLiteralSocketNamesAndBootIdentity(t *testing.T) {
	root := shortSocketTempDir(t)
	base := filepath.Join(root, "s")
	dir := filepath.Join(base, SocketContractDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	names := []string{"-Agent", "Agent workspace", "agent", "No active zellij sessions found."}
	for _, name := range names {
		listener, err := net.Listen("unix", filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
	}
	script := filepath.Join(root, "zellij")
	body := "#!/bin/sh\n[ \"$*\" = 'list-sessions --short --no-formatting' ] || exit 9\nprintf '%s\\n' '-Agent' 'Agent workspace' 'agent' 'No active zellij sessions found.'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	observer := CommandCataloger{Command: script, SocketBase: base, CacheHome: filepath.Join(root, "cache"), BootID: "boot-one"}
	first, err := observer.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	observer.BootID = "boot-two"
	second, err := observer.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		session := first.Exact(name)
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if session.Status != StatusActive || session.ID != SessionID("boot-one", name, uint64(stat.Dev), stat.Ino) {
			t.Fatalf("wrong exact identity: %+v", session)
		}
		if second.Exact(name).ID == session.ID {
			t.Fatal("boot change did not rotate identity")
		}
	}
}

func TestCatalogAcceptsOnlyPinnedEmptyResult(t *testing.T) {
	cases := []struct {
		name, body string
		accepted   bool
	}{
		{"pinned empty", "echo 'No active zellij sessions found.' >&2; exit 1", true},
		{"empty failure", "exit 1", false},
		{"empty message wrong code", "echo 'No active zellij sessions found.' >&2; exit 2", false},
		{"empty plus diagnostic", "echo 'No active zellij sessions found.' >&2; echo 'permission denied' >&2; exit 1", false},
		{"empty plus names", "echo Alpha; echo 'No active zellij sessions found.' >&2; exit 1", false},
		{"success diagnostic", "echo 'permission denied' >&2; exit 0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "zellij")
			body := "#!/bin/sh\n" + tc.body + "\n"
			if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			catalog, err := (CommandCataloger{Command: script, SocketBase: filepath.Join(root, "sockets"), CacheHome: filepath.Join(root, "cache"), BootID: "boot"}).Observe(context.Background())
			if tc.accepted {
				if err != nil || catalog.Names == nil || len(catalog.Names) != 0 {
					t.Fatalf("empty result: %+v err=%v", catalog, err)
				}
			} else if err == nil {
				t.Fatalf("uncertain catalog accepted: %+v", catalog)
			}
		})
	}
}

func TestLiteralSessionNamesAreNotTrimmedOrTokenized(t *testing.T) {
	want := []string{"Alpha", "Alpha beta", "-agent", "No active zellij sessions found."}
	got, err := parseLines([]byte(strings.Join(want, "\n") + "\n"))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("names=%q err=%v", got, err)
	}
	for _, invalid := range []string{" Alpha", "Alpha ", "\x1b[31mAlpha", "../Alpha", "Alpha/beta", ".", ".."} {
		if _, err := parseLines([]byte(invalid + "\n")); err == nil {
			t.Fatalf("invalid name accepted: %q", invalid)
		}
	}
}
