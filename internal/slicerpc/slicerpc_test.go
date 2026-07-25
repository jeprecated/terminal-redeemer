package slicerpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/niriipc"
)

type launcherFunc func(context.Context, string) LaunchResult

func (f launcherFunc) Launch(ctx context.Context, id string) LaunchResult { return f(ctx, id) }

func request(verb Verb, payload any) Request {
	raw, _ := json.Marshal(payload)
	return Request{SchemaVersion: 1, AcceptSchemaVersions: []uint32{1}, RequestID: "req-1", Verb: verb, Payload: raw}
}
func TestDecodeRequestBoundaries(t *testing.T) {
	valid := `{"schema_version":1,"accept_schema_versions":[1],"request_id":"r-1","verb":"liveness","payload":{}}`
	if _, err := DecodeRequest(strings.NewReader(valid)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"schema_version":1,"schema_version":1,"request_id":"r","verb":"liveness"}`,
		`{"schema_version":1,"request_id":"r","verb":"nope"}`,
		`{"schema_version":1,"request_id":"bad\nmetadata","verb":"liveness"}`,
		`{"schema_version":1,"request_id":"r","verb":"liveness","unknown":true}`,
	} {
		if _, err := DecodeRequest(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted hostile request %s", raw)
		}
	}
	if _, err := DecodeRequest(bytes.NewReader(bytes.Repeat([]byte("x"), MaxRequestBytes+1))); err == nil {
		t.Fatal("accepted oversized request")
	}
}
func TestDecodePayloadRejectsCaseFoldedFieldsAndUnsafeQueryMetadata(t *testing.T) {
	var payload TokenPayload
	if err := DecodePayload([]byte(`{"token":"0","session_nAme":" "}`), &payload); err == nil {
		t.Fatal("case-folded unknown payload field accepted")
	}
	response := (Server{TokenStateUnavailable: true}).Handle(context.Background(), request(VerbTokenQuery, TokenPayload{Token: "token-1", SessionName: "unsafe session", WorkspaceName: "Work"}))
	if response.Outcome.Status != StatusInvalid || response.Outcome.Code != "invalid_launch_metadata" {
		t.Fatalf("unsafe query metadata reached token authority: %+v", response)
	}
}

func TestLaunchPersistsPendingBeforeSideEffectAndReplays(t *testing.T) {
	store, err := NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := Server{SourceHostID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Tokens: store, Now: func() time.Time { return time.Unix(10, 0) }, Launcher: launcherFunc(func(_ context.Context, id string) LaunchResult {
		calls++
		record, err := store.Read("token-1")
		if err != nil {
			t.Fatalf("pending not durable before launch: %v", err)
		}
		if record.Status != TokenPending || record.HostTerminalID != id {
			t.Fatalf("record=%#v id=%q", record, id)
		}
		return LaunchResult{Started: true}
	})}
	first := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "token-1"}))
	if first.Outcome.Status != StatusOK {
		t.Fatalf("first=%#v", first)
	}
	second := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "token-1"}))
	if second.Outcome.Status != StatusOK || calls != 1 {
		t.Fatalf("replay=%#v calls=%d", second, calls)
	}
	query := server.Handle(context.Background(), request(VerbTokenQuery, TokenPayload{Token: "token-1"}))
	if query.Outcome.Status != StatusOK {
		t.Fatalf("query=%#v", query)
	}
}
func TestAmbiguousCancellationStaysPendingAndNeverRepeats(t *testing.T) {
	store, _ := NewTokenStore(t.TempDir())
	calls := 0
	server := Server{SourceHostID: "host", Tokens: store, Launcher: launcherFunc(func(ctx context.Context, _ string) LaunchResult {
		calls++
		<-ctx.Done()
		return LaunchResult{Started: true, Err: ctx.Err()}
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first := server.Handle(ctx, request(VerbLaunch, LaunchPayload{Token: "token-2"}))
	if first.Outcome.Status != StatusPending {
		t.Fatalf("first=%#v", first)
	}
	second := server.Handle(context.Background(), request(VerbTokenReplay, TokenPayload{Token: "token-2"}))
	if second.Outcome.Status != StatusPending || calls != 1 {
		t.Fatalf("replay=%#v calls=%d", second, calls)
	}
}
func TestDefiniteLaunchFailureIsStable(t *testing.T) {
	store, _ := NewTokenStore(t.TempDir())
	calls := 0
	server := Server{SourceHostID: "host", Tokens: store, Launcher: launcherFunc(func(context.Context, string) LaunchResult {
		calls++
		return LaunchResult{Err: errors.New("exec not found")}
	})}
	for i := 0; i < 2; i++ {
		response := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "token-3"}))
		if response.Outcome.Status != StatusFailed {
			t.Fatalf("response=%#v", response)
		}
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
func TestDirectKittyLauncherExactArgvAndCleanEnvironment(t *testing.T) {
	var gotCommand string
	var gotArgs, gotEnv []string
	launcher := DirectKittyLauncher{Command: "/nix/store/kitty", Environment: map[string]string{"NIRI_SOCKET": "/run/niri.sock", "WAYLAND_DISPLAY": "wayland-1", "XDG_RUNTIME_DIR": "/run/user/1000"}, Run: func(_ context.Context, command string, args, env []string) LaunchResult {
		gotCommand = command
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return LaunchResult{Started: true}
	}}
	if result := launcher.Launch(context.Background(), "term_safe"); result.Err != nil || !result.Started {
		t.Fatalf("result=%#v", result)
	}
	if gotCommand != "/nix/store/kitty" || strings.Join(gotArgs, " ") != "--config NONE --detach --class terminal-redeemer-host --title terminal-redeemer-host:term_safe" {
		t.Fatalf("argv=%q command=%q", gotArgs, gotCommand)
	}
	joined := strings.Join(gotEnv, "\n")
	if len(gotEnv) != 4 {
		t.Fatalf("launch environment=%q", gotEnv)
	}
	for _, required := range []string{"NIRI_SOCKET=/run/niri.sock", "WAYLAND_DISPLAY=wayland-1", "XDG_RUNTIME_DIR=/run/user/1000", "TERMINAL_REDEEMER_HOST_TERMINAL_ID=term_safe"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing %q in %q", required, gotEnv)
		}
	}
	if strings.Contains(joined, "PATH=") || strings.Contains(joined, "SSH_AUTH_SOCK=") {
		t.Fatalf("unclean environment: %q", gotEnv)
	}
}

type fakeNiri struct {
	snapshots []niriipc.State
	actions   []any
}

func (f *fakeNiri) Snapshot(context.Context) (niriipc.State, error) {
	if len(f.snapshots) == 0 {
		return niriipc.State{}, errors.New("none")
	}
	state := f.snapshots[0]
	if len(f.snapshots) > 1 {
		f.snapshots = f.snapshots[1:]
	}
	return state, nil
}
func (f *fakeNiri) Action(_ context.Context, action any) error {
	f.actions = append(f.actions, action)
	return nil
}
func TestWorkspaceEnsureVerifiesNamedAndTrailingWorkspace(t *testing.T) {
	name := "target"
	output := "DP-1"
	id := uint64(9)
	initial := niriipc.State{Workspaces: []niriipc.Workspace{{ID: 1, Index: 1, Output: &output, IsActive: true}, {ID: id, Index: 2, Output: &output}}}
	next := niriipc.State{Workspaces: []niriipc.Workspace{{ID: 1, Index: 1, Output: &output, IsActive: true}, {ID: id, Index: 2, Output: &output, Name: &name}, {ID: 10, Index: 3, Output: &output}}}
	fake := &fakeNiri{snapshots: []niriipc.State{initial, next}}
	server := Server{Niri: fake, PollInterval: time.Nanosecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response := server.Handle(ctx, request(VerbWorkspaceEnsure, WorkspaceEnsurePayload{Name: name}))
	if response.Outcome.Status != StatusOK || len(fake.actions) != 1 {
		t.Fatalf("response=%#v actions=%#v", response, fake.actions)
	}
}
func TestWorkspaceEnsureRejectsDuplicateExactNamesBeforeAction(t *testing.T) {
	name, output := "target", "DP-1"
	fake := &fakeNiri{snapshots: []niriipc.State{{Workspaces: []niriipc.Workspace{
		{ID: 1, Index: 1, Output: &output, IsActive: true, Name: &name},
		{ID: 2, Index: 2, Output: &output, Name: &name},
		{ID: 3, Index: 3, Output: &output},
	}}}}
	response := (Server{Niri: fake}).Handle(context.Background(), request(VerbWorkspaceEnsure, WorkspaceEnsurePayload{Name: name}))
	if response.Outcome.Status != StatusUnavailable || len(fake.actions) != 0 {
		t.Fatalf("duplicate exact name was accepted: response=%#v actions=%#v", response, fake.actions)
	}
}

func TestWorkspaceEnsureRejectsCanonicalCollisionBeforeAction(t *testing.T) {
	name, collision, output := "target", "ＴＡＲＧＥＴ", "DP-1"
	fake := &fakeNiri{snapshots: []niriipc.State{{Workspaces: []niriipc.Workspace{
		{ID: 1, Index: 1, Output: &output, IsActive: true, Name: &name},
		{ID: 2, Index: 2, Output: &output, Name: &collision},
		{ID: 3, Index: 3, Output: &output},
	}}}}
	response := (Server{Niri: fake}).Handle(context.Background(), request(VerbWorkspaceEnsure, WorkspaceEnsurePayload{Name: name}))
	if response.Outcome.Status != StatusUnavailable || len(fake.actions) != 0 {
		t.Fatalf("canonical collision was accepted: response=%#v actions=%#v", response, fake.actions)
	}
}

func TestWorkspaceEnsureRevalidatesCollisionAfterWrite(t *testing.T) {
	name, collision, output := "target", "ＴＡＲＧＥＴ", "DP-1"
	candidateID := uint64(9)
	initial := niriipc.State{Workspaces: []niriipc.Workspace{
		{ID: 1, Index: 1, Output: &output, IsActive: true},
		{ID: candidateID, Index: 2, Output: &output},
	}}
	next := niriipc.State{Workspaces: []niriipc.Workspace{
		{ID: 1, Index: 1, Output: &output, IsActive: true},
		{ID: candidateID, Index: 2, Output: &output, Name: &name},
		{ID: 10, Index: 3, Output: &output, Name: &collision},
		{ID: 11, Index: 4, Output: &output},
	}}
	fake := &fakeNiri{snapshots: []niriipc.State{initial, next}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response := (Server{Niri: fake, PollInterval: time.Nanosecond}).Handle(ctx, request(VerbWorkspaceEnsure, WorkspaceEnsurePayload{Name: name}))
	if response.Outcome.Status != StatusUnavailable || len(fake.actions) != 1 {
		t.Fatalf("post-write collision was accepted: response=%#v actions=%#v", response, fake.actions)
	}
}

func TestWorkspaceEnsureRevalidatesTopologyAndCandidateOutputAfterWrite(t *testing.T) {
	name, firstOutput, secondOutput := "target", "DP-1", "DP-2"
	candidateID := uint64(9)
	initial := niriipc.State{Workspaces: []niriipc.Workspace{
		{ID: 1, Index: 1, Output: &firstOutput, IsActive: true},
		{ID: candidateID, Index: 2, Output: &firstOutput},
	}}
	tests := []struct {
		name string
		next niriipc.State
	}{
		{"active output changed", niriipc.State{Workspaces: []niriipc.Workspace{
			{ID: 1, Index: 1, Output: &secondOutput, IsActive: true},
			{ID: candidateID, Index: 2, Output: &secondOutput, Name: &name},
			{ID: 10, Index: 3, Output: &secondOutput},
		}}},
		{"multi-output topology", niriipc.State{Workspaces: []niriipc.Workspace{
			{ID: 1, Index: 1, Output: &firstOutput, IsActive: true},
			{ID: candidateID, Index: 2, Output: &secondOutput, Name: &name},
			{ID: 10, Index: 3, Output: &firstOutput},
		}}},
		{"candidate runtime identity replaced", niriipc.State{Workspaces: []niriipc.Workspace{
			{ID: 1, Index: 1, Output: &firstOutput, IsActive: true},
			{ID: 99, Index: 2, Output: &firstOutput, Name: &name},
			{ID: 10, Index: 3, Output: &firstOutput},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeNiri{snapshots: []niriipc.State{initial, test.next}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			response := (Server{Niri: fake, PollInterval: time.Nanosecond}).Handle(ctx, request(VerbWorkspaceEnsure, WorkspaceEnsurePayload{Name: name}))
			if response.Outcome.Status != StatusUnavailable || len(fake.actions) != 1 {
				t.Fatalf("topology/output change was accepted: response=%#v actions=%#v", response, fake.actions)
			}
		})
	}
}

func TestPostStartLaunchErrorRemainsPending(t *testing.T) {
	store, _ := NewTokenStore(t.TempDir())
	calls := 0
	server := Server{SourceHostID: "host", Tokens: store, Launcher: launcherFunc(func(context.Context, string) LaunchResult {
		calls++
		return LaunchResult{Started: true, Err: errors.New("exit 1 after start")}
	})}
	first := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "ambiguous"}))
	second := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "ambiguous"}))
	if first.Outcome.Status != StatusPending || second.Outcome.Status != StatusPending || calls != 1 {
		t.Fatalf("first=%#v second=%#v calls=%d", first, second, calls)
	}
}

type blockingReadCloser struct {
	once   sync.Once
	closed chan struct{}
}

func (r *blockingReadCloser) Read([]byte) (int, error) { <-r.closed; return 0, os.ErrClosed }
func (r *blockingReadCloser) Close() error             { r.once.Do(func() { close(r.closed) }); return nil }
func TestDecodeRequestContextBoundsNeverEOFInput(t *testing.T) {
	reader := &blockingReadCloser{closed: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := DecodeRequestContext(ctx, reader); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("decode did not cancel promptly")
	}
}

func TestPendingCreationCrashBoundariesNeverExposeTornFinalRecord(t *testing.T) {
	t.Run("file sync before link", func(t *testing.T) {
		store, _ := NewTokenStore(t.TempDir())
		store.syncFile = func(*os.File) error { return errors.New("crash") }
		if _, _, err := store.CreatePending("host", "prelink", time.Now()); err == nil {
			t.Fatal("expected error")
		}
		if _, err := store.Read("prelink"); !errors.Is(err, ErrTokenNotFound) {
			t.Fatalf("final record exists: %v", err)
		}
		entries, _ := os.ReadDir(store.Root())
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".pending-") {
				t.Fatalf("temporary record leaked: %s", entry.Name())
			}
		}
	})
	t.Run("directory sync after atomic link", func(t *testing.T) {
		store, _ := NewTokenStore(t.TempDir())
		calls := 0
		store.syncDirectory = func(string) error {
			calls++
			if calls == 1 {
				return errors.New("crash")
			}
			return nil
		}
		launched := 0
		server := Server{SourceHostID: "host", Tokens: store, Launcher: launcherFunc(func(context.Context, string) LaunchResult { launched++; return LaunchResult{Started: true} })}
		response := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "linked"}))
		if response.Outcome.Code != "token_state_unavailable" || launched != 0 {
			t.Fatalf("response=%#v launched=%d", response, launched)
		}
		record, err := store.Read("linked")
		if err != nil || record.Status != TokenPending {
			t.Fatalf("record=%#v err=%v", record, err)
		}
		replay := server.Handle(context.Background(), request(VerbLaunch, LaunchPayload{Token: "linked"}))
		if replay.Outcome.Status != StatusPending || launched != 0 {
			t.Fatalf("replay=%#v launched=%d", replay, launched)
		}
	})
}

func TestTokenStoreRejectsSymlinkedComponents(t *testing.T) {
	t.Run("slice component", func(t *testing.T) {
		state := t.TempDir()
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(state, "slice")); err != nil {
			t.Fatal(err)
		}
		if _, err := NewTokenStore(state); err == nil {
			t.Fatal("accepted symlinked slice component")
		}
	})
	t.Run("root mode changed", func(t *testing.T) {
		store, _ := NewTokenStore(t.TempDir())
		if err := os.Chmod(store.root, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Read("token"); err == nil {
			t.Fatal("accepted non-private token root")
		}
	})
	t.Run("root replaced", func(t *testing.T) {
		store, _ := NewTokenStore(t.TempDir())
		real := store.root + "-real"
		if err := os.Rename(store.root, real); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, store.root); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Read("token"); err == nil {
			t.Fatal("accepted symlinked token root")
		}
	})
}

func TestWorkspaceEnsureRejectsNonTrailingEmptyCandidate(t *testing.T) {
	output, named := "DP-1", "later"
	id := uint64(2)
	fake := &fakeNiri{snapshots: []niriipc.State{{Workspaces: []niriipc.Workspace{
		{ID: 1, Index: 1, Output: &output, IsActive: true},
		{ID: id, Index: 2, Output: &output},
		{ID: 3, Index: 3, Output: &output, Name: &named},
	}}}}
	server := Server{Niri: fake}
	response := server.Handle(context.Background(), request(VerbWorkspaceEnsure, WorkspaceEnsurePayload{Name: "target"}))
	if response.Outcome.Status != StatusUnavailable || len(fake.actions) != 0 {
		t.Fatalf("response=%#v actions=%#v", response, fake.actions)
	}
}

func TestDirectKittyLauncherRejectsIncompleteOrExtraContext(t *testing.T) {
	for _, environment := range []map[string]string{{"NIRI_SOCKET": "/run/niri.sock"}, {"NIRI_SOCKET": "/run/niri.sock", "WAYLAND_DISPLAY": "wayland-1", "XDG_RUNTIME_DIR": "/run/user/1000", "HOME": "/secret"}, {"NIRI_SOCKET": "/run/niri.sock", "WAYLAND_DISPLAY": string([]byte{0xff}), "XDG_RUNTIME_DIR": "/run/user/1000"}} {
		result := (DirectKittyLauncher{Command: "kitty", Environment: environment, Run: func(context.Context, string, []string, []string) LaunchResult { return LaunchResult{Started: true} }}).Launch(context.Background(), "term_safe")
		if result.Err == nil || result.Started {
			t.Fatalf("accepted context %#v", environment)
		}
	}
}

func TestTokenJournalEnrollmentDistinguishesNeverCreatedFromMissingAfterUse(t *testing.T) {
	fresh := t.TempDir()
	store, err := NewTokenStore(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Read("never-created"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("fresh absence=%v", err)
	}
	if _, _, err = store.CreatePending("host", "used-token", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(store.Root()); err != nil {
		t.Fatal(err)
	}
	if _, err = NewTokenStore(fresh); err == nil {
		t.Fatal("missing-after-use journal was recreated")
	}
	server := Server{TokenStateUnavailable: true}
	response := server.Handle(context.Background(), request(VerbTokenReplay, TokenPayload{Token: "used-token"}))
	if response.Outcome.Code != "token_state_unavailable" {
		t.Fatalf("response=%+v", response)
	}
	unmarked := t.TempDir()
	if err = os.MkdirAll(filepath.Join(unmarked, "slice", "rpc-tokens"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = NewTokenStore(unmarked); err == nil {
		t.Fatal("unmarked journal accepted")
	}
}

func TestTokenStateRejectsCorruptionAndUnsafeMode(t *testing.T) {
	store, _ := NewTokenStore(t.TempDir())
	_, _, _ = store.CreatePending("host", "token-4", time.Now())
	path, _ := store.path("token-4")
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("token-4"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestSpatialApplyStrictOwnershipPayloadAndTypedResult(t *testing.T) {
	called := 0
	server := Server{SpatialApply: func(_ context.Context, payload SpatialApplyPayload) error {
		called++
		if payload.SourceID != "src_test" || payload.SourceEpoch != "epoch-1" || payload.RuntimeWindowID != 42 {
			t.Fatalf("payload=%#v", payload)
		}
		return nil
	}}
	payload := SpatialApplyPayload{SourceID: "src_test", SourceEpoch: "epoch-1", RuntimeWindowID: 42, Changes: []SpatialChange{{Kind: "width_percent", Percent: 50}}}
	response := server.Handle(context.Background(), request(VerbSpatialApply, payload))
	if response.Outcome.Status != StatusOK || called != 1 {
		t.Fatalf("response=%#v called=%d", response, called)
	}
	bad := SpatialApplyPayload{SourceID: "src_test", SourceEpoch: "epoch-1", RuntimeWindowID: 42, Changes: []SpatialChange{{Kind: "workspace"}}}
	response = server.Handle(context.Background(), request(VerbSpatialApply, bad))
	if response.Outcome.Code != "invalid_spatial_apply" || called != 1 {
		t.Fatalf("invalid response=%#v", response)
	}
}
