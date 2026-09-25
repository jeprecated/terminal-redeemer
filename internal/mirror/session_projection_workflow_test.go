package mirror

import (
	"context"
	"testing"
	"time"
)

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
