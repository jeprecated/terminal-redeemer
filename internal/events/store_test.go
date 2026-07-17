package events

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreAppendAndReadInOrder(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})

	events := []Event{
		{V: 1, TS: time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "a"}, StateHash: "sha256:a"},
		{V: 1, TS: time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "b"}, StateHash: "sha256:b"},
	}

	for _, e := range events {
		if _, err := writer.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, _, err := store.ReadSince(0)
	if err != nil {
		t.Fatalf("read since: %v", err)
	}

	if len(got) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(got))
	}

	for i := range events {
		if got[i].StateHash != events[i].StateHash {
			t.Fatalf("event[%d] mismatch: want %q got %q", i, events[i].StateHash, got[i].StateHash)
		}
	}
}

func TestStoreRejectsMalformedEvent(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})

	bad := Event{V: 1, TS: time.Now().UTC(), Host: "host-a", Profile: "default", StateHash: "sha256:x"}
	if _, err := writer.Append(bad); err == nil {
		t.Fatal("expected malformed event error")
	}

	got, _, err := store.ReadSince(0)
	if err != nil {
		t.Fatalf("read since: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("expected 0 events, got %d", len(got))
	}
}

func TestStoreReplayCursorTracking(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})

	base := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	for i := range 3 {
		e := Event{V: 1, TS: base.Add(time.Duration(i) * time.Second), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "seed"}, StateHash: "sha256:seed"}
		if _, err := writer.Append(e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	firstBatch, cursor, err := store.ReadSince(0)
	if err != nil {
		t.Fatalf("read first batch: %v", err)
	}
	if len(firstBatch) != 3 {
		t.Fatalf("expected 3 events, got %d", len(firstBatch))
	}

	if _, err := writer.Append(Event{V: 1, TS: base.Add(4 * time.Second), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "new"}, StateHash: "sha256:new"}); err != nil {
		t.Fatalf("append incremental: %v", err)
	}

	secondBatch, nextCursor, err := store.ReadSince(cursor)
	if err != nil {
		t.Fatalf("read second batch: %v", err)
	}
	if len(secondBatch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(secondBatch))
	}
	if secondBatch[0].StateHash != "sha256:new" {
		t.Fatalf("expected new event hash, got %q", secondBatch[0].StateHash)
	}
	if nextCursor <= cursor {
		t.Fatalf("expected cursor advance: %d -> %d", cursor, nextCursor)
	}
}

func TestLockPreventsConcurrentWriters(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	writerA, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire first writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writerA.Close()
	})

	_, err = store.AcquireWriter()
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestAppendAddsBootIDAndLegacyRecordRemainsReadable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	legacy := `{"v":1,"ts":"2026-02-15T10:00:00Z","host":"host-a","profile":"default","event_type":"window_patch","window_key":"w-1","patch":{"title":"legacy"},"state_hash":"sha256:legacy"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "events.jsonl"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy log: %v", err)
	}
	store, err := NewStoreWithBootIDSource(root, func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	if _, err := writer.Append(Event{V: 1, TS: time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "new"}, StateHash: "sha256:new"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	got, _, err := store.ReadSince(0)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].BootID != "" {
		t.Fatalf("legacy event boot ID = %q, want empty", got[0].BootID)
	}
	if got[1].BootID != "boot-test" {
		t.Fatalf("new event boot ID = %q, want boot-test", got[1].BootID)
	}
}

func TestAppendAcknowledgesOnlyAfterSync(t *testing.T) {
	t.Parallel()

	store, err := NewStoreWithBootIDSource(t.TempDir(), func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	syncErr := errors.New("sync failed")
	called := false
	writer.syncFile = func(*os.File) error {
		called = true
		return syncErr
	}
	_, err = writer.Append(Event{V: 1, TS: time.Now().UTC(), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "x"}, StateHash: "sha256:x"})
	if !called {
		t.Fatal("expected append to synchronize the event file")
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("expected sync error, got %v", err)
	}
}

func TestReadSinceIgnoresOnlyMalformedTrailingRecord(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewStoreWithBootIDSource(root, func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	offset, err := writer.Append(Event{V: 1, TS: time.Now().UTC(), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "complete"}, StateHash: "sha256:x"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := appendRaw(filepath.Join(root, "events.jsonl"), `{"v":1,"ts":`); err != nil {
		t.Fatalf("append partial event: %v", err)
	}

	got, cursor, err := store.ReadSince(0)
	if err != nil {
		t.Fatalf("read with trailing corruption: %v", err)
	}
	if len(got) != 1 || got[0].Patch["title"] != "complete" {
		t.Fatalf("complete prefix not preserved: %#v", got)
	}
	if cursor != offset {
		t.Fatalf("cursor advanced over malformed tail: want %d got %d", offset, cursor)
	}

	if err := appendRaw(filepath.Join(root, "events.jsonl"), "\n"+legacyEventLine("after")); err != nil {
		t.Fatalf("append later event: %v", err)
	}
	if _, _, err := store.ReadSince(0); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected interior corruption error, got %v", err)
	}
}

func TestAcquireWriterRepairsMalformedTrailingRecord(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewStoreWithBootIDSource(root, func() (string, error) { return "boot-test", nil })
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer: %v", err)
	}
	first := Event{V: 1, TS: time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC), Host: "host-a", Profile: "default", EventType: "window_patch", WindowKey: "w-1", Patch: map[string]any{"title": "first"}, StateHash: "sha256:first"}
	if _, err := writer.Append(first); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close first writer: %v", err)
	}
	if err := appendRaw(filepath.Join(root, "events.jsonl"), `{"v":1,"ts":`); err != nil {
		t.Fatalf("append simulated crash tail: %v", err)
	}

	writer, err = store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer after crash: %v", err)
	}
	second := first
	second.TS = first.TS.Add(time.Second)
	second.Patch = map[string]any{"title": "second"}
	second.StateHash = "sha256:second"
	if _, err := writer.Append(second); err != nil {
		t.Fatalf("append after recovery: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close recovered writer: %v", err)
	}

	got, _, err := store.ReadSince(0)
	if err != nil {
		t.Fatalf("read repaired log: %v", err)
	}
	if len(got) != 2 || got[0].Patch["title"] != "first" || got[1].Patch["title"] != "second" {
		t.Fatalf("unexpected repaired events: %#v", got)
	}
}

func TestStaleLockFileDoesNotBlockWriter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "meta"), 0o755); err != nil {
		t.Fatalf("make meta dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meta", "lock"), []byte("stale pid 1234\n"), 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("stale marker blocked writer: %v", err)
	}
	_ = writer.Close()
}

func TestWriterLockReleasedAfterProcessDeath(t *testing.T) {
	if os.Getenv("REDEEM_LOCK_HELPER") == "1" {
		root := os.Getenv("REDEEM_LOCK_ROOT")
		store, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		writer, err := store.AcquireWriter()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = writer.Close() }()
		if err := os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}

	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWriterLockReleasedAfterProcessDeath$")
	cmd.Env = append(os.Environ(), "REDEEM_LOCK_HELPER=1", "REDEEM_LOCK_ROOT="+root)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lock holder: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	ready := filepath.Join(root, "ready")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("timed out waiting for child to acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill lock holder: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed child to exit unsuccessfully")
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store after crash: %v", err)
	}
	writer, err := store.AcquireWriter()
	if err != nil {
		t.Fatalf("acquire writer after holder death: %v", err)
	}
	_ = writer.Close()
}

func appendRaw(path string, payload string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(payload); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func legacyEventLine(title string) string {
	return `{"v":1,"ts":"2026-02-15T10:00:00Z","host":"host-a","profile":"default","event_type":"window_patch","window_key":"w-1","patch":{"title":"` + title + `"},"state_hash":"sha256:x"}` + "\n"
}
