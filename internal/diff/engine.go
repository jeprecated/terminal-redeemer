package diff

import (
	"reflect"
	"sort"

	"github.com/jmo/terminal-redeemer/internal/model"
)

type Patch struct {
	WindowKey string         `json:"window_key,omitempty"`
	Fields    map[string]any `json:"patch,omitempty"`
	State     *model.State   `json:"state,omitempty"`
}

type Engine struct{}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) Diff(before model.State, after model.State) ([]Patch, bool, error) {
	beforeHash, err := before.Hash()
	if err != nil {
		return nil, false, err
	}
	afterHash, err := after.Hash()
	if err != nil {
		return nil, false, err
	}
	if beforeHash == afterHash {
		return nil, false, nil
	}

	beforeNorm := model.Normalize(before)
	afterNorm := model.Normalize(after)

	// Workspace metadata has state-wide meaning. A full-state patch keeps
	// workspace-only changes and mixed workspace/window changes atomic and
	// replayable instead of emitting a new hash with no applicable patch.
	if !reflect.DeepEqual(beforeNorm.Workspaces, afterNorm.Workspaces) {
		return []Patch{{State: &afterNorm}}, true, nil
	}

	beforeByKey := make(map[string]model.Window, len(beforeNorm.Windows))
	for _, window := range beforeNorm.Windows {
		beforeByKey[window.Key] = window
	}
	afterByKey := make(map[string]model.Window, len(afterNorm.Windows))
	for _, window := range afterNorm.Windows {
		afterByKey[window.Key] = window
	}

	keys := make([]string, 0, len(beforeByKey)+len(afterByKey))
	seen := make(map[string]struct{}, len(beforeByKey)+len(afterByKey))
	for key := range beforeByKey {
		keys = append(keys, key)
		seen[key] = struct{}{}
	}
	for key := range afterByKey {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	patches := make([]Patch, 0)
	for _, key := range keys {
		beforeWindow, hadBefore := beforeByKey[key]
		afterWindow, hadAfter := afterByKey[key]

		if hadAfter && !hadBefore {
			patches = append(patches, Patch{
				WindowKey: key,
				Fields: map[string]any{
					"app_id":        afterWindow.AppID,
					"workspace_id":  afterWindow.WorkspaceID,
					"workspace_ref": afterWindow.WorkspaceRef,
					"pid":           afterWindow.PID,
					"title":         afterWindow.Title,
					"placement":     afterWindow.Placement,
					"terminal":      afterWindow.Terminal,
				},
			})
			continue
		}

		if hadBefore && !hadAfter {
			patches = append(patches, Patch{
				WindowKey: key,
				Fields: map[string]any{
					"deleted": true,
				},
			})
			continue
		}

		fields := diffWindowFields(beforeWindow, afterWindow)
		if len(fields) == 0 {
			continue
		}
		patches = append(patches, Patch{WindowKey: key, Fields: fields})
	}

	return patches, len(patches) > 0, nil
}

func diffWindowFields(before model.Window, after model.Window) map[string]any {
	patch := make(map[string]any)

	if before.AppID != after.AppID {
		patch["app_id"] = after.AppID
	}
	if before.WorkspaceID != after.WorkspaceID {
		patch["workspace_id"] = after.WorkspaceID
	}
	if !reflect.DeepEqual(before.WorkspaceRef, after.WorkspaceRef) {
		patch["workspace_ref"] = after.WorkspaceRef
	}
	if before.PID != after.PID {
		patch["pid"] = after.PID
	}
	if before.Title != after.Title {
		patch["title"] = after.Title
	}
	if !reflect.DeepEqual(before.Placement, after.Placement) {
		patch["placement"] = after.Placement
	}
	if !terminalEqual(before.Terminal, after.Terminal) {
		if after.Terminal == nil {
			patch["terminal"] = nil
		} else {
			patch["terminal"] = after.Terminal
		}
	}

	return patch
}

func terminalEqual(a, b *model.Terminal) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.CWD != b.CWD || a.SessionTag != b.SessionTag {
		return false
	}
	if len(a.ProcessTags) != len(b.ProcessTags) {
		return false
	}
	for i := range a.ProcessTags {
		if a.ProcessTags[i] != b.ProcessTags[i] {
			return false
		}
	}
	return true
}
