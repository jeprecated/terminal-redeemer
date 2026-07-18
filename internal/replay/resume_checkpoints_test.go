package replay

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/checkpoints"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
)

func resumeState(title string) model.State {
	return model.State{Workspaces: []model.Workspace{}, Windows: []model.Window{{Key: "w", AppID: "kitty", Title: title}}}
}

func stateMap(t *testing.T, state model.State) map[string]any {
	t.Helper()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func appendFull(t *testing.T, writer *events.Writer, at time.Time, state model.State) int64 {
	t.Helper()
	hash, err := state.Hash()
	if err != nil {
		t.Fatal(err)
	}
	offset, err := writer.Append(events.Event{V: 1, TS: at, Host: "host", Profile: "default", EventType: "state_full", State: stateMap(t, state), StateHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	return offset
}

func TestResumeCheckpointUsesRollingObservationFreshness(t *testing.T) {
	root := t.TempDir()
	eventStore, _ := events.NewStoreWithBootIDSource(root, func() (string, error) { return "boot-a", nil })
	writer, _ := eventStore.AcquireWriter()
	t0 := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	state := resumeState("same")
	offset := appendFull(t, writer, t0, state)
	_ = writer.Close()
	rolling, _ := checkpoints.NewStore(root)
	hash, _ := state.Hash()
	observed := t0.Add(45 * time.Minute)
	if _, err := rolling.Write(checkpoints.Checkpoint{V: 1, BootID: "boot-a", Host: "host", Profile: "default", ObservedAt: observed, State: state, StateHash: hash, EventOffset: offset}); err != nil {
		t.Fatal(err)
	}

	got, err := ListResumeCheckpoints(root)
	if err != nil || len(got) != 1 {
		t.Fatalf("checkpoints=%#v err=%v", got, err)
	}
	if !got[0].CapturedAt.Equal(observed) || got[0].State.Windows[0].Title != "same" {
		t.Fatalf("rolling freshness not selected: %#v", got[0])
	}
}

func TestResumeCheckpointNewerEventWinsPublicationLag(t *testing.T) {
	root := t.TempDir()
	eventStore, _ := events.NewStoreWithBootIDSource(root, func() (string, error) { return "boot-a", nil })
	writer, _ := eventStore.AcquireWriter()
	t0 := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	stateA := resumeState("a")
	offsetA := appendFull(t, writer, t0, stateA)
	rolling, _ := checkpoints.NewStore(root)
	hashA, _ := stateA.Hash()
	if _, err := rolling.Write(checkpoints.Checkpoint{V: 1, BootID: "boot-a", Host: "host", Profile: "default", ObservedAt: t0.Add(time.Minute), State: stateA, StateHash: hashA, EventOffset: offsetA}); err != nil {
		t.Fatal(err)
	}
	stateB := resumeState("b")
	appendFull(t, writer, t0.Add(2*time.Minute), stateB)
	_ = writer.Close()

	got, err := ListResumeCheckpoints(root)
	if err != nil || len(got) != 1 {
		t.Fatalf("checkpoints=%#v err=%v", got, err)
	}
	if got[0].State.Windows[0].Title != "b" || !got[0].CapturedAt.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("new durable event did not win: %#v", got[0])
	}
}

func TestResumeCheckpointCorruptionFallsBackToBootAwareEvent(t *testing.T) {
	root := t.TempDir()
	eventStore, _ := events.NewStoreWithBootIDSource(root, func() (string, error) { return "boot-a", nil })
	writer, _ := eventStore.AcquireWriter()
	t0 := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	appendFull(t, writer, t0, resumeState("event"))
	_ = writer.Close()
	rolling, _ := checkpoints.NewStore(root)
	if err := os.WriteFile(rolling.Path("boot-a", "host", "default"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ListResumeCheckpoints(root)
	if err != nil || len(got) != 1 || got[0].State.Windows[0].Title != "event" {
		t.Fatalf("event fallback=%#v err=%v", got, err)
	}
}
