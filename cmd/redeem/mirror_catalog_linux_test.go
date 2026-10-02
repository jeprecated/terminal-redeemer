package main

import (
	"bytes"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/bootid"
	"github.com/jmo/terminal-redeemer/internal/config"
	"github.com/jmo/terminal-redeemer/internal/mirror"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

func TestMirrorSessionCatalogIsHeadlessAndBootBound(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "redeem-catalog-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	base := filepath.Join(root, "s")
	dir := filepath.Join(base, zellijlive.SocketContractDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "-Agent")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	script := "#!/bin/sh\n[ \"$*\" = 'list-sessions --short --no-formatting' ] || exit 8\nprintf '%s\\n' '-Agent'\n"
	if err := os.WriteFile(filepath.Join(root, "zellij"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("ZELLIJ_SOCKET_DIR", base)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("NIRI_SOCKET", "/missing/niri")
	t.Setenv("WAYLAND_DISPLAY", "")
	var out, diagnostic bytes.Buffer
	if code := runMirror([]string{"session-catalog"}, config.Defaults(), &out, &diagnostic); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &diagnostic)
	}
	inventory, err := mirror.DecodeSessionInventory(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	boot, err := bootid.Current()
	if err != nil {
		t.Fatal(err)
	}
	id, err := zellijlive.ExactSocketIDAt(unix.AT_FDCWD, path, boot, "-Agent")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.SessionIDs["-Agent"] != id {
		t.Fatalf("not boot-bound: %+v", inventory)
	}
}

func TestMirrorSessionCatalogFailureNeverPublishesEmptyInventory(t *testing.T) {
	root := t.TempDir()
	script := "#!/bin/sh\necho 'permission denied' >&2; exit 1\n"
	if err := os.WriteFile(filepath.Join(root, "zellij"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("ZELLIJ_SOCKET_DIR", filepath.Join(root, "s"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	for _, args := range [][]string{{"session-catalog"}, {"session-catalog", "--timeout", "0"}, {"session-catalog", "unexpected"}} {
		var out, diagnostic bytes.Buffer
		if code := runMirror(args, config.Defaults(), &out, &diagnostic); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, &out, &diagnostic)
		}
	}
}

func TestMirrorSessionCatalogBoundsBlockedSubprocess(t *testing.T) {
	root := t.TempDir()
	script := "#!/bin/sh\nsleep 30 & wait\n"
	if err := os.WriteFile(filepath.Join(root, "zellij"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ZELLIJ_SOCKET_DIR", filepath.Join(root, "s"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var out, diagnostic bytes.Buffer
	start := time.Now()
	code := runMirror([]string{"session-catalog", "--timeout", "100ms"}, config.Defaults(), &out, &diagnostic)
	if code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "deadline") || time.Since(start) > 2*time.Second {
		t.Fatalf("code=%d stdout=%s stderr=%s elapsed=%s", code, &out, &diagnostic, time.Since(start))
	}
}

func TestRealZellijEmptyCatalogIsAuthoritative(t *testing.T) {
	if _, err := exec.LookPath("zellij"); err != nil {
		t.Skip("Zellij is unavailable")
	}
	root := t.TempDir()
	// Override both catalogs; never list or mutate the user's actual sessions.
	t.Setenv("ZELLIJ_SOCKET_DIR", filepath.Join(root, "s"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var out, diagnostic bytes.Buffer
	if code := runMirror([]string{"session-catalog"}, config.Defaults(), &out, &diagnostic); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &diagnostic)
	}
	inventory, err := mirror.DecodeSessionInventory(out.Bytes())
	if err != nil || inventory.ActiveSessions == nil || len(inventory.ActiveSessions) != 0 || inventory.SessionIDs == nil || len(inventory.SessionIDs) != 0 {
		t.Fatalf("inventory=%+v err=%v", inventory, err)
	}
}
