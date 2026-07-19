package capture

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/checkpoints"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
)

type collectorFunc func(context.Context) (model.State, error)

func (f collectorFunc) Collect(ctx context.Context) (model.State, error) { return f(ctx) }

func captureState(title string) model.State {
	return model.State{
		Workspaces: []model.Workspace{{ID: "ws", Index: 1}},
		Windows:    []model.Window{{Key: "window", AppID: "kitty", WorkspaceID: "ws", Title: title}},
	}
}

func runnerFor(state model.State, eventStore EventStore, checkpointStore CheckpointStore, now func() time.Time) *Runner {
	return NewRunner(Config{
		Collector:  collectorFunc(func(context.Context) (model.State, error) { return state, nil }),
		EventStore: eventStore, CheckpointStore: checkpointStore,
		SnapshotEvery: 100, Host: "host", Profile: "default", Source: "test", Now: now,
	})
}

func TestUnchangedSuppressionAcrossIndependentRunners(t *testing.T) {
	root := t.TempDir()
	boot := func() (string, error) { return "boot-a", nil }
	firstEvents, err := events.NewStoreWithBootIDSource(root, boot)
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := checkpoints.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	state := captureState("same")
	first := runnerFor(state, firstEvents, rolling, func() time.Time { return now })
	if result, err := first.CaptureOnce(context.Background()); err != nil || result.EventsWritten != 1 {
		t.Fatalf("first capture result=%#v err=%v", result, err)
	}

	secondEvents, err := events.NewStoreWithBootIDSource(root, boot)
	if err != nil {
		t.Fatal(err)
	}
	secondRolling, err := checkpoints.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(20 * time.Minute)
	second := runnerFor(state, secondEvents, secondRolling, func() time.Time { return later })
	if result, err := second.CaptureOnce(context.Background()); err != nil || result.EventsWritten != 0 {
		t.Fatalf("independent unchanged capture result=%#v err=%v", result, err)
	}
	recorded, _, err := secondEvents.ReadSince(0)
	if err != nil || len(recorded) != 1 {
		t.Fatalf("events=%#v err=%v", recorded, err)
	}
	checkpoint, err := secondRolling.Read("boot-a", "host", "default")
	if err != nil || !checkpoint.ObservedAt.Equal(later) {
		t.Fatalf("rolling freshness=%#v err=%v", checkpoint, err)
	}
}

func TestTitleOnlyChangeSuppressesEventAndRefreshesStoredTitle(t *testing.T) {
	root := t.TempDir()
	boot := func() (string, error) { return "boot-a", nil }
	eventStore, err := events.NewStoreWithBootIDSource(root, boot)
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := checkpoints.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if result, err := runnerFor(captureState("running ⠐"), eventStore, rolling, func() time.Time { return now }).CaptureOnce(context.Background()); err != nil || result.EventsWritten != 1 {
		t.Fatalf("first capture result=%#v err=%v", result, err)
	}
	later := now.Add(time.Minute)
	if result, err := runnerFor(captureState("running ⠂"), eventStore, rolling, func() time.Time { return later }).CaptureOnce(context.Background()); err != nil || result.EventsWritten != 0 {
		t.Fatalf("title-only capture result=%#v err=%v", result, err)
	}
	recorded, _, err := eventStore.ReadSince(0)
	if err != nil || len(recorded) != 1 {
		t.Fatalf("events=%#v err=%v", recorded, err)
	}
	checkpoint, err := rolling.Read("boot-a", "host", "default")
	if err != nil {
		t.Fatal(err)
	}
	if !checkpoint.ObservedAt.Equal(later) || checkpoint.State.Windows[0].Title != "running ⠂" {
		t.Fatalf("rolling checkpoint did not retain latest observation: %#v", checkpoint)
	}
}

func TestFirstCapturePerBootAndChangedOrEmptyState(t *testing.T) {
	root := t.TempDir()
	currentBoot := "boot-a"
	boot := func() (string, error) { return currentBoot, nil }
	eventStore, err := events.NewStoreWithBootIDSource(root, boot)
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := checkpoints.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	stateA := captureState("a")
	stateB := captureState("b")
	stateB.Windows[0].PID = 42
	for _, state := range []model.State{stateA, stateB, {Workspaces: []model.Workspace{}, Windows: []model.Window{}}} {
		result, err := runnerFor(state, eventStore, rolling, func() time.Time { now = now.Add(time.Minute); return now }).CaptureOnce(context.Background())
		if err != nil || result.EventsWritten != 1 {
			t.Fatalf("changed capture result=%#v err=%v", result, err)
		}
	}
	// The same normalized empty state is still the first durable state for a
	// new boot, even though it matches the preceding boot.
	currentBoot = "boot-b"
	result, err := runnerFor(model.State{Workspaces: []model.Workspace{}, Windows: []model.Window{}}, eventStore, rolling, func() time.Time { return now.Add(time.Minute) }).CaptureOnce(context.Background())
	if err != nil || result.EventsWritten != 1 {
		t.Fatalf("new-boot capture result=%#v err=%v", result, err)
	}
	recorded, _, err := eventStore.ReadSince(0)
	if err != nil || len(recorded) != 4 || recorded[3].BootID != "boot-b" {
		t.Fatalf("events=%#v err=%v", recorded, err)
	}
}

func TestEventNewerThanCheckpointSuppressesDuplicateAndRepairs(t *testing.T) {
	root := t.TempDir()
	boot := func() (string, error) { return "boot-a", nil }
	eventStore, err := events.NewStoreWithBootIDSource(root, boot)
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := checkpoints.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if _, err := runnerFor(captureState("a"), eventStore, rolling, func() time.Time { return t0 }).CaptureOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	stateB := model.Normalize(captureState("b"))
	hashB, _ := stateB.Hash()
	writer, err := eventStore.AcquireWriter()
	if err != nil {
		t.Fatal(err)
	}
	newOffset, err := writer.Append(events.Event{V: 1, TS: t0.Add(time.Minute), Host: "host", Profile: "default", EventType: "state_full", State: stateAsMap(stateB), StateHash: hashB})
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()

	result, err := runnerFor(stateB, eventStore, rolling, func() time.Time { return t0.Add(2 * time.Minute) }).CaptureOnce(context.Background())
	if err != nil || result.EventsWritten != 0 {
		t.Fatalf("repair result=%#v err=%v", result, err)
	}
	recorded, _, _ := eventStore.ReadSince(0)
	if len(recorded) != 2 {
		t.Fatalf("repair appended duplicate: %#v", recorded)
	}
	checkpoint, err := rolling.Read("boot-a", "host", "default")
	if err != nil || checkpoint.EventOffset != newOffset || checkpoint.StateHash != hashB {
		t.Fatalf("checkpoint not repaired: %#v err=%v", checkpoint, err)
	}
}

type failOnceCheckpointStore struct {
	store  *checkpoints.Store
	mu     sync.Mutex
	failed bool
}

func (s *failOnceCheckpointStore) Read(bootID, host, profile string) (checkpoints.Checkpoint, error) {
	return s.store.Read(bootID, host, profile)
}
func (s *failOnceCheckpointStore) Write(checkpoint checkpoints.Checkpoint) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.failed {
		s.failed = true
		return "", errors.New("simulated checkpoint publication failure")
	}
	return s.store.Write(checkpoint)
}

func TestCheckpointFailureAfterEventIsRepairedWithoutDuplicate(t *testing.T) {
	root := t.TempDir()
	boot := func() (string, error) { return "boot-a", nil }
	eventStore, _ := events.NewStoreWithBootIDSource(root, boot)
	rolling, _ := checkpoints.NewStore(root)
	failing := &failOnceCheckpointStore{store: rolling}
	state := captureState("durable")
	t0 := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if _, err := runnerFor(state, eventStore, failing, func() time.Time { return t0 }).CaptureOnce(context.Background()); err == nil {
		t.Fatal("expected checkpoint publication failure")
	}
	recorded, _, _ := eventStore.ReadSince(0)
	if len(recorded) != 1 {
		t.Fatalf("durable event missing: %#v", recorded)
	}
	result, err := runnerFor(state, eventStore, failing, func() time.Time { return t0.Add(time.Minute) }).CaptureOnce(context.Background())
	if err != nil || result.EventsWritten != 0 {
		t.Fatalf("repair result=%#v err=%v", result, err)
	}
	recorded, _, _ = eventStore.ReadSince(0)
	if len(recorded) != 1 {
		t.Fatalf("repair duplicated durable event: %#v", recorded)
	}
}

func TestCorruptCheckpointFallsBackToEvent(t *testing.T) {
	root := t.TempDir()
	boot := func() (string, error) { return "boot-a", nil }
	eventStore, _ := events.NewStoreWithBootIDSource(root, boot)
	rolling, _ := checkpoints.NewStore(root)
	state := captureState("same")
	t0 := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if _, err := runnerFor(state, eventStore, rolling, func() time.Time { return t0 }).CaptureOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := rolling.Path("boot-a", "host", "default")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err := runnerFor(state, eventStore, rolling, func() time.Time { return t0.Add(time.Minute) }).CaptureOnce(context.Background())
	if err != nil || result.EventsWritten != 0 {
		t.Fatalf("missing fallback result=%#v err=%v", result, err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = runnerFor(state, eventStore, rolling, func() time.Time { return t0.Add(2 * time.Minute) }).CaptureOnce(context.Background())
	if err != nil || result.EventsWritten != 0 {
		t.Fatalf("corrupt fallback result=%#v err=%v", result, err)
	}
	if _, err := rolling.Read("boot-a", "host", "default"); err != nil {
		t.Fatalf("corrupt checkpoint was not repaired: %v", err)
	}
}

func TestConcurrentCapturesCannotDuplicateEvent(t *testing.T) {
	root := t.TempDir()
	boot := func() (string, error) { return "boot-a", nil }
	storeA, _ := events.NewStoreWithBootIDSource(root, boot)
	storeB, _ := events.NewStoreWithBootIDSource(root, boot)
	rollingA, _ := checkpoints.NewStore(root)
	rollingB, _ := checkpoints.NewStore(root)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	collector := collectorFunc(func(context.Context) (model.State, error) {
		ready <- struct{}{}
		<-release
		return captureState("same"), nil
	})
	newRunner := func(store EventStore, rolling CheckpointStore) *Runner {
		return NewRunner(Config{Collector: collector, EventStore: store, CheckpointStore: rolling, Host: "host", Profile: "default", Source: "test"})
	}
	results := make(chan error, 2)
	for _, runner := range []*Runner{newRunner(storeA, rollingA), newRunner(storeB, rollingB)} {
		go func(runner *Runner) { _, err := runner.CaptureOnce(context.Background()); results <- err }(runner)
	}
	<-ready
	<-ready
	close(release)
	errA, errB := <-results, <-results
	if errA != nil && errB != nil {
		t.Fatalf("both concurrent captures failed: %v / %v", errA, errB)
	}
	recorded, _, err := storeA.ReadSince(0)
	if err != nil || len(recorded) != 1 {
		t.Fatalf("concurrent events=%#v err=%v", recorded, err)
	}
}
