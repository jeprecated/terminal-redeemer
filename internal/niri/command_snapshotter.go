package niri

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type CommandRunner interface {
	Run(ctx context.Context, command string) ([]byte, error)
}

type ShellRunner struct{}

func (ShellRunner) Run(ctx context.Context, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	return cmd.Output()
}

type CommandSnapshotter struct {
	Command string
	Runner  CommandRunner
}

func (s CommandSnapshotter) Snapshot(ctx context.Context) ([]byte, error) {
	runner := s.Runner
	if runner == nil {
		runner = ShellRunner{}
	}
	out, err := runner.Run(ctx, s.Command)
	if err != nil {
		return nil, fmt.Errorf("run niri snapshot command: %w", err)
	}
	if !isWindowsCommand(s.Command) {
		return out, nil
	}

	workspaces, err := runner.Run(ctx, "niri msg -j workspaces")
	if err != nil {
		return nil, fmt.Errorf("run niri workspaces command: %w", err)
	}
	combined, err := combineSnapshotPayloads(workspaces, out)
	if err != nil {
		return nil, fmt.Errorf("combine niri snapshot payloads: %w", err)
	}
	return combined, nil
}

func isWindowsCommand(command string) bool {
	return strings.TrimSpace(command) == "niri msg -j windows"
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
