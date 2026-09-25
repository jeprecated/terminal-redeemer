package mirror

import (
	"context"
	"fmt"
	"strings"
)

func sourceHelperArgv(snapshotCommand []string, helper string, args ...string) ([]string, error) {
	if len(snapshotCommand) < 3 || snapshotCommand[len(snapshotCommand)-2] != "mirror" || snapshotCommand[len(snapshotCommand)-1] != "snapshot" {
		return nil, fmt.Errorf("mirror.snapshotCommand must end with exact argv suffix `mirror snapshot` for source helpers")
	}
	if strings.TrimSpace(snapshotCommand[0]) == "" {
		return nil, fmt.Errorf("mirror.snapshotCommand has no executable prefix")
	}
	prefix := append([]string(nil), snapshotCommand[:len(snapshotCommand)-2]...)
	return append(append(prefix, "mirror", helper), args...), nil
}

func PlanSessionCatalog(cfg RemoteConfig) (Command, error) {
	if strings.TrimSpace(cfg.SSHCommand) == "" {
		return Command{}, fmt.Errorf("SSH command must not be empty")
	}
	remote, err := sourceHelperArgv(cfg.SnapshotCommand, "session-catalog")
	if err != nil {
		return Command{}, err
	}
	// OpenSSH takes the first value of -o options. These must precede user
	// options: a background recovery check must not become a password prompt.
	options := unattendedSSHOptions()
	options = append(options, cfg.SSHOptions...)
	args, err := buildSSHArgs(options, []string{"-T", "-n"}, cfg.Host, QuoteCommand(remote))
	if err != nil {
		return Command{}, err
	}
	return Command{Name: cfg.SSHCommand, Args: args}, nil
}

func unattendedSSHOptions() []string {
	// A shared ControlMaster connection would bypass these bounds and keepalives,
	// and may be dead after suspend while still accepting new sessions.
	return []string{"-o", "BatchMode=yes", "-o", "ConnectionAttempts=1", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=2", "-o", "ControlMaster=no", "-o", "ControlPath=none"}
}

// AcquireSessionInventory is an observation, never an attach/create operation.
// Its caller owns a deadline and host-level single-flight/retry scheduling.
func AcquireSessionInventory(ctx context.Context, runner Runner, cfg RemoteConfig) (SessionInventory, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return SessionInventory{}, fmt.Errorf("session inventory requires a context deadline")
	}
	if err := sessionContextError(ctx); err != nil {
		return SessionInventory{}, err
	}
	command, err := PlanSessionCatalog(cfg)
	if err != nil {
		return SessionInventory{}, err
	}
	var payload []byte
	switch native := runner.(type) {
	case nil:
		payload, err = boundedSessionCatalog(ctx, ExecRunner{}, command)
	case ExecRunner:
		payload, err = boundedSessionCatalog(ctx, native, command)
	case *ExecRunner:
		payload, err = boundedSessionCatalog(ctx, *native, command)
	default:
		payload, err = runner.Output(ctx, command)
	}
	if err != nil {
		return SessionInventory{}, fmt.Errorf("acquire session inventory from %s: %w", cfg.Host, err)
	}
	if err := sessionContextError(ctx); err != nil {
		return SessionInventory{}, err
	}
	return DecodeSessionInventory(payload)
}
