package niri

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

type ipcSpikeState struct {
	Outputs    map[string]json.RawMessage `json:"outputs"`
	Workspaces []struct {
		ID     uint64  `json:"id"`
		Output *string `json:"output"`
	} `json:"workspaces"`
	Windows []struct {
		ID          uint64  `json:"id"`
		WorkspaceID *uint64 `json:"workspace_id"`
	} `json:"windows"`
}

func TestNiriIPCInitialReplayFixtureEndsAtConfigLoaded(t *testing.T) {
	file, err := os.Open("testdata/ipc-event-stream.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lines := make([]map[string]json.RawMessage, 0, 6)
	for scanner.Scan() {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("decode event line %d: %v", len(lines)+1, err)
		}
		lines = append(lines, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want event-stream reply plus five initial events", len(lines))
	}
	if string(lines[0]["Ok"]) != `"Handled"` {
		t.Fatalf("unexpected stream reply: %s", lines[0]["Ok"])
	}
	if _, ok := lines[1]["WorkspacesChanged"]; !ok {
		t.Fatalf("first initial event is not WorkspacesChanged: %v", lines[1])
	}
	if _, ok := lines[2]["WindowsChanged"]; !ok {
		t.Fatalf("second initial event is not WindowsChanged: %v", lines[2])
	}
	var configLoaded struct {
		Failed bool `json:"failed"`
	}
	if raw, ok := lines[len(lines)-1]["ConfigLoaded"]; !ok || len(lines[len(lines)-1]) != 1 || json.Unmarshal(raw, &configLoaded) != nil || configLoaded.Failed {
		t.Fatalf("initial replay does not end at successful ConfigLoaded: %v", lines[len(lines)-1])
	}
	adversarial, err := os.ReadFile("testdata/ipc-event-stream-failed-config.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	last := bytes.TrimSpace(adversarial)
	last = last[bytes.LastIndexByte(last, '\n')+1:]
	var failed map[string]struct {
		Failed bool `json:"failed"`
	}
	if err := json.Unmarshal(last, &failed); err != nil || !failed["ConfigLoaded"].Failed {
		t.Fatalf("adversarial fixture does not carry failed ConfigLoaded: %s err=%v", last, err)
	}
}

func TestNiriIPCCompleteAndDanglingStateFixtures(t *testing.T) {
	complete := readIPCSpikeState(t, "testdata/ipc-complete-state.json")
	if reasons := validateIPCSpikeState(complete); len(reasons) != 0 {
		t.Fatalf("complete fixture is degraded: %v", reasons)
	}

	dangling := readIPCSpikeState(t, "testdata/ipc-dangling-state.json")
	reasons := validateIPCSpikeState(dangling)
	if len(reasons) != 2 {
		t.Fatalf("got reasons %v, want missing output and missing workspace", reasons)
	}
	joined := strings.Join(reasons, "; ")
	if !strings.Contains(joined, "missing output") || !strings.Contains(joined, "missing workspace") {
		t.Fatalf("unexpected degradation reasons: %v", reasons)
	}
}

func TestNiriIPCMVPActionFixtureUsesExactBoundedShapes(t *testing.T) {
	file, err := os.Open("testdata/ipc-actions.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	wantVariants := []string{
		"SetWorkspaceName",
		"SetWorkspaceName",
		"MoveWindowToWorkspace",
		"MoveWindowToTiling",
		"MoveWindowToFloating",
		"SetWindowWidth",
		"SetWindowHeight",
	}
	wantBodies := []map[string]any{
		{"name": "tr-spike", "workspace": map[string]any{"Id": float64(2)}},
		{"name": "TR-SPIKE", "workspace": map[string]any{"Id": float64(3)}},
		{"window_id": float64(42), "reference": map[string]any{"Id": float64(2)}, "focus": false},
		{"id": float64(42)},
		{"id": float64(42)},
		{"id": float64(42), "change": map[string]any{"SetProportion": float64(45)}},
		{"id": float64(42), "change": map[string]any{"SetProportion": float64(40)}},
	}
	scanner := bufio.NewScanner(file)
	index := 0
	for scanner.Scan() {
		var envelope struct {
			Request struct {
				Action map[string]json.RawMessage `json:"Action"`
			} `json:"request"`
			Reply map[string]json.RawMessage `json:"reply"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			t.Fatalf("decode action line %d: %v", index+1, err)
		}
		if index >= len(wantVariants) {
			t.Fatalf("unexpected extra action line: %s", scanner.Text())
		}
		if _, ok := envelope.Request.Action[wantVariants[index]]; !ok {
			t.Fatalf("action %d does not contain %s: %v", index+1, wantVariants[index], envelope.Request.Action)
		}
		if string(envelope.Reply["Ok"]) != `"Handled"` {
			t.Fatalf("action %d has unexpected reply: %v", index+1, envelope.Reply)
		}
		var body map[string]any
		if err := json.Unmarshal(envelope.Request.Action[wantVariants[index]], &body); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(body, wantBodies[index]) {
			t.Fatalf("action %d body=%v want exact %v", index+1, body, wantBodies[index])
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(wantVariants) {
		t.Fatalf("got %d actions, want %d", index, len(wantVariants))
	}
}

func TestNiriIPCSourceInstanceFingerprintRotatesWithSocketIdentity(t *testing.T) {
	first := ipcSourceInstanceFingerprint("boot-a", 69, 100)
	second := ipcSourceInstanceFingerprint("boot-a", 69, 101)
	if first == second {
		t.Fatal("socket inode change did not rotate source instance")
	}
	if first != ipcSourceInstanceFingerprint("boot-a", 69, 100) {
		t.Fatal("same boot and socket identity is not stable")
	}
	if first == ipcSourceInstanceFingerprint("boot-b", 69, 100) {
		t.Fatal("boot change did not rotate source instance")
	}
}

func readIPCSpikeState(t *testing.T, path string) ipcSpikeState {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state ipcSpikeState
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return state
}

func validateIPCSpikeState(state ipcSpikeState) []string {
	workspaceIDs := make(map[uint64]struct{}, len(state.Workspaces))
	for _, workspace := range state.Workspaces {
		workspaceIDs[workspace.ID] = struct{}{}
	}
	reasons := make([]string, 0)
	for _, workspace := range state.Workspaces {
		if workspace.Output == nil {
			continue
		}
		if _, ok := state.Outputs[*workspace.Output]; !ok {
			reasons = append(reasons, fmt.Sprintf("workspace %d references missing output %s", workspace.ID, *workspace.Output))
		}
	}
	for _, window := range state.Windows {
		if window.WorkspaceID == nil {
			continue
		}
		if _, ok := workspaceIDs[*window.WorkspaceID]; !ok {
			reasons = append(reasons, fmt.Sprintf("window %d references missing workspace %d", window.ID, *window.WorkspaceID))
		}
	}
	return reasons
}

func ipcSourceInstanceFingerprint(bootID string, device uint64, inode uint64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", bootID, device, inode)))
	return hex.EncodeToString(digest[:])
}
