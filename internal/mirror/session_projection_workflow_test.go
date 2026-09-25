package mirror

import (
	"context"
	"testing"
	"time"
)

func TestApplyAndFollowRefuseNameOnlyLaunches(t *testing.T) {
	for _, dry := range []bool{false, true} {
		snapshot := activeSnapshot("lattice", "A")
		snapshot.SessionIDs = nil
		cfg := applyTestConfig(t, testPin("A"), snapshot)
		cfg.DryRun = dry
		runner := &pinRunner{}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		result, err := ApplyPinned(ctx, cfg, ApplyDeps{Runner: runner, ListWindows: func(context.Context) ([]OwnedWindow, error) { return nil, nil }, Workspaces: func(context.Context) ([]OwnedWorkspace, error) { return nil, nil }, Inspect: func(context.Context, []OwnedWindow, ProjectionEvidenceConfig) (ProjectionInventory, error) {
			return ProjectionInventory{}, nil
		}})
		cancel()
		if err != nil || len(result.Items) != 1 || result.Items[0].Status != ApplyFailed || len(runner.commands) != 0 {
			t.Fatalf("name-only apply: %+v %v %+v", result, err, runner.commands)
		}
	}
	snapshot := followSnapshot("A")
	snapshot.SessionIDs = nil
	sim := newFollowSim()
	result := FollowOnce(context.Background(), followConfig(), snapshot, selectedWorkspace(snapshot), FrozenDestination{ID: "local"}, &FollowState{}, sim.deps())
	if result.Healthy || result.Attempted != 0 || len(sim.commands) != 0 {
		t.Fatalf("name-only follow: %+v %+v", result, sim.commands)
	}
}

func TestSupervisedPresenceSavesAndDeduplicatesRegardlessOfReadiness(t *testing.T) {
	for _, ready := range []bool{false, true} {
		snapshot := followSnapshot("A")
		snapshot.GeneratedAt = time.Now()
		id := recoveryRequest(0).SessionID
		snapshot.SessionIDs = map[string]string{"A": id}
		local := OwnedWindow{ID: 40, PID: 400, WorkspaceID: "local"}
		projection := Projection{Window: local, SourceHost: "lattice", Session: "A", SessionID: id, Supervised: true, Ready: ready}
		inventory := ProjectionInventory{Exact: []Projection{projection}}
		workspaces := []OwnedWorkspace{{ID: "local", Index: 2, Name: "Dev"}}
		saved, err := BuildPin(snapshot, "lattice", []OwnedWindow{local}, workspaces, inventory)
		if err != nil || len(saved.Pin.Projections) != 1 {
			t.Fatalf("ready=%v save=%+v err=%v", ready, saved, err)
		}
		applied := prepareApply(saved.Pin, activeSet("A"), inventory, workspaces)
		if len(applied.Items) != 1 || applied.Items[0].Status != ApplyAlreadyOpen {
			t.Fatalf("offline helper duplicated: %+v", applied)
		}
		sim := newFollowSim()
		sim.exact = &projection
		selection := SelectionForWorkspace(snapshot.Workspaces[1])
		followed := FollowOnce(context.Background(), followConfig(), snapshot, selection, FrozenDestination{ID: "local"}, &FollowState{}, sim.deps())
		if !followed.Healthy || followed.Existing != 1 || len(sim.commands) != 0 {
			t.Fatalf("follow duplicated existing helper: %+v %+v", followed, sim.commands)
		}
		snapshot.SessionIDs["A"] = recoveryRequest(1).SessionID
		if _, err := BuildPin(snapshot, "lattice", []OwnedWindow{local}, workspaces, inventory); err == nil {
			t.Fatal("save adopted a same-name replacement")
		}
		snapshot.SessionIDs = nil
		if _, err := BuildPin(snapshot, "lattice", []OwnedWindow{local}, workspaces, inventory); err == nil {
			t.Fatal("save accepted missing incarnation evidence")
		}
	}
}

func TestApplyNeverDuplicatesUnverifiedHelperWindow(t *testing.T) {
	root := t.TempDir()
	remote := RemoteConfig{Host: "lattice", SSHCommand: "ssh", SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
	r := recoveryRequest(0)
	// Opened with a --self-command override the inspecting config cannot verify.
	argv, err := sessionSupervisorArgv("/home/u/dev/redeem", remote, r.Session, r.SessionID, r.Token)
	if err != nil {
		t.Fatal(err)
	}
	writeMirrorProc(t, root, 100, 1, 10, []string{"kitty"})
	writeMirrorProc(t, root, 101, 100, 11, argv)
	writeMirrorProc(t, root, 200, 1, 20, []string{"kitty"})
	windows := []OwnedWindow{{ID: 1, PID: 100}, {ID: 2, PID: 200}}
	inventory, err := InspectProjections(context.Background(), windows, ProjectionEvidenceConfig{ProcRoot: root, SelfCommand: "redeem", SSHCommand: "ssh"})
	if err != nil || len(inventory.Untracked) != 2 || len(inventory.Unverified) != 1 || inventory.Unverified[0].ID != 1 {
		t.Fatalf("unverified helper not distinguished from plain window: %+v %v", inventory, err)
	}
	snapshot := activeSnapshot("lattice", r.Session)
	snapshot.SessionIDs = map[string]string{r.Session: r.SessionID}
	cfg := applyTestConfig(t, testPin(r.Session), snapshot)
	runner := &pinRunner{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := ApplyPinned(ctx, cfg, ApplyDeps{Runner: runner, ListWindows: func(context.Context) ([]OwnedWindow, error) { return windows, nil }, Workspaces: func(context.Context) ([]OwnedWorkspace, error) { return nil, nil }, Inspect: func(context.Context, []OwnedWindow, ProjectionEvidenceConfig) (ProjectionInventory, error) {
		return inventory, nil
	}})
	if err != nil || len(result.Items) != 1 || result.Items[0].Status != ApplyAmbiguous || len(runner.commands) != 0 {
		t.Fatalf("apply launched beside an unverified helper: %+v %v %+v", result, err, runner.commands)
	}
	plain := ProjectionInventory{Untracked: []OwnedWindow{windows[1]}}
	if applied := prepareApply(testPin(r.Session), activeSet(r.Session), plain, nil); applied.Items[0].Status != ApplyReady {
		t.Fatalf("plain untracked window blocked apply: %+v", applied)
	}
}
