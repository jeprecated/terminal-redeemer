package niri

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const DefaultSnapshotCommand = "niri msg -j windows"

// CommandRunner executes one program with explicit argv. The distributed Niri
// query never crosses a shell; only explicitly configured compatibility
// commands use `sh -c`.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type CommandSnapshotter struct {
	Command string
	Runner  CommandRunner
}

func (s CommandSnapshotter) Snapshot(ctx context.Context) ([]byte, error) {
	runner := s.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if !isDefaultWindowsCommand(s.Command) {
		out, err := runner.Run(ctx, "sh", "-c", s.Command)
		if err != nil {
			return nil, commandError("run custom Niri snapshot command", out, err)
		}
		return out, nil
	}

	windows, err := runner.Run(ctx, "niri", "msg", "-j", "windows")
	if err != nil {
		return nil, commandError("run Niri windows query", windows, err)
	}
	workspaces, err := runner.Run(ctx, "niri", "msg", "-j", "workspaces")
	if err != nil {
		return nil, commandError("run Niri workspaces query", workspaces, err)
	}
	combined, err := combineSnapshotPayloads(workspaces, windows)
	if err != nil {
		return nil, fmt.Errorf("combine Niri snapshot payloads: %w", err)
	}
	return combined, nil
}

func commandError(operation string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail != "" {
		return fmt.Errorf("%s: %w: %s", operation, err, detail)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func isDefaultWindowsCommand(command string) bool {
	return strings.TrimSpace(command) == DefaultSnapshotCommand
}

func combineSnapshotPayloads(workspaces []byte, windows []byte) ([]byte, error) {
	var workspacesPayload []any
	if err := json.Unmarshal(workspaces, &workspacesPayload); err != nil {
		return nil, err
	}
	var windowsPayload []any
	if err := json.Unmarshal(windows, &windowsPayload); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"workspaces": workspacesPayload,
		"windows":    windowsPayload,
	})
}
