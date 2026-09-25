package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func sessionSupervisorArgv(self string, remote RemoteConfig, session, id, token string) ([]string, error) {
	if strings.TrimSpace(self) == "" || !correlationTokenPattern.MatchString(token) {
		return nil, fmt.Errorf("persistent view requires self executable and projection token")
	}
	if _, err := PlanSessionAttachment(remote, session, id, "0123456789abcdef0123456789abcdef"); err != nil {
		return nil, err
	}
	if _, err := recoveryIdentity(remote); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(remote)
	if err != nil {
		return nil, err
	}
	argv := []string{self, "mirror", "session-supervisor", "--remote-json", string(payload), "--session", session, "--session-id", id, "--token", token}
	// Keep every launched helper observable by procmeta's bounded cmdline read.
	size := 0
	for _, arg := range argv {
		size += len(arg) + 1
	}
	if size > 64<<10 {
		return nil, fmt.Errorf("persistent helper argv exceeds process evidence bound")
	}
	return argv, nil
}

func sameSupervisorExecutable(observed, configured string) bool {
	if configured == "" {
		return false
	}
	if sameExecutableArgv0(observed, configured) {
		return true
	}
	resolved := configured
	if path, err := exec.LookPath(configured); err == nil {
		resolved = path
	}
	if path, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = path
	}
	if !filepath.IsAbs(resolved) {
		return false
	}
	if sameExecutableArgv0(observed, resolved) {
		return true
	}
	// Nix's transparent executable wrapper stays in the configured binary's
	// resolved directory. A same-basename executable elsewhere is not evidence.
	return filepath.Clean(observed) == filepath.Join(filepath.Dir(resolved), "."+filepath.Base(resolved)+"-wrapped")
}

func inspectSupervisor(ctx context.Context, argv []string, pid int, window OwnedWindow, cfg ProjectionEvidenceConfig) (Projection, error) {
	if len(argv) != 11 || !sameSupervisorExecutable(argv[0], cfg.SelfCommand) || argv[1] != "mirror" || argv[2] != "session-supervisor" || argv[3] != "--remote-json" || argv[5] != "--session" || argv[7] != "--session-id" || argv[9] != "--token" || len(argv[4]) > 64<<10 {
		return Projection{}, fmt.Errorf("unrecognized persistent helper command")
	}
	var remote RemoteConfig
	if err := json.Unmarshal([]byte(argv[4]), &remote); err != nil {
		return Projection{}, err
	}
	ssh := cfg.SSHCommand
	if ssh == "" {
		ssh = "ssh"
	}
	if remote.SSHCommand != ssh || !equalStrings(remote.SSHOptions, cfg.SSHOptions) || cfg.SnapshotCommand != nil && !equalStrings(remote.SnapshotCommand, cfg.SnapshotCommand) {
		return Projection{}, fmt.Errorf("persistent helper transport differs from configured source")
	}
	if _, err := sessionSupervisorArgv(cfg.SelfCommand, remote, argv[6], argv[8], argv[10]); err != nil {
		return Projection{}, err
	}
	query := cfg.localState
	if query == nil {
		query = func(ctx context.Context, remote RemoteConfig, token string) (SessionLocalState, int, error) {
			return SessionLocalExchange(ctx, remote, cfg.RuntimeDir, token, nil, nil)
		}
	}
	state, peer, err := query(ctx, remote, argv[10])
	if err != nil {
		return Projection{}, err
	}
	identity, _ := recoveryIdentity(remote)
	if peer != pid || !validSessionLocalState(state) || state.Transport != identity || state.Token != argv[10] || state.Session != argv[6] || state.SessionID != argv[8] || !validAttachmentAttempt(state.Origin.Client) {
		return Projection{}, fmt.Errorf("persistent helper process or incarnation differs from observation")
	}
	return Projection{Window: window, SourceHost: remote.Host, Session: state.Session, SessionID: state.SessionID, CorrelationToken: state.Token, Supervised: true, Ready: state.State == string(sessionReady) && state.Origin.Generation != 0}, nil
}
