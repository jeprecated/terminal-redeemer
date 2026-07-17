package snapshots

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	want := Snapshot{
		V:               1,
		CreatedAt:       time.Date(2026, 2, 15, 10, 20, 0, 0, time.UTC),
		Host:            "host-a",
		Profile:         "default",
		LastEventOffset: 123,
		StateHash:       "sha256:snap",
		State: map[string]any{
			"windows": map[string]any{"w:1": map[string]any{"workspace_idx": 2}},
		},
	}

	path, err := store.Write(want)
	if err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	got, err := store.Read(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	if got.LastEventOffset != want.LastEventOffset {
		t.Fatalf("last_event_offset mismatch: want %d got %d", want.LastEventOffset, got.LastEventOffset)
	}
	if got.StateHash != want.StateHash {
		t.Fatalf("state_hash mismatch: want %q got %q", want.StateHash, got.StateHash)
	}
}

func TestLoadNearestAtOrBeforeTimestamp(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	base := time.Date(2026, 2, 15, 10, 20, 0, 0, time.UTC)
	times := []time.Time{base, base.Add(10 * time.Minute), base.Add(20 * time.Minute)}

	for i, ts := range times {
		_, err := store.Write(Snapshot{
			V:               1,
			CreatedAt:       ts,
			Host:            "host-a",
			Profile:         "default",
			LastEventOffset: int64(i + 1),
			StateHash:       "sha256:snap",
			State:           map[string]any{"i": i},
		})
		if err != nil {
			t.Fatalf("write snapshot %d: %v", i, err)
		}
	}

	got, gotPath, err := store.LoadNearest(base.Add(15 * time.Minute))
	if err != nil {
		t.Fatalf("load nearest: %v", err)
	}

	if got.LastEventOffset != 2 {
		t.Fatalf("expected offset 2, got %d", got.LastEventOffset)
	}

	if filepath.Base(gotPath) != "1771151400.json" {
		t.Fatalf("unexpected snapshot path: %s", gotPath)
	}
}

func TestSnapshotAddsBootIDAndReadsLegacySnapshot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewStoreWithBootIDSource(root, func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	legacyPath := filepath.Join(root, "snapshots", "1771150800.json")
	legacy := `{"v":1,"created_at":"2026-02-15T10:20:00Z","host":"host-a","profile":"default","last_event_offset":1,"state":{},"state_hash":"sha256:legacy"}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}
	legacySnapshot, err := store.Read(legacyPath)
	if err != nil {
		t.Fatalf("read legacy snapshot: %v", err)
	}
	if legacySnapshot.BootID != "" {
		t.Fatalf("legacy boot ID = %q, want empty", legacySnapshot.BootID)
	}

	path, err := store.Write(Snapshot{V: 1, CreatedAt: time.Date(2026, 2, 15, 10, 21, 0, 0, time.UTC), Host: "host-a", Profile: "default", LastEventOffset: 2, State: map[string]any{}, StateHash: "sha256:new"})
	if err != nil {
		t.Fatalf("write new snapshot: %v", err)
	}
	got, err := store.Read(path)
	if err != nil {
		t.Fatalf("read new snapshot: %v", err)
	}
	if got.BootID != "boot-test" {
		t.Fatalf("new boot ID = %q, want boot-test", got.BootID)
	}
}

func TestSnapshotWriteSyncsFileBeforeRenameAndDirectory(t *testing.T) {
	t.Parallel()

	store, err := NewStoreWithBootIDSource(t.TempDir(), func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	var calls []string
	store.syncFile = func(file *os.File) error {
		calls = append(calls, "file_sync")
		return file.Sync()
	}
	store.rename = func(oldPath, newPath string) error {
		calls = append(calls, "rename")
		return os.Rename(oldPath, newPath)
	}
	store.syncDir = func(path string) error {
		calls = append(calls, "dir_sync")
		return syncDirectory(path)
	}

	_, err = store.Write(Snapshot{V: 1, CreatedAt: time.Now().UTC(), Host: "host-a", Profile: "default", LastEventOffset: 1, State: map[string]any{}, StateHash: "sha256:x"})
	if err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	if want := []string{"file_sync", "rename", "dir_sync"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("durability calls = %v, want %v", calls, want)
	}
	matches, err := filepath.Glob(filepath.Join(store.dir, ".snapshot-*.tmp"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary snapshots left behind: %v", matches)
	}
}

func TestSnapshotSyncFailureDoesNotReplaceExistingSnapshot(t *testing.T) {
	t.Parallel()

	store, err := NewStoreWithBootIDSource(t.TempDir(), func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ts := time.Date(2026, 2, 15, 10, 20, 0, 0, time.UTC)
	path, err := store.Write(Snapshot{V: 1, CreatedAt: ts, Host: "host-a", Profile: "default", LastEventOffset: 1, State: map[string]any{"value": "old"}, StateHash: "sha256:old"})
	if err != nil {
		t.Fatalf("write initial snapshot: %v", err)
	}
	oldPayload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read initial snapshot: %v", err)
	}
	syncErr := errors.New("sync failed")
	store.syncFile = func(*os.File) error { return syncErr }
	_, err = store.Write(Snapshot{V: 1, CreatedAt: ts, Host: "host-a", Profile: "default", LastEventOffset: 2, State: map[string]any{"value": "new"}, StateHash: "sha256:new"})
	if !errors.Is(err, syncErr) {
		t.Fatalf("expected sync failure, got %v", err)
	}
	gotPayload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved snapshot: %v", err)
	}
	if !reflect.DeepEqual(gotPayload, oldPayload) {
		t.Fatal("failed replacement changed the existing snapshot")
	}
}

func TestShouldSnapshotCadence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		events int
		every  int
		want   bool
	}{
		{events: 0, every: 100, want: false},
		{events: 1, every: 100, want: false},
		{events: 100, every: 100, want: true},
		{events: 200, every: 100, want: true},
		{events: 201, every: 100, want: false},
		{events: 10, every: 0, want: false},
	}

	for _, tc := range testCases {
		got := ShouldSnapshot(tc.events, tc.every)
		if got != tc.want {
			t.Fatalf("events=%d every=%d: want %v got %v", tc.events, tc.every, tc.want, got)
		}
	}
}
