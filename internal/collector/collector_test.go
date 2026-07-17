package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/jmo/terminal-redeemer/internal/model"
)

func TestCollectFromFixtureAndEnrich(t *testing.T) {
	t.Parallel()

	snapshotter := stubSnapshotter{raw: []byte(`{
  "workspaces": [{"id": "ws-1", "idx": 1, "name": "main"}],
  "windows": [{"id": 101, "app_id": "kitty", "title": "shell", "workspace_id": "ws-1", "pid": 4242}]
}`)}
	enricher := stubEnricher{window: model.Window{Key: "w:kitty:101", AppID: "kitty", WorkspaceID: "ws-1", PID: 4242, Terminal: &model.Terminal{CWD: "/tmp"}}}

	c := New(snapshotter, enricher)
	state, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(state.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(state.Windows))
	}
	if state.Windows[0].Terminal == nil || state.Windows[0].Terminal.CWD != "/tmp" {
		t.Fatalf("expected enriched cwd, got %#v", state.Windows[0].Terminal)
	}
}

func TestCollectRetainsPlacementWhenTerminalIsEnriched(t *testing.T) {
	t.Parallel()

	snapshotter := stubSnapshotter{raw: []byte(`{
  "workspaces": [{"id": 7, "idx": 2, "name": null, "output": "DP-1"}],
  "windows": [{"id": 101, "app_id": "kitty", "title": "shell", "workspace_id": 7, "pid": 4242, "is_floating": false, "layout": {"pos_in_scrolling_layout": [4, 1], "tile_size": [900, 700]}}]
}`)}
	c := New(snapshotter, metadataEnricher{})
	state, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	window := state.Windows[0]
	if window.Terminal == nil || window.Terminal.SessionTag != "verified-session" || window.Terminal.CWD != "/work/project" {
		t.Fatalf("verified terminal metadata missing: %#v", window.Terminal)
	}
	if window.WorkspaceRef == nil || window.WorkspaceRef.Output != "DP-1" || window.WorkspaceRef.Index != 2 {
		t.Fatalf("durable workspace metadata missing: %#v", window.WorkspaceRef)
	}
	if window.Placement == nil || window.Placement.Column == nil || *window.Placement.Column != 4 || window.Placement.IsFloating == nil || *window.Placement.IsFloating {
		t.Fatalf("placement metadata missing: %#v", window.Placement)
	}
}

func TestCollectGracefullyDegradesOnMetadataError(t *testing.T) {
	t.Parallel()

	snapshotter := stubSnapshotter{raw: []byte(`{
  "workspaces": [{"id": "ws-1", "idx": 1}],
  "windows": [{"id": 101, "app_id": "kitty", "workspace_id": "ws-1", "pid": 4242}]
}`)}
	enricher := stubEnricher{err: errors.New("metadata failure")}

	c := New(snapshotter, enricher)
	state, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect should not fail on metadata errors: %v", err)
	}

	if len(state.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(state.Windows))
	}
	if state.Windows[0].Terminal != nil {
		t.Fatalf("expected no terminal metadata due to degraded mode, got %#v", state.Windows[0].Terminal)
	}
}

type metadataEnricher struct{}

func (metadataEnricher) EnrichWindow(window model.Window) (model.Window, error) {
	window.Terminal = &model.Terminal{CWD: "/work/project", SessionTag: "verified-session"}
	return window, nil
}

type stubSnapshotter struct {
	raw []byte
	err error
}

func (s stubSnapshotter) Snapshot(_ context.Context) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.raw, nil
}

type stubEnricher struct {
	window model.Window
	err    error
}

func (s stubEnricher) EnrichWindow(window model.Window) (model.Window, error) {
	if s.err != nil {
		return model.Window{}, s.err
	}
	if s.window.Key == "" {
		return window, nil
	}
	return s.window, nil
}
