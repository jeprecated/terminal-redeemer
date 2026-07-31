package slicetui

import (
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
	"github.com/jmo/terminal-redeemer/internal/slicelayout"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

func TestRowsExposeIndependentAxesAndOrphanDrops(t *testing.T) {
	now := time.Now().UTC()
	state := slicecontroller.State{
		AllEligible:        true,
		SelectedWorkspaces: map[string]string{"work": "Work"},
		Pickups:            map[string]bool{"src-a": true},
		ClosedByUser: map[string]slicecontroller.SessionDrop{
			"sess-a":      {SessionID: "sess-a", SessionName: "alpha", CreatedAt: now},
			"sess-orphan": {SessionID: "sess-orphan", SessionName: "gone", CreatedAt: now},
		},
		Sources: map[string]slicecontroller.TrackedSource{
			"src-a": {SourceID: "src-a", SessionID: "sess-a", SessionName: "alpha", WorkspaceKey: "work", Lifecycle: slicecontroller.SourceEligible, Connection: slicecontroller.ConnectionDisconnected, Conflict: "projection_ambiguous"},
			"src-b": {SourceID: "src-b", SessionID: "sess-b", SessionName: "beta", WorkspaceKey: "", Lifecycle: slicecontroller.SourceEligible, Connection: slicecontroller.ConnectionConnected},
		},
		Projections: map[string]slicecontroller.Projection{
			"src-b": {SourceID: "src-b", Status: slicecontroller.ProjectionOwned},
		},
		Spatial: map[string]slicecontroller.SpatialRecord{
			"src-a": {Conflict: "spatial_failed"},
			"src-b": {Conflict: "spatial_failed", Baseline: &slicelayout.Spatial{}},
		},
		PendingCleanups: map[string]slicecontroller.CleanupGate{
			"src-a": {SourceID: "src-a", Conflict: "ownership_unproven"},
		},
		SuccessorGates: map[string]slicecontroller.SuccessorGate{
			"src-a": {OldSourceID: "src-a", SessionID: "sess-a", Conflict: "ambiguous_successor"},
		},
		Lineage: map[string]slicecontroller.LineageRecord{
			"src-a": {OldSourceID: "src-a", SessionID: "sess-a", Status: "conflict"},
		},
		Inventory: &sliceprotocol.Authoritative{Sources: []sliceprotocol.Source{
			{SourceID: "src-a", Session: sliceprotocol.Session{ID: "sess-a", Name: "alpha"}, Workspace: sliceprotocol.Workspace{Name: "Work", Key: "work"}},
			{SourceID: "src-b", Session: sliceprotocol.Session{ID: "sess-b", Name: "beta"}, Workspace: sliceprotocol.Workspace{}},
		}},
	}

	rows := rowsForState(state)
	if len(rows) != 6 {
		t.Fatalf("rows=%d %#v", len(rows), rows)
	}
	if rows[0].Kind != rowAll || !rows[0].Selected {
		t.Fatalf("all row=%+v", rows[0])
	}

	var named, unnamed, alpha, beta, orphan *row
	for i := range rows {
		switch rows[i].ID {
		case "workspace:work":
			named = &rows[i]
		case "group:unnamed-workspace":
			unnamed = &rows[i]
		case "source:src-a":
			alpha = &rows[i]
		case "source:src-b":
			beta = &rows[i]
		case "drop:sess-orphan":
			orphan = &rows[i]
		}
	}
	if named == nil || !named.Selected || unnamed == nil || unnamed.Kind != rowGroup {
		t.Fatalf("workspace grouping named=%+v unnamed=%+v", named, unnamed)
	}
	if alpha == nil || !alpha.Discoverable || alpha.Desired || !alpha.Closed || !alpha.Pickup || alpha.Connection != slicecontroller.ConnectionDisconnected || len(alpha.Conflicts) != 5 {
		t.Fatalf("alpha axes=%+v", alpha)
	}
	if beta == nil || !beta.Discoverable || !beta.Desired || beta.Closed || beta.Projection != slicecontroller.ProjectionOwned || beta.Connection != slicecontroller.ConnectionConnected || len(beta.Conflicts) != 1 || beta.Conflicts[0] != "spatial=spatial_failed" {
		t.Fatalf("beta axes=%+v", beta)
	}
	if orphan == nil || !orphan.Closed || !strings.Contains(orphan.label(), "source absent") {
		t.Fatalf("orphan=%+v", orphan)
	}
	label := alpha.label()
	for _, want := range []string{"[discoverable]", "[closed]", "[conflict:tracked=projection_ambiguous]", "[conflict:spatial=spatial_failed]", "[conflict:cleanup=ownership_unproven]", "[conflict:successor=ambiguous_successor]", "[conflict:lineage=conflict]"} {
		if !strings.Contains(label, want) {
			t.Fatalf("label %q missing %q", label, want)
		}
	}
}

func TestPolicyDesireIsIndependentFromLifecycle(t *testing.T) {
	for _, lifecycle := range []slicecontroller.SourceLifecycle{slicecontroller.SourceEligible, slicecontroller.SourceGoneGrace, slicecontroller.SourceConflict, slicecontroller.SourceClosed, slicecontroller.SourceReplaced} {
		state := slicecontroller.State{
			SelectedWorkspaces: map[string]string{"work": "Work"}, Pickups: map[string]bool{}, ClosedByUser: map[string]slicecontroller.SessionDrop{},
			Sources:     map[string]slicecontroller.TrackedSource{"src": {SourceID: "src", SessionID: "session", SessionName: "shell", WorkspaceKey: "work", Lifecycle: lifecycle}},
			Projections: map[string]slicecontroller.Projection{}, Spatial: map[string]slicecontroller.SpatialRecord{},
		}
		rows := rowsForState(state)
		source := rows[rowIndex(rows, "source:src")]
		if !source.Desired {
			t.Fatalf("lifecycle %s incorrectly removed policy desire: %+v", lifecycle, source)
		}
		state.ClosedByUser["session"] = slicecontroller.SessionDrop{SessionID: "session", SessionName: "shell", CreatedAt: time.Now()}
		rows = rowsForState(state)
		source = rows[rowIndex(rows, "source:src")]
		if source.Desired {
			t.Fatalf("lifecycle %s ignored close exclusion: %+v", lifecycle, source)
		}
	}
}

func TestUnnamedGroupIdentityCannotCollideWithNamedWorkspace(t *testing.T) {
	state := slicecontroller.State{
		SelectedWorkspaces: map[string]string{"(unnamed)": "(unnamed)"}, Pickups: map[string]bool{}, ClosedByUser: map[string]slicecontroller.SessionDrop{},
		Sources: map[string]slicecontroller.TrackedSource{
			"named":     {SourceID: "named", SessionID: "named-session", SessionName: "named", WorkspaceKey: "(unnamed)", Lifecycle: slicecontroller.SourceEligible},
			"synthetic": {SourceID: "synthetic", SessionID: "synthetic-session", SessionName: "synthetic", WorkspaceKey: "", Lifecycle: slicecontroller.SourceEligible},
		},
		Projections: map[string]slicecontroller.Projection{}, Spatial: map[string]slicecontroller.SpatialRecord{},
	}
	rows := rowsForState(state)
	named := rowIndex(rows, "workspace:(unnamed)")
	synthetic := rowIndex(rows, "group:unnamed-workspace")
	if named < 0 || synthetic < 0 || named == synthetic || rows[named].Kind != rowWorkspace || rows[synthetic].Kind != rowGroup {
		t.Fatalf("colliding rows=%+v", rows)
	}
}

func TestRowsKeepSelectedEmptyWorkspace(t *testing.T) {
	state := slicecontroller.State{SelectedWorkspaces: map[string]string{"quiet": "Quiet"}, Pickups: map[string]bool{}, ClosedByUser: map[string]slicecontroller.SessionDrop{}, Sources: map[string]slicecontroller.TrackedSource{}, Projections: map[string]slicecontroller.Projection{}, Spatial: map[string]slicecontroller.SpatialRecord{}}
	rows := rowsForState(state)
	if len(rows) != 2 || rows[1].ID != "workspace:quiet" || !rows[1].Selected {
		t.Fatalf("rows=%+v", rows)
	}
}
