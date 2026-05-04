package mirror

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/procmeta"
)

type fakeReader map[int]procmeta.ProcessInfo

func (r fakeReader) Inspect(pid int) (procmeta.ProcessInfo, error) {
	return r[pid], nil
}

type fakeVerifier map[string]bool

func (v fakeVerifier) Exists(session string) (bool, error) {
	return v[session], nil
}

type fakeResolver map[string]string

func (r fakeResolver) Resolve(session string) (string, error) {
	return r[session], nil
}

func TestCaptureOrdersWindowsAndIncludesZellijSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixturePath := filepath.Join(root, "niri.json")
	payload := []byte(`{
		"workspaces": [
			{"id": "ws-2", "idx": 2, "output": "DP-1"},
			{"id": "ws-1", "idx": 1, "output": "DP-1"}
		],
		"windows": [
			{"id": 30, "app_id": "kitty", "title": "third", "workspace_id": "ws-2", "pid": 300, "layout": {"pos_in_scrolling_layout": [1, 1], "tile_size": [100, 50], "window_size": [98, 48]}},
			{"id": 20, "app_id": "kitty", "title": "second", "workspace_id": "ws-1", "pid": 200, "layout": {"pos_in_scrolling_layout": [2, 1]}},
			{"id": 10, "app_id": "kitty", "title": "first", "workspace_id": "ws-1", "pid": 100, "layout": {"pos_in_scrolling_layout": [1, 1]}},
			{"id": 40, "app_id": "firefox", "title": "browser", "workspace_id": "ws-1", "pid": 400, "layout": {"pos_in_scrolling_layout": [3, 1]}}
		]
	}`)
	if err := os.WriteFile(fixturePath, payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	snapshot, err := Capture(context.Background(), Options{
		Host:        "lattice",
		Profile:     "default",
		FixturePath: fixturePath,
		GeneratedAt: time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC),
		Reader: fakeReader{
			100: {CWD: "/home/jmo/one", Env: map[string]string{"ZELLIJ_SESSION_NAME": "one"}},
			200: {CWD: "/home/jmo/two", Env: map[string]string{"ZELLIJ_SESSION_NAME": "two"}},
			300: {CWD: "/home/jmo/three", Env: map[string]string{"ZELLIJ_SESSION_NAME": "three"}},
		},
		ProcessMetadata: procmeta.Config{IncludeSessionTag: true},
	})
	if err != nil {
		t.Fatalf("capture mirror snapshot: %v", err)
	}

	if snapshot.Host != "lattice" {
		t.Fatalf("expected host lattice, got %q", snapshot.Host)
	}
	if len(snapshot.Windows) != 4 {
		t.Fatalf("expected 4 windows, got %d", len(snapshot.Windows))
	}

	wantTitles := []string{"first", "second", "browser", "third"}
	for i, want := range wantTitles {
		if got := snapshot.Windows[i].Title; got != want {
			t.Fatalf("window %d title: got %q want %q", i, got, want)
		}
		if snapshot.Windows[i].Order != i {
			t.Fatalf("window %d order: got %d", i, snapshot.Windows[i].Order)
		}
	}

	first := snapshot.Windows[0]
	if first.ZellijSession != "one" {
		t.Fatalf("expected top-level zellij session one, got %q", first.ZellijSession)
	}
	if first.Terminal == nil || first.Terminal.CWD != "/home/jmo/one" || first.Terminal.ZellijSession != "one" {
		t.Fatalf("unexpected terminal metadata: %#v", first.Terminal)
	}
	if snapshot.Windows[2].Terminal != nil {
		t.Fatalf("expected non-terminal browser to omit terminal metadata: %#v", snapshot.Windows[2].Terminal)
	}
}

func TestCaptureExtractsVerifiedSessionFromTitle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fixturePath := filepath.Join(root, "niri.json")
	payload := []byte(`{
		"workspaces": [{"id": "ws-1", "idx": 1}],
		"windows": [{"id": 10, "app_id": "kitty", "title": "title-session | π - work", "workspace_id": "ws-1", "pid": 100}]
	}`)
	if err := os.WriteFile(fixturePath, payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	snapshot, err := Capture(context.Background(), Options{
		FixturePath: fixturePath,
		Reader:      fakeReader{100: {CWD: "/home/jmo", Env: map[string]string{}}},
		Verifier:    fakeVerifier{"title-session": true},
		Resolver:    fakeResolver{"title-session": "/home/jmo/project"},
		ProcessMetadata: procmeta.Config{
			IncludeSessionTag: true,
		},
	})
	if err != nil {
		t.Fatalf("capture mirror snapshot: %v", err)
	}

	if len(snapshot.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(snapshot.Windows))
	}
	window := snapshot.Windows[0]
	if window.ZellijSession != "title-session" {
		t.Fatalf("expected title-derived session, got %q", window.ZellijSession)
	}
	if window.Terminal == nil || window.Terminal.CWD != "/home/jmo/project" {
		t.Fatalf("expected resolver-upgraded cwd, got %#v", window.Terminal)
	}
}
