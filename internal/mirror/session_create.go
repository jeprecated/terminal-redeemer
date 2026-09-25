package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	"strings"

	"github.com/jmo/terminal-redeemer/internal/bootid"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

// CreatedSession is a receipt for one successful creation invocation, not a
// name-based recovery request. A lost receipt must never be reconstructed by
// retrying creation or looking up the generated name later.
type CreatedSession struct {
	Session   string `json:"session"`
	SessionID string `json:"session_id"`
}

func RunSessionCreation(ctx context.Context, session string) (CreatedSession, error) {
	boot, err := bootid.Current()
	if err != nil {
		return CreatedSession{}, err
	}
	observer := zellijlive.CommandCataloger{BootID: boot}
	return createSessionOnce(ctx, session, observer, func(ctx context.Context) (string, error) {
		base := zellijlive.DefaultSocketBase(os.Getuid())
		env := sessionCreationEnv(os.Environ(), base)
		_, err := boundedSessionCatalog(ctx, ExecRunner{Env: env}, Command{Name: "zellij", Args: []string{"attach", "--create-background", "--", session}})
		if err != nil {
			return "", err
		}
		// Anchor the successful creator's socket before any subsequent catalog
		// walk. A name observed later cannot replace this initial identity.
		path := filepath.Join(base, zellijlive.SocketContractDir, session)
		return zellijlive.ExactSocketIDAt(unix.AT_FDCWD, path, boot, session)
	})
}

// sessionCreationEnv pins Zellij to the socket base the receipt is read from;
// Zellij's own fallback without XDG_RUNTIME_DIR differs from DefaultSocketBase.
func sessionCreationEnv(environ []string, base string) []string {
	env := []string{}
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		scrub := key == "ZELLIJ_SOCKET_DIR"
		for _, name := range zellijEnvironment {
			if key == name {
				scrub = true
				break
			}
		}
		if !scrub {
			env = append(env, entry)
		}
	}
	return append(env, "ZELLIJ_SOCKET_DIR="+base)
}

func createSessionOnce(ctx context.Context, session string, observer zellijlive.Cataloger, create func(context.Context) (string, error)) (CreatedSession, error) {
	if !generatedSessionPattern.MatchString(session) {
		return CreatedSession{}, fmt.Errorf("creation requires a generated mirror session name")
	}
	if _, ok := ctx.Deadline(); !ok {
		return CreatedSession{}, fmt.Errorf("creation requires a deadline")
	}
	if err := sessionContextError(ctx); err != nil {
		return CreatedSession{}, err
	}
	before, err := observer.Observe(ctx)
	if err != nil {
		return CreatedSession{}, err
	}
	// Refuse both active and resurrectable collisions. This invocation alone is
	// allowed to create; no command is retried after it has possibly run.
	for _, name := range before.Names {
		if name == session {
			return CreatedSession{}, fmt.Errorf("generated session already exists; refusing creation")
		}
	}
	if before.Names == nil || before.Sessions == nil {
		return CreatedSession{}, fmt.Errorf("incomplete pre-creation catalog")
	}
	if err := sessionContextError(ctx); err != nil {
		return CreatedSession{}, err
	}
	id, err := create(ctx)
	if err != nil {
		return CreatedSession{}, fmt.Errorf("creation outcome uncertain; do not retry creation: %w", err)
	}
	if !validSessionID(id) {
		return CreatedSession{}, fmt.Errorf("creation identity unavailable; do not retry creation")
	}
	after, err := ObserveSessionInventory(ctx, observer)
	if err != nil {
		return CreatedSession{}, fmt.Errorf("creation identity uncertain; do not retry creation: %w", err)
	}
	if err := sessionContextError(ctx); err != nil {
		return CreatedSession{}, err
	}
	if after.SessionIDs[session] != id {
		return CreatedSession{}, fmt.Errorf("created session disappeared or changed before receipt; do not retry creation")
	}
	return CreatedSession{Session: session, SessionID: id}, nil
}

func PlanSessionCreation(cfg RemoteConfig, session string) (Command, error) {
	if !generatedSessionPattern.MatchString(session) {
		return Command{}, fmt.Errorf("creation requires a generated mirror session name")
	}
	if strings.TrimSpace(cfg.SSHCommand) == "" {
		return Command{}, fmt.Errorf("SSH command is empty")
	}
	remote, err := sourceHelperArgv(cfg.SnapshotCommand, "session-create", "--session", session)
	if err != nil {
		return Command{}, err
	}
	options := append(unattendedSSHOptions(), cfg.SSHOptions...)
	args, err := buildSSHArgs(options, []string{"-T", "-n"}, cfg.Host, QuoteCommand(remote))
	if err != nil {
		return Command{}, err
	}
	return Command{Name: cfg.SSHCommand, Args: args}, nil
}

func CreateRemoteSession(ctx context.Context, runner Runner, cfg RemoteConfig, session string) (CreatedSession, error) {
	if _, ok := ctx.Deadline(); !ok {
		return CreatedSession{}, fmt.Errorf("creation requires a deadline")
	}
	command, err := PlanSessionCreation(cfg, session)
	if err != nil {
		return CreatedSession{}, err
	}
	if err := sessionContextError(ctx); err != nil {
		return CreatedSession{}, err
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
		return CreatedSession{}, fmt.Errorf("creation outcome uncertain for %s; never retried: %w", session, err)
	}
	if err := sessionContextError(ctx); err != nil {
		return CreatedSession{}, err
	}
	var receipt CreatedSession
	if len(payload) > 4096 {
		return receipt, fmt.Errorf("creation receipt exceeds bound")
	}
	if err := json.Unmarshal(payload, &receipt); err != nil {
		return CreatedSession{}, fmt.Errorf("creation receipt unavailable; never retried: %w", err)
	}
	if receipt.Session != session || !validSessionID(receipt.SessionID) {
		return CreatedSession{}, fmt.Errorf("creation receipt does not prove the requested session")
	}
	return receipt, nil
}
