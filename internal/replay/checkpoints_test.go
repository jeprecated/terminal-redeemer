package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/events"
)

func TestListCheckpointsIncludesCompleteLegacyAndBootAwareStateOnly(t *testing.T) {
	root := t.TempDir()
	t0 := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	recorded := []events.Event{
		{V: 1, TS: t0, Host: "local", Profile: "default", EventType: "state_full", State: map[string]any{"workspaces": []any{}, "windows": []any{}}, StateHash: "sha256:a"},
		{V: 1, TS: t0.Add(time.Minute), Host: "local", Profile: "default", BootID: "boot-a", EventType: "window_patch", WindowKey: "w", Patch: map[string]any{"title": "patch"}, StateHash: "sha256:b"},
		{V: 1, TS: t0.Add(2 * time.Minute), Host: "local", Profile: "default", BootID: "boot-a", EventType: "state_full", State: map[string]any{"workspaces": []any{}, "windows": []any{map[string]any{"key": "w", "app_id": "kitty"}}}, StateHash: "sha256:c"},
	}
	var payload []byte
	for _, event := range recorded {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, line...)
		payload = append(payload, '\n')
	}
	if err := os.WriteFile(filepath.Join(root, "events.jsonl"), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ListCheckpoints(root)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("checkpoint count = %d, want 2: %#v", len(got), got)
	}
	if got[0].BootID != "" || got[1].BootID != "boot-a" || len(got[1].State.Windows) != 1 {
		t.Fatalf("checkpoints = %#v", got)
	}
}
