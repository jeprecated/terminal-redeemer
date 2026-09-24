package mirror

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSourceHelperTimeout = 15 * time.Second
	DefaultLocalAttachTimeout  = 10 * time.Second
	DefaultSourcePollInterval  = 100 * time.Millisecond
)

// ValidateWorkspaceReference accepts a bounded Niri workspace name or positive
// numeric index. It is passed as argv locally and shell-quoted across SSH.
func ValidateWorkspaceReference(reference string) error {
	if reference == "" {
		return nil
	}
	if strings.TrimSpace(reference) != reference || len(reference) > 128 || strings.HasPrefix(reference, "-") {
		return fmt.Errorf("invalid workspace reference %q", reference)
	}
	for _, r := range reference {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid workspace reference %q", reference)
		}
	}
	if index, err := strconv.Atoi(reference); err == nil && index <= 0 {
		return fmt.Errorf("workspace index must be positive")
	}
	return nil
}

type SourceAttachConfig struct {
	SourceHost      string
	SSHCommand      string
	SSHOptions      []string
	SnapshotCommand []string
	Session         string
	Workspace       string
}

// PlanSourceAttach derives the source-side Redeem invocation only from a
// configured command ending in the exact `mirror snapshot` suffix. Prefixes
// such as wrappers or absolute executable paths are preserved unchanged.
func PlanSourceAttach(cfg SourceAttachConfig) (Command, error) {
	if err := ValidateDestination(cfg.SourceHost); err != nil {
		return Command{}, err
	}
	if !generatedSessionPattern.MatchString(cfg.Session) {
		return Command{}, fmt.Errorf("invalid generated mirror session name %q", cfg.Session)
	}
	if err := ValidateWorkspaceReference(cfg.Workspace); err != nil {
		return Command{}, err
	}
	if strings.TrimSpace(cfg.SSHCommand) == "" {
		return Command{}, fmt.Errorf("SSH command must not be empty")
	}
	remoteArgv, err := sourceAttachArgv(cfg.SnapshotCommand, cfg.Session, cfg.Workspace)
	if err != nil {
		return Command{}, err
	}
	args, err := buildSSHArgs(cfg.SSHOptions, nil, cfg.SourceHost, QuoteCommand(remoteArgv))
	if err != nil {
		return Command{}, err
	}
	return Command{Name: cfg.SSHCommand, Args: args}, nil
}

func sourceAttachArgv(snapshotCommand []string, session string, workspace string) ([]string, error) {
	args := []string{"--session", session}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	return sourceHelperArgv(snapshotCommand, "attach-local", args...)
}
