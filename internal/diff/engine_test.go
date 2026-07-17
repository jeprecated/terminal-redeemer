package diff

import (
	"testing"

	"github.com/jmo/terminal-redeemer/internal/model"
)

func TestUnchangedStateEmitsNoPatches(t *testing.T) {
	t.Parallel()

	state := model.State{
		Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}},
		Windows:    []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "shell"}},
	}

	engine := NewEngine()
	patches, changed, err := engine.Diff(state, state)
	if err != nil {
		t.Fatalf("diff unchanged: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false")
	}
	if len(patches) != 0 {
		t.Fatalf("expected no patches, got %d", len(patches))
	}
}

func TestSingleFieldChangeEmitsSparsePatch(t *testing.T) {
	t.Parallel()

	before := model.State{
		Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}},
		Windows:    []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "old title"}},
	}
	after := model.State{
		Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}},
		Windows:    []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "new title"}},
	}

	engine := NewEngine()
	patches, changed, err := engine.Diff(before, after)
	if err != nil {
		t.Fatalf("diff single field: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(patches) != 1 {
		t.Fatalf("expected one patch, got %d", len(patches))
	}

	patch := patches[0]
	if patch.WindowKey != "w-1" {
		t.Fatalf("expected patch for w-1, got %q", patch.WindowKey)
	}
	if len(patch.Fields) != 1 {
		t.Fatalf("expected sparse patch with one field, got %#v", patch.Fields)
	}
	if patch.Fields["title"] != "new title" {
		t.Fatalf("expected title patch, got %#v", patch.Fields)
	}
}

func TestWorkspaceOnlyChangeEmitsFullStatePatch(t *testing.T) {
	t.Parallel()

	before := model.State{Workspaces: []model.Workspace{{ID: "runtime-1", Index: 1, Output: "DP-1"}}}
	after := model.State{Workspaces: []model.Workspace{{ID: "runtime-1", Index: 1, Name: "work", Output: "DP-1"}}}

	patches, changed, err := NewEngine().Diff(before, after)
	if err != nil {
		t.Fatalf("diff workspace metadata: %v", err)
	}
	if !changed || len(patches) != 1 || patches[0].State == nil {
		t.Fatalf("workspace-only change was not represented: changed=%v patches=%#v", changed, patches)
	}
	if patches[0].State.Workspaces[0].Name != "work" {
		t.Fatalf("full-state patch lost workspace metadata: %#v", patches[0].State)
	}
}

func TestPlacementOnlyChangeEmitsSparseWindowPatch(t *testing.T) {
	t.Parallel()

	columnA, columnB := 1, 2
	floating := false
	beforeWindow := model.Window{Key: "w-1", AppID: "kitty", WorkspaceID: "runtime-1", Placement: &model.Placement{Column: &columnA, IsFloating: &floating}}
	afterWindow := beforeWindow
	afterWindow.Placement = &model.Placement{Column: &columnB, IsFloating: &floating, TileSize: []float64{900, 700}}
	workspace := []model.Workspace{{ID: "runtime-1", Index: 1, Name: "work", Output: "DP-1"}}

	patches, changed, err := NewEngine().Diff(
		model.State{Workspaces: workspace, Windows: []model.Window{beforeWindow}},
		model.State{Workspaces: workspace, Windows: []model.Window{afterWindow}},
	)
	if err != nil {
		t.Fatalf("diff placement: %v", err)
	}
	if !changed || len(patches) != 1 || patches[0].State != nil {
		t.Fatalf("placement-only change was not sparse: changed=%v patches=%#v", changed, patches)
	}
	if len(patches[0].Fields) != 1 || patches[0].Fields["placement"] == nil {
		t.Fatalf("unexpected placement patch: %#v", patches[0].Fields)
	}
}

func TestOptionalFieldAddRemoveBehavior(t *testing.T) {
	t.Parallel()

	baseWindow := model.Window{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "shell"}
	withMeta := baseWindow
	withMeta.Terminal = &model.Terminal{CWD: "/tmp", SessionTag: "sess-a"}

	engine := NewEngine()

	addPatches, changed, err := engine.Diff(
		model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{baseWindow}},
		model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{withMeta}},
	)
	if err != nil {
		t.Fatalf("diff add optional: %v", err)
	}
	if !changed || len(addPatches) != 1 {
		t.Fatalf("expected one add patch, changed=%v len=%d", changed, len(addPatches))
	}
	if addPatches[0].Fields["terminal"] == nil {
		t.Fatalf("expected terminal add payload, got %#v", addPatches[0].Fields)
	}

	removePatches, changed, err := engine.Diff(
		model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{withMeta}},
		model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{baseWindow}},
	)
	if err != nil {
		t.Fatalf("diff remove optional: %v", err)
	}
	if !changed || len(removePatches) != 1 {
		t.Fatalf("expected one remove patch, changed=%v len=%d", changed, len(removePatches))
	}
	if value, ok := removePatches[0].Fields["terminal"]; !ok || value != nil {
		t.Fatalf("expected terminal nil tombstone, got %#v", removePatches[0].Fields)
	}
}
