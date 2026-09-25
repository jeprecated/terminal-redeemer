package mirror

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupervisorProjectionOfflineOwnershipAndTransportDescendant(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			root := t.TempDir()
			remote := RemoteConfig{Host: "lattice", SSHCommand: "ssh", SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
			r := recoveryRequest(0)
			self := "/nix/store/example/bin/redeem"
			argv, err := sessionSupervisorArgv(self, remote, r.Session, r.SessionID, r.Token)
			if err != nil {
				t.Fatal(err)
			}
			argv[0] = "/nix/store/example/bin/.redeem-wrapped"
			writeMirrorProc(t, root, 100, 1, 10, []string{"/nix/store/kitty/bin/.kitty-wrapped"})
			writeMirrorProc(t, root, 101, 100, 11, argv)
			// Even an otherwise recognizable SSH descendant belongs to the helper,
			// not a second projection. Titles remain irrelevant.
			legacy, _ := PlanLaunch(Window{ZellijSession: r.Session}, LaunchConfig{SourceHost: remote.Host, SSHCommand: "ssh", LauncherCommand: "kitty", AppID: "owned"})
			writeMirrorProc(t, root, 102, 101, 12, launchSSHArgv(t, legacy))
			transport, _ := recoveryIdentity(remote)
			state := SessionLocalState{Transport: transport, Token: r.Token, Session: r.Session, SessionID: r.SessionID, State: "offline", Origin: SessionInputOrigin{Client: r.Client}}
			if ready {
				state.State = "ready"
				state.Origin.Attempt = r.Attempt
				state.Origin.Generation = 1
			}
			cfg := ProjectionEvidenceConfig{ProcRoot: root, SelfCommand: self, SSHCommand: "ssh", SnapshotCommand: remote.SnapshotCommand,
				localState: func(context.Context, RemoteConfig, string) (SessionLocalState, int, error) { return state, 101, nil }}
			windows := []OwnedWindow{{ID: 1, PID: 100, Title: "not an identity"}}
			inventory, err := InspectProjections(context.Background(), windows, cfg)
			if err != nil || len(inventory.Exact) != 1 || inventory.Exact[0].Ready != ready || !inventory.Exact[0].Supervised || inventory.Exact[0].SessionID != r.SessionID {
				t.Fatalf("ownership: %+v %v", inventory, err)
			}
			cfg.localState = func(context.Context, RemoteConfig, string) (SessionLocalState, int, error) { return state, 999, nil }
			inventory, err = InspectProjections(context.Background(), windows, cfg)
			if err != nil || len(inventory.Untracked) != 1 || len(inventory.Exact) != 0 {
				t.Fatal("foreign IPC process adopted projection")
			}
			cfg.localState = func(context.Context, RemoteConfig, string) (SessionLocalState, int, error) {
				path := filepath.Join(root, "101", "stat")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data[:strings.LastIndex(string(data), " ")+1], []byte("99")...)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				return state, 101, nil
			}
			inventory, err = InspectProjections(context.Background(), windows, cfg)
			if err != nil || len(inventory.Untracked) != 1 || len(inventory.Exact) != 0 {
				t.Fatal("PID reused during IPC adopted prior ancestry")
			}
		})
	}
}

func TestTwoSupervisorsUnderOneWindowRemainAmbiguous(t *testing.T) {
	root := t.TempDir()
	writeMirrorProc(t, root, 100, 1, 10, []string{"kitty"})
	remote := RemoteConfig{Host: "lattice", SSHCommand: "ssh", SnapshotCommand: []string{"redeem", "mirror", "snapshot"}}
	identity, _ := recoveryIdentity(remote)
	states := map[string]SessionLocalState{}
	pids := map[string]int{}
	for i := 0; i < 2; i++ {
		r := recoveryRequest(i)
		argv, err := sessionSupervisorArgv("redeem", remote, r.Session, r.SessionID, r.Token)
		if err != nil {
			t.Fatal(err)
		}
		writeMirrorProc(t, root, 101+i, 100, 11+i, argv)
		states[r.Token] = SessionLocalState{Transport: identity, Token: r.Token, Session: r.Session, SessionID: r.SessionID, State: "offline", Origin: SessionInputOrigin{Client: r.Client}}
		pids[r.Token] = 101 + i
	}
	cfg := ProjectionEvidenceConfig{ProcRoot: root, SelfCommand: "redeem", SSHCommand: "ssh", localState: func(_ context.Context, _ RemoteConfig, token string) (SessionLocalState, int, error) {
		return states[token], pids[token], nil
	}}
	inventory, err := InspectProjections(context.Background(), []OwnedWindow{{ID: 1, PID: 100}}, cfg)
	if err != nil || len(inventory.Exact) != 0 || len(inventory.Ambiguous) != 1 || len(inventory.AmbiguousCandidates[1]) != 2 {
		t.Fatalf("multiple helpers became exact: %+v %v", inventory, err)
	}
}

func TestSupervisorExecutableDoesNotAcceptWrongDirectoryOrNearMatch(t *testing.T) {
	for _, observed := range []string{"/evil/redeem", "/evil/.redeem-wrapped", "/nix/store/example/bin/.redeem-wrapped-extra"} {
		if sameSupervisorExecutable(observed, "/nix/store/example/bin/redeem") {
			t.Fatalf("accepted %s", observed)
		}
	}
}

func TestProjectionIncompleteDescendantObservationDiscardsEarlierMatch(t *testing.T) {
	root := t.TempDir()
	writeMirrorProc(t, root, 100, 1, 10, []string{"kitty"})
	plan, _ := PlanLaunch(Window{ZellijSession: "known"}, LaunchConfig{SourceHost: "lattice", SSHCommand: "ssh", LauncherCommand: "kitty", AppID: "owned"})
	writeMirrorProc(t, root, 101, 100, 11, launchSSHArgv(t, plan))
	writeMirrorProc(t, root, 102, 100, 12, []string{"unreadable"})
	if err := os.Remove(filepath.Join(root, "102", "cmdline")); err != nil {
		t.Fatal(err)
	}
	inventory, err := InspectProjections(context.Background(), []OwnedWindow{{ID: 1, PID: 100}}, ProjectionEvidenceConfig{ProcRoot: root, SSHCommand: "ssh"})
	if err != nil || len(inventory.Exact) != 0 || len(inventory.Untracked) != 1 {
		t.Fatalf("partial process evidence escaped: %+v %v", inventory, err)
	}
}
