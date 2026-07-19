package capture

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/diff"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
	"github.com/jmo/terminal-redeemer/internal/replay"
	"github.com/jmo/terminal-redeemer/internal/snapshots"
)

func TestCaptureOnceSuppressesUnchangedState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	eventStore, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	snapStore, err := snapshots.NewStore(root)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	state := model.State{
		Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}},
		Windows:    []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "shell"}},
	}

	collector := &sequenceCollector{states: []model.State{state, state}}
	runner := NewRunner(Config{
		Collector:     collector,
		DiffEngine:    diff.NewEngine(),
		EventStore:    eventStore,
		SnapshotStore: snapStore,
		SnapshotEvery: 100,
		Host:          "host-a",
		Profile:       "default",
		Source:        "test",
		Now:           func() time.Time { return time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC) },
		Logger:        io.Discard,
	})

	if _, err := runner.CaptureOnce(context.Background()); err != nil {
		t.Fatalf("capture once first: %v", err)
	}
	if _, err := runner.CaptureOnce(context.Background()); err != nil {
		t.Fatalf("capture once second: %v", err)
	}

	got, _, err := eventStore.ReadSince(0)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one change-only full-state event, got %d", len(got))
	}
	for i, event := range got {
		if event.EventType != "state_full" {
			t.Fatalf("event[%d] type = %q, want state_full", i, event.EventType)
		}
	}
}

func TestCaptureOnceFailureLeavesHistoryUntouchedAndNextRunRecovers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	eventStore, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	snapStore, err := snapshots.NewStore(root)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	recovered := model.State{
		Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}},
		Windows:    []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "recovered"}},
	}
	runner := NewRunner(Config{
		Collector: &sequenceCollector{sequence: []collectResult{
			{err: errors.New("temporary niri error")},
			{state: recovered},
		}},
		DiffEngine:    diff.NewEngine(),
		EventStore:    eventStore,
		SnapshotStore: snapStore,
		SnapshotEvery: 100,
		Host:          "host-a",
		Profile:       "default",
		Source:        "test",
	})

	if _, err := runner.CaptureOnce(context.Background()); err == nil {
		t.Fatal("expected first capture to fail")
	}
	got, _, err := eventStore.ReadSince(0)
	if err != nil {
		t.Fatalf("read history after failure: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("failed capture wrote %d events", len(got))
	}

	result, err := runner.CaptureOnce(context.Background())
	if err != nil {
		t.Fatalf("capture after recovery: %v", err)
	}
	if result.EventsWritten != 1 {
		t.Fatalf("recovered capture wrote %d events, want 1", result.EventsWritten)
	}
	got, _, err = eventStore.ReadSince(0)
	if err != nil {
		t.Fatalf("read recovered history: %v", err)
	}
	if len(got) != 1 || got[0].EventType != "state_full" {
		t.Fatalf("recovered history is not one full reconciliation: %#v", got)
	}
}

func TestCaptureRunLoopsAndContinuesOnRecoverableErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	eventStore, err := events.NewStore(root)
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	snapStore, err := snapshots.NewStore(root)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	stateA := model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "a", PID: 1}}}
	stateB := model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "b", PID: 2}}}

	var logs bytes.Buffer
	collector := &sequenceCollector{sequence: []collectResult{{state: stateA}, {err: errors.New("temporary niri error")}, {state: stateB}}}
	runner := NewRunner(Config{
		Collector:     collector,
		DiffEngine:    diff.NewEngine(),
		EventStore:    eventStore,
		SnapshotStore: snapStore,
		SnapshotEvery: 100,
		Host:          "host-a",
		Profile:       "default",
		Source:        "test",
		Now:           func() time.Time { return time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC) },
		Logger:        &logs,
	})

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() {
		done <- runner.CaptureRun(ctx, ticks)
	}()

	ticks <- time.Now()
	ticks <- time.Now()
	ticks <- time.Now()
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("capture run: %v", err)
	}

	got, _, err := eventStore.ReadSince(0)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events from successful ticks, got %d", len(got))
	}
	if !bytes.Contains(logs.Bytes(), []byte("capture_once_error")) {
		t.Fatalf("expected recoverable error log, got %q", logs.String())
	}
}

func TestWorkspaceOnlyChangePersistsThroughEventsSnapshotAndReplay(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bootSource := func() (string, error) { return "boot-placement", nil }
	eventStore, err := events.NewStoreWithBootIDSource(root, bootSource)
	if err != nil {
		t.Fatal(err)
	}
	snapStore, err := snapshots.NewStoreWithBootIDSource(root, bootSource)
	if err != nil {
		t.Fatal(err)
	}

	stateA := model.State{
		Workspaces: []model.Workspace{{ID: "runtime-8", Index: 2, Output: "DP-1"}},
		Windows:    []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "runtime-8", WorkspaceRef: &model.WorkspaceRef{Index: 2, Output: "DP-1"}}},
	}
	stateB := model.Normalize(stateA)
	stateB.Workspaces[0].Name = "work"
	stateB.Windows[0].WorkspaceRef.Name = "work"
	capturedAt := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	runner := NewRunner(Config{
		Collector: &sequenceCollector{states: []model.State{stateA, stateB}}, DiffEngine: diff.NewEngine(),
		EventStore: eventStore, SnapshotStore: snapStore, SnapshotEvery: 2,
		Host: "host-a", Profile: "default", Source: "test", Now: func() time.Time { return capturedAt },
	})

	if _, err := runner.captureDiff(context.Background()); err != nil {
		t.Fatalf("capture initial state: %v", err)
	}
	result, err := runner.captureDiff(context.Background())
	if err != nil {
		t.Fatalf("capture workspace-only change: %v", err)
	}
	if result.EventsWritten != 1 || result.SnapshotPath == "" {
		t.Fatalf("workspace-only result = %#v", result)
	}
	recorded, _, err := eventStore.ReadSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 2 || recorded[1].EventType != "state_full" {
		t.Fatalf("workspace-only event missing: %#v", recorded)
	}

	replayEngine, err := replay.NewEngine(root)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := replayEngine.At(capturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Workspaces[0].Name != "work" || replayed.Windows[0].WorkspaceRef == nil || replayed.Windows[0].WorkspaceRef.Name != "work" {
		t.Fatalf("workspace-only metadata lost during replay: %#v", replayed)
	}
}

func TestSnapshotCadenceHonored(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bootSource := func() (string, error) { return "boot-capture-test", nil }
	eventStore, err := events.NewStoreWithBootIDSource(root, bootSource)
	if err != nil {
		t.Fatalf("new event store: %v", err)
	}
	snapStore, err := snapshots.NewStoreWithBootIDSource(root, bootSource)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	stateA := model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "a", PID: 1}}}
	stateB := model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "b", PID: 2}}}
	stateC := model.State{Workspaces: []model.Workspace{{ID: "ws-1", Index: 1}}, Windows: []model.Window{{Key: "w-1", AppID: "kitty", WorkspaceID: "ws-1", Title: "c", PID: 3}}}

	collector := &sequenceCollector{states: []model.State{stateA, stateB, stateC}}
	runner := NewRunner(Config{
		Collector:     collector,
		DiffEngine:    diff.NewEngine(),
		EventStore:    eventStore,
		SnapshotStore: snapStore,
		SnapshotEvery: 2,
		Host:          "host-a",
		Profile:       "default",
		Source:        "test",
		Now:           func() time.Time { return time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC) },
		Logger:        io.Discard,
	})

	if _, err := runner.CaptureOnce(context.Background()); err != nil {
		t.Fatalf("capture 1: %v", err)
	}
	if _, err := runner.CaptureOnce(context.Background()); err != nil {
		t.Fatalf("capture 2: %v", err)
	}
	if _, err := runner.CaptureOnce(context.Background()); err != nil {
		t.Fatalf("capture 3: %v", err)
	}

	entries, err := filepath.Glob(filepath.Join(root, "snapshots", "*.json"))
	if err != nil {
		t.Fatalf("glob snapshots: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one snapshot at cadence 2, got %d", len(entries))
	}
	writtenEvents, _, err := eventStore.ReadSince(0)
	if err != nil {
		t.Fatalf("read captured events: %v", err)
	}
	for i, event := range writtenEvents {
		if event.BootID != "boot-capture-test" {
			t.Fatalf("event[%d] boot ID = %q", i, event.BootID)
		}
	}
	writtenSnapshot, err := snapStore.Read(entries[0])
	if err != nil {
		t.Fatalf("read captured snapshot: %v", err)
	}
	if writtenSnapshot.BootID != "boot-capture-test" {
		t.Fatalf("snapshot boot ID = %q", writtenSnapshot.BootID)
	}
}

type collectResult struct {
	state model.State
	err   error
}

type sequenceCollector struct {
	states   []model.State
	sequence []collectResult
	index    int
}

func (s *sequenceCollector) Collect(_ context.Context) (model.State, error) {
	if len(s.sequence) > 0 {
		if s.index >= len(s.sequence) {
			return s.sequence[len(s.sequence)-1].state, s.sequence[len(s.sequence)-1].err
		}
		result := s.sequence[s.index]
		s.index++
		return result.state, result.err
	}

	if s.index >= len(s.states) {
		return s.states[len(s.states)-1], nil
	}
	state := s.states[s.index]
	s.index++
	return state, nil
}
