package mirror

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Command is an argv-based process request. Stdin may contain binary data.
type Command struct {
	Name  string
	Args  []string
	Stdin []byte
}

// Runner isolates all SSH, Niri, Kitty, and clipboard process execution.
type Runner interface {
	Output(context.Context, Command) ([]byte, error)
	Run(context.Context, Command) error
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, request Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, request.Name, request.Args...)
	cmd.Stdin = bytes.NewReader(request.Stdin)
	output, err := cmd.Output()
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return nil, fmt.Errorf("%s: %w: %s", request.Name, err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return nil, fmt.Errorf("%s: %w", request.Name, err)
}

func (ExecRunner) Run(ctx context.Context, request Command) error {
	cmd := exec.CommandContext(ctx, request.Name, request.Args...)
	cmd.Stdin = bytes.NewReader(request.Stdin)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if len(output) > 0 {
		return fmt.Errorf("%s: %w: %s", request.Name, err, strings.TrimSpace(string(output)))
	}
	return fmt.Errorf("%s: %w", request.Name, err)
}
