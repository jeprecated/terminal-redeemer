package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jmo/terminal-redeemer/internal/bootid"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

const attachmentMarkerPrefix = "\x1eREDEEM_ATTACH_V1:"

var attachmentAttemptPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func validAttachmentAttempt(attempt string) bool {
	return attachmentAttemptPattern.MatchString(attempt)
}

// AttachmentMarker is internal transport evidence, bound to a fresh random
// attempt. Terminal data is otherwise opaque. Missing/malformed/old markers
// never authorize input, intentional closure, or session-end decisions.
func AttachmentMarker(attempt, event string) string {
	if !validAttachmentAttempt(attempt) {
		return ""
	}
	switch event {
	case "ready", "detached", "missing", "replaced", "unverifiable", "unsupported", "invalid", "failed", "cancelled":
		return attachmentMarkerPrefix + attempt + ":" + event + "\x1f"
	default:
		return ""
	}
}

type SessionAttachConfig struct {
	Session, SessionID, Attempt string
	Command, SocketBase         string
	StartupTimeout              time.Duration
	Stdin                       *os.File
	Stdout, Stderr              io.Writer
}

// RunSessionAttachment is a single attach-only attempt, not a retry loop. Its
// future local supervisor must discard physical input until the ready marker
// is received and validated for the current generation. No public launch path
// uses this helper yet.
func RunSessionAttachment(ctx context.Context, cfg SessionAttachConfig) (string, error) {
	if !zellijlive.SafeSessionName(cfg.Session) || !validSessionID(cfg.SessionID) || !validAttachmentAttempt(cfg.Attempt) || cfg.StartupTimeout <= 0 {
		return "invalid", fmt.Errorf("exact attachment requires session, incarnation, fresh attempt and positive timeout")
	}
	if cfg.Command == "" {
		cfg.Command = "zellij"
	}
	if cfg.SocketBase == "" {
		cfg.SocketBase = zellijlive.DefaultSocketBase(os.Getuid())
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancelStartup()
	if ctx.Err() != nil {
		return "cancelled", ctx.Err()
	}
	// Use the source machine's command, without a release/version gate.
	// The relay still requires exact socket identity and real attach/render IPC.
	if _, err := exec.LookPath(cfg.Command); err != nil {
		return "unsupported", err
	}
	boot, err := bootid.Current()
	if err != nil {
		return "unverifiable", err
	}
	pinned, err := pinSessionSocket(cfg.SocketBase, cfg.Session, cfg.SessionID, boot)
	if err != nil {
		switch {
		case errors.Is(err, errSessionMissing):
			return "missing", err
		case errors.Is(err, errSessionReplaced):
			return "replaced", err
		default:
			return "unverifiable", err
		}
	}
	defer pinned.Close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	relay, err := startAttachmentRelay(runCtx, pinned, cfg.Attempt)
	if err != nil {
		return "failed", err
	}
	defer func() { cancel(); <-relay.done }()
	// A private fixed alias avoids CLI option parsing of leading-dash names and
	// long socket paths. Only its relay can resolve this alias to the pinned
	// endpoint; the remote session's real name/identity is never changed.
	// Inherit the helper's foreground process group. Giving this TTY reader
	// procrun's separate background group would stop it with SIGTTIN.
	cmd := exec.CommandContext(runCtx, cfg.Command, "attach", "session", "options", "--on-force-close", "detach")
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = attachmentEnvironment(os.Environ(), pinned)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = cfg.Stdin, cfg.Stdout, cfg.Stderr
	restore := preserveTerminal(int(cfg.Stdin.Fd()))
	defer restore()
	if err := startupCtx.Err(); err != nil {
		return "failed", err
	}
	if err := cmd.Start(); err != nil {
		return "failed", err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	ready := relay.readyCh
	deadline := startupCtx.Done()
	for {
		select {
		case err := <-exited:
			if ctx.Err() != nil {
				return "cancelled", ctx.Err()
			}
			if relay.wasDetached() && err == nil {
				return "detached", nil
			}
			if err == nil {
				err = fmt.Errorf("Zellij exited without validated detach evidence")
			}
			return "failed", err
		case <-ready:
			ready, deadline = nil, nil
			cancelStartup()
		case err := <-relay.failed:
			cancel()
			<-exited
			return "failed", err
		case <-deadline:
			cancel()
			<-exited
			if ctx.Err() != nil {
				return "cancelled", ctx.Err()
			}
			return "failed", fmt.Errorf("attachment readiness deadline: %w", startupCtx.Err())
		case <-ctx.Done():
			cancel()
			<-exited
			return "cancelled", ctx.Err()
		}
	}
}

func attachmentEnvironment(env []string, pinned *pinnedSessionSocket) []string {
	out := make([]string, 0, len(env)+2)
	for _, value := range env {
		key, _, _ := strings.Cut(value, "=")
		if key == "ZELLIJ" || strings.HasPrefix(key, "ZELLIJ_") || key == "XDG_CACHE_HOME" {
			continue
		}
		out = append(out, value)
	}
	root := filepath.Dir(pinned.view)
	return append(out, "ZELLIJ_SOCKET_DIR="+pinned.view, "XDG_CACHE_HOME="+filepath.Join(root, "cache"))
}

// PlanSessionAttachment is attach-only. It deliberately has no create option.
func PlanSessionAttachment(cfg RemoteConfig, session, id, attempt string) (Command, error) {
	if !zellijlive.SafeSessionName(session) || !validSessionID(id) || !validAttachmentAttempt(attempt) || strings.TrimSpace(cfg.SSHCommand) == "" {
		return Command{}, fmt.Errorf("invalid exact attachment request")
	}
	remote, err := sourceHelperArgv(cfg.SnapshotCommand, "session-attach", "--session", session, "--session-id", id, "--attempt", attempt)
	if err != nil {
		return Command{}, err
	}
	options := append(unattendedSSHOptions(), cfg.SSHOptions...)
	args, err := buildSSHArgs(options, []string{"-tt"}, cfg.Host, QuoteCommand(remote))
	if err != nil {
		return Command{}, err
	}
	return Command{Name: cfg.SSHCommand, Args: args}, nil
}
