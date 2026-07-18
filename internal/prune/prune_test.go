package prune

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/checkpoints"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
	"github.com/jmo/terminal-redeemer/internal/snapshots"
)

func TestAgeBasedPruningEventsAndSnapshots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	eventStore, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	writer, err := eventStore.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	defer func() {
		_ = writer.Close()
	}()

	snapStore, err := snapshots.NewStore(root)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	now := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	oldTS := now.AddDate(0, 0, -40)
	newTS := now.AddDate(0, 0, -5)

	olderTS := oldTS.Add(-24 * time.Hour)
	if _, err := writer.Append(events.Event{V: 1, TS: olderTS, Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "older"}, StateHash: "sha256:pre"}); err != nil {
		t.Fatalf("append older event: %v", err)
	}
	if _, err := writer.Append(events.Event{V: 1, TS: oldTS, Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "old"}, StateHash: "sha256:a"}); err != nil {
		t.Fatalf("append old event: %v", err)
	}
	if _, err := writer.Append(events.Event{V: 1, TS: newTS, Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "new"}, StateHash: "sha256:b"}); err != nil {
		t.Fatalf("append new event: %v", err)
	}

	if _, err := snapStore.Write(snapshots.Snapshot{V: 1, CreatedAt: oldTS, Host: "host-a", Profile: "default", LastEventOffset: 10, StateHash: "sha256:a", State: map[string]any{"windows": []any{}}}); err != nil {
		t.Fatalf("write old snapshot: %v", err)
	}
	if _, err := snapStore.Write(snapshots.Snapshot{V: 1, CreatedAt: oldTS.Add(-24 * time.Hour), Host: "host-a", Profile: "default", LastEventOffset: 5, StateHash: "sha256:oldest", State: map[string]any{"windows": []any{}}}); err != nil {
		t.Fatalf("write oldest snapshot: %v", err)
	}
	if _, err := snapStore.Write(snapshots.Snapshot{V: 1, CreatedAt: newTS, Host: "host-a", Profile: "default", LastEventOffset: 20, StateHash: "sha256:b", State: map[string]any{"windows": []any{}}}); err != nil {
		t.Fatalf("write new snapshot: %v", err)
	}
	rolling, err := checkpoints.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	empty := model.State{Workspaces: []model.Workspace{}, Windows: []model.Window{}}
	emptyHash, _ := empty.Hash()
	for _, checkpoint := range []checkpoints.Checkpoint{
		{V: 1, BootID: "boot-old", Host: "host-a", Profile: "default", ObservedAt: oldTS, State: empty, StateHash: emptyHash, EventOffset: 10},
		{V: 1, BootID: "boot-new", Host: "host-a", Profile: "default", ObservedAt: newTS, State: empty, StateHash: emptyHash, EventOffset: 20},
	} {
		if _, err := rolling.Write(checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	_ = writer.Close()

	runner := NewRunner(root, 30, func() time.Time { return now })
	summary, err := runner.Run()
	if err != nil {
		t.Fatalf("prune run: %v", err)
	}
	if summary.EventsPruned == 0 || summary.CheckpointsPruned != 1 || summary.SnapshotsPruned == 0 {
		t.Fatalf("expected old data pruned, got %+v", summary)
	}
}

func TestPruneSafetyWithActiveWriter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	runner := NewRunner(root, 30, time.Now)
	if _, err := runner.Run(); !errors.Is(err, ErrActiveWriter) {
		t.Fatalf("expected active writer error, got %v", err)
	}
}

func TestPruneIgnoresStaleLockMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "meta"), 0o755); err != nil {
		t.Fatalf("make meta dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meta", "lock"), []byte("stale pid 1234"), 0o600); err != nil {
		t.Fatalf("write stale lock file: %v", err)
	}

	runner := NewRunner(root, 30, time.Now)
	if _, err := runner.Run(); err != nil {
		t.Fatalf("stale marker blocked prune: %v", err)
	}
}

func TestPruneHoldsExclusionBeforeMutating(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	checked := false
	runner := NewRunner(root, 30, func() time.Time {
		checked = true
		writer, acquireErr := store.AcquireWriter()
		if writer != nil {
			_ = writer.Close()
		}
		if !errors.Is(acquireErr, events.ErrLocked) {
			t.Fatalf("writer acquired during prune: %v", acquireErr)
		}
		return time.Now()
	})
	if _, err := runner.Run(); err != nil {
		t.Fatalf("prune run: %v", err)
	}
	if !checked {
		t.Fatal("expected prune callback to run")
	}
}

func TestNoDataLossForCurrentReplayWindow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	eventStore, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	writer, err := eventStore.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	defer func() {
		_ = writer.Close()
	}()

	now := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	cutoffOlder := now.AddDate(0, 0, -50)
	cutoffEdge := now.AddDate(0, 0, -31)
	inside := now.AddDate(0, 0, -5)

	if _, err := writer.Append(events.Event{V: 1, TS: cutoffOlder, Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "older"}, StateHash: "sha256:1"}); err != nil {
		t.Fatalf("append older: %v", err)
	}
	if _, err := writer.Append(events.Event{V: 1, TS: cutoffEdge, Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "edge"}, StateHash: "sha256:2"}); err != nil {
		t.Fatalf("append edge: %v", err)
	}
	if _, err := writer.Append(events.Event{V: 1, TS: inside, Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "inside"}, StateHash: "sha256:3"}); err != nil {
		t.Fatalf("append inside: %v", err)
	}
	_ = writer.Close()

	runner := NewRunner(root, 30, func() time.Time { return now })
	if _, err := runner.Run(); err != nil {
		t.Fatalf("prune run: %v", err)
	}

	remaining, _, err := eventStore.ReadSince(0)
	if err != nil {
		t.Fatalf("read remaining: %v", err)
	}
	if len(remaining) < 2 {
		t.Fatalf("expected at least anchor+inside events preserved, got %d", len(remaining))
	}
	if remaining[len(remaining)-1].Patch["title"] != "inside" {
		t.Fatalf("expected latest event preserved, got %#v", remaining[len(remaining)-1].Patch)
	}
}
