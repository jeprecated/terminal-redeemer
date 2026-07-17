package resume

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jmo/terminal-redeemer/internal/model"
	"github.com/jmo/terminal-redeemer/internal/niri"
)

type SnapshotSource interface {
	Snapshot(context.Context) ([]byte, error)
}

type SnapshotObserver struct {
	Source SnapshotSource
}

func (o SnapshotObserver) Windows(ctx context.Context) ([]ObservedWindow, error) {
	if o.Source == nil {
		return nil, fmt.Errorf("Niri snapshot source is unavailable")
	}
	raw, err := o.Source.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	state, err := niri.ParseSnapshot(raw)
	if err != nil {
		return nil, err
	}
	out := make([]ObservedWindow, 0, len(state.Windows))
	for _, window := range state.Windows {
		id, err := runtimeWindowID(window.Key)
		if err != nil {
			continue
		}
		out = append(out, ObservedWindow{ID: id, PID: window.PID, AppID: window.AppID, WorkspaceID: window.WorkspaceID})
	}
	return out, nil
}

func runtimeWindowID(key string) (int, error) {
	idx := strings.LastIndex(key, ":")
	if idx < 0 || idx == len(key)-1 {
		return 0, fmt.Errorf("window key has no runtime ID: %q", key)
	}
	id, err := strconv.Atoi(key[idx+1:])
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("window key has invalid runtime ID: %q", key)
	}
	return id, nil
}

// ProcAttachmentProbe requires a live descendant whose argv is exactly
// zellij attach -- <session>. Seeing the launch command in Kitty's own argv is
// deliberately insufficient evidence.
type ProcAttachmentProbe struct {
	ProcRoot string
}

func (p ProcAttachmentProbe) Attached(_ context.Context, rootPID int, session string) (bool, error) {
	if rootPID <= 0 || strings.TrimSpace(session) == "" {
		return false, nil
	}
	root := strings.TrimSpace(p.ProcRoot)
	if root == "" {
		root = "/proc"
	}
	children, err := processChildren(root)
	if err != nil {
		return false, err
	}
	queue := append([]int(nil), children[rootPID]...)
	seen := map[int]struct{}{}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		args, err := readProcArgs(root, pid)
		if err == nil && len(args) == 4 && filepath.Base(args[0]) == "zellij" && args[1] == "attach" && args[2] == "--" && args[3] == session {
			return true, nil
		}
		queue = append(queue, children[pid]...)
	}
	return false, nil
}

func processChildren(root string) (map[int][]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read process table: %w", err)
	}
	children := make(map[int][]int)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(root, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		ppid, err := parentPID(string(payload))
		if err != nil {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	for ppid := range children {
		sort.Ints(children[ppid])
	}
	return children, nil
}

func parentPID(stat string) (int, error) {
	idx := strings.LastIndex(stat, ")")
	if idx < 0 || idx+2 >= len(stat) {
		return 0, fmt.Errorf("unexpected stat format")
	}
	fields := strings.Fields(stat[idx+2:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected stat fields")
	}
	return strconv.Atoi(fields[1])
}

func readProcArgs(root string, pid int) ([]string, error) {
	payload, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(payload), "\x00")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out, nil
}

type ActionRunner interface {
	Run(context.Context, string, ...string) error
}

type ExecActionRunner struct {
	Command string
}

func (r ExecActionRunner) Run(ctx context.Context, action string, args ...string) error {
	command := strings.TrimSpace(r.Command)
	if command == "" {
		command = "niri"
	}
	argv := []string{"msg", "action", action}
	argv = append(argv, args...)
	out, err := exec.CommandContext(ctx, command, argv...).CombinedOutput()
	if err == nil {
		return nil
	}
	if detail := strings.TrimSpace(string(out)); detail != "" {
		return fmt.Errorf("niri action %s failed: %w: %s", action, err, detail)
	}
	return fmt.Errorf("niri action %s failed: %w", action, err)
}

type NiriActions struct {
	Runner ActionRunner
}

func (a NiriActions) MoveToWorkspace(ctx context.Context, windowID int, target WorkspaceTarget) error {
	if windowID <= 0 || strings.TrimSpace(target.ID) == "" {
		return fmt.Errorf("invalid Niri workspace move")
	}
	// Niri's action accepts a workspace name or index, not its runtime ID.
	// The ID remains the post-action observation target.
	reference := strings.TrimSpace(target.Name)
	if reference == "" && target.Index > 0 {
		reference = strconv.Itoa(target.Index)
	}
	if reference == "" {
		return fmt.Errorf("resolved Niri workspace has no actionable name or index")
	}
	return a.runner().Run(ctx, "move-window-to-workspace", "--window-id", strconv.Itoa(windowID), reference)
}

func (a NiriActions) ApplyLayout(ctx context.Context, windowID int, placement model.Placement) LayoutResult {
	if windowID <= 0 {
		return LayoutResult{Status: LayoutDegraded, Reason: "invalid window ID for optional layout"}
	}
	requested := 0
	failures := make([]string, 0, 4)
	if placement.Column != nil {
		requested++
		failures = append(failures, "column order unsupported without focus/order heuristics")
	}
	if placement.IsFloating != nil {
		requested++
		action := "move-window-to-tiling"
		if *placement.IsFloating {
			action = "move-window-to-floating"
		}
		if err := a.runner().Run(ctx, action, "--id", strconv.Itoa(windowID)); err != nil {
			failures = append(failures, err.Error())
		}
	}

	width, height, haveSize := preferredSize(placement)
	if haveSize {
		requested += 2
		if err := a.runner().Run(ctx, "set-window-width", "--id", strconv.Itoa(windowID), strconv.Itoa(width)); err != nil {
			failures = append(failures, err.Error())
		}
		if err := a.runner().Run(ctx, "set-window-height", "--id", strconv.Itoa(windowID), strconv.Itoa(height)); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if requested == 0 {
		return LayoutResult{Status: LayoutNotRequested}
	}
	if len(failures) > 0 {
		return LayoutResult{Status: LayoutDegraded, Reason: strings.Join(failures, "; ")}
	}
	return LayoutResult{Status: LayoutApplied}
}

func (a NiriActions) runner() ActionRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return ExecActionRunner{Command: "niri"}
}

func preferredSize(placement model.Placement) (int, int, bool) {
	if placement.IsFloating != nil && *placement.IsFloating && len(placement.WindowSize) >= 2 {
		return placement.WindowSize[0], placement.WindowSize[1], placement.WindowSize[0] > 0 && placement.WindowSize[1] > 0
	}
	if len(placement.TileSize) >= 2 {
		width := int(math.Round(placement.TileSize[0]))
		height := int(math.Round(placement.TileSize[1]))
		return width, height, width > 0 && height > 0
	}
	if len(placement.WindowSize) >= 2 {
		return placement.WindowSize[0], placement.WindowSize[1], placement.WindowSize[0] > 0 && placement.WindowSize[1] > 0
	}
	return 0, 0, false
}
