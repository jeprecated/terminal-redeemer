package slicelaunch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/slicerpc"
	"github.com/jmo/terminal-redeemer/internal/sourceinventory"
)

type workspaceStub struct {
	name string
	err  error
}

func (w workspaceStub) Current(context.Context) (string, error) { return w.name, w.err }

type selectionStub bool

func (s selectionStub) Selected(string) (bool, error) { return bool(s), nil }

type localStub struct{ calls *int }

func (l localStub) Launch(context.Context) error { *l.calls++; return nil }

type remoteStub struct {
	calls     *[]slicerpc.Request
	responses []slicerpc.Response
	errors    []error
}

func (r *remoteStub) Call(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
	*r.calls = append(*r.calls, q)
	n := len(*r.calls) - 1
	if n < len(r.errors) && r.errors[n] != nil {
		return slicerpc.Response{}, r.errors[n]
	}
	if n < len(r.responses) {
		return r.responses[n], nil
	}
	return slicerpc.Response{}, errors.New("offline")
}

type handoffStub struct {
	values *[]Intent
	err    error
}

func (h handoffStub) Send(_ context.Context, i Intent) error {
	*h.values = append(*h.values, i)
	return h.err
}
func response(request slicerpc.Request, status slicerpc.OutcomeStatus, launch string, source bool) slicerpc.Response {
	var p struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(request.Payload, &p)
	result := map[string]any{"token": p.Token, "host_terminal_id": "term_stable", "launch_status": launch, "session_name": SessionName(p.Token), "workspace_name": "Work"}
	if source {
		epoch := "11111111-1111-4111-8111-111111111111"
		sourceID, _ := sourceinventory.SourceID(epoch, 9)
		result["source_id"] = sourceID
		result["source_epoch"] = epoch
		result["runtime_window_id"] = json.Number("9")
	}
	return slicerpc.Response{SchemaVersion: 1, RequestID: request.RequestID, Outcome: slicerpc.Outcome{Status: status}, Result: result}
}
func baseRouter(t *testing.T, selected bool) (Router, *Store, *int, *[]slicerpc.Request, *remoteStub) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetMode(true); err != nil {
		t.Fatal(err)
	}
	local := 0
	calls := []slicerpc.Request{}
	remote := &remoteStub{calls: &calls}
	r := Router{Store: store, Workspace: workspaceStub{name: "Work"}, Selection: selectionStub(selected), Local: localStub{calls: &local}, Remote: remote, RetryWindow: time.Minute, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 3, Sleep: func(context.Context, time.Duration) error { return nil }}
	return r, store, &local, &calls, remote
}
func TestModeOffAndUnselectedUseOnlyUnchangedLocalBoundary(t *testing.T) {
	for _, tc := range []struct{ enabled, selected bool }{{false, true}, {true, false}} {
		r, store, local, calls, _ := baseRouter(t, tc.selected)
		_, _ = store.SetMode(tc.enabled)
		if !tc.enabled {
			r.Workspace = workspaceStub{err: errors.New("Niri unavailable")}
		}
		result, err := r.Route(context.Background())
		if err != nil || !result.Local || result.Routed || *local != 1 || len(*calls) != 0 {
			t.Fatalf("tc=%+v result=%+v local=%d remote=%d err=%v", tc, result, *local, len(*calls), err)
		}
	}
}
func TestFirstSuccessPersistsStableIntentAndHandoff(t *testing.T) {
	r, store, local, calls, _ := baseRouter(t, true)
	handoffs := []Intent{}
	r.Handoff = handoffStub{values: &handoffs}
	// Response must know the generated request/token, so wrap through a dynamic remote.
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return response(q, slicerpc.StatusOK, string(slicerpc.TokenLaunched), true), nil
	})
	result, err := r.Route(context.Background())
	if err != nil || !result.Routed || result.Intent == nil || result.Intent.Status != IntentLaunched || *local != 0 || len(handoffs) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := store.Read(result.Intent.Token)
	if err != nil || saved.SessionName != SessionName(saved.Token) || len(saved.SessionName) != 35 || saved.SourceID == "" || saved.RuntimeWindowID != 9 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if len(*calls) != 1 || (*calls)[0].Verb != slicerpc.VerbLaunch {
		t.Fatalf("calls=%+v", *calls)
	}
}

type remoteFunc func(context.Context, slicerpc.Request) (slicerpc.Response, error)

func (f remoteFunc) Call(ctx context.Context, r slicerpc.Request) (slicerpc.Response, error) {
	return f(ctx, r)
}
func TestControllerHandoffIsRequiredBeforeRemoteSideEffect(t *testing.T) {
	r, _, local, calls, _ := baseRouter(t, true)
	values := []Intent{}
	r.Handoff = handoffStub{values: &values, err: errors.New("controller down")}
	result, err := r.Route(context.Background())
	if err == nil || result.Intent == nil || result.Intent.Status != IntentPending || *local != 0 || len(*calls) != 0 || len(values) != 1 {
		t.Fatalf("result=%+v local=%d calls=%d handoffs=%d err=%v", result, *local, len(*calls), len(values), err)
	}
}

func TestLostResponseReplaysSameTokenWithoutLocalFallback(t *testing.T) {
	r, _, local, calls, _ := baseRouter(t, true)
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		if len(*calls) == 1 {
			return slicerpc.Response{}, errors.New("lost")
		}
		return response(q, slicerpc.StatusOK, string(slicerpc.TokenLaunched), true), nil
	})
	result, err := r.Route(context.Background())
	if err != nil || result.Intent == nil || *local != 0 || len(*calls) != 2 || (*calls)[0].Verb != slicerpc.VerbLaunch || (*calls)[1].Verb != slicerpc.VerbTokenReplay {
		t.Fatalf("result=%+v calls=%+v err=%v", result, *calls, err)
	}
	var first, second struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal((*calls)[0].Payload, &first)
	_ = json.Unmarshal((*calls)[1].Payload, &second)
	if first.Token == "" || first.Token != second.Token {
		t.Fatalf("tokens %q %q", first.Token, second.Token)
	}
}
func TestExhaustionThenExplicitReconnectUsesSameIntent(t *testing.T) {
	r, store, local, calls, _ := baseRouter(t, true)
	r.MaxAttempts = 2
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return slicerpc.Response{}, errors.New("offline")
	})
	result, err := r.Route(context.Background())
	if err == nil || result.Intent.Status != IntentDisconnected || *local != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	token := result.Intent.Token
	before := result.Intent.SessionName
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return response(q, slicerpc.StatusOK, string(slicerpc.TokenLaunched), true), nil
	})
	again, err := r.Reconnect(context.Background(), token)
	if err != nil || again.Intent.Token != token || again.Intent.SessionName != before || again.Intent.Status != IntentLaunched {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	saved, _ := store.Read(token)
	if saved.Token != token {
		t.Fatal("same intent not persisted")
	}
}

func TestPendingResumeAfterStoreAndRouterReconstructionRetainsBudget(t *testing.T) {
	r, store, local, calls, _ := baseRouter(t, true)
	r.MaxAttempts = 3
	r.Sleep = func(context.Context, time.Duration) error { return errors.New("process interrupted") }
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return slicerpc.Response{}, errors.New("response lost")
	})
	first, err := r.Route(context.Background())
	if err == nil || first.Intent == nil || first.Intent.Status != IntentPending || first.Intent.Attempt != 1 || *local != 0 {
		t.Fatalf("first=%+v local=%d err=%v", first, *local, err)
	}
	deadline := first.Intent.RetryExpiresAt
	token, session := first.Intent.Token, first.Intent.SessionName

	restored, err := NewStore(store.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	r.Store = restored
	r.Sleep = func(context.Context, time.Duration) error { return nil }
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return response(q, slicerpc.StatusOK, string(slicerpc.TokenLaunched), true), nil
	})
	resumed, err := r.Reconnect(context.Background(), token)
	if err != nil || resumed.Intent == nil || resumed.Intent.Token != token || resumed.Intent.SessionName != session || resumed.Intent.RetryExpiresAt != deadline || resumed.Intent.Attempt != 2 || resumed.Intent.Status != IntentLaunched {
		t.Fatalf("resumed=%+v deadline=%v err=%v", resumed, deadline, err)
	}
	if len(*calls) != 2 || (*calls)[0].Verb != slicerpc.VerbLaunch || (*calls)[1].Verb != slicerpc.VerbTokenReplay {
		t.Fatalf("calls=%+v", *calls)
	}
}
func TestReconstructedPendingIntentAlreadyExpiredMakesNoRemoteCall(t *testing.T) {
	r, store, local, _, _ := baseRouter(t, true)
	now := time.Date(2036, 1, 2, 3, 4, 5, 0, time.UTC)
	r.Now = func() time.Time { return now }
	r.MaxAttempts = 3
	r.Sleep = func(context.Context, time.Duration) error { return errors.New("process interrupted") }
	firstCalls := 0
	r.Remote = remoteFunc(func(context.Context, slicerpc.Request) (slicerpc.Response, error) {
		firstCalls++
		return slicerpc.Response{}, errors.New("response lost")
	})
	first, err := r.Route(context.Background())
	if err == nil || first.Intent == nil || first.Intent.Status != IntentPending || first.Intent.Attempt != 1 || firstCalls != 1 || *local != 0 {
		t.Fatalf("first=%+v calls=%d local=%d err=%v", first, firstCalls, *local, err)
	}
	before := *first.Intent
	now = before.RetryExpiresAt
	restored, err := NewStore(store.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	remoteCalls := 0
	r.Store = restored
	r.Remote = remoteFunc(func(context.Context, slicerpc.Request) (slicerpc.Response, error) {
		remoteCalls++
		return slicerpc.Response{}, errors.New("must not be called")
	})
	r.Sleep = func(context.Context, time.Duration) error { t.Fatal("expired retry slept"); return nil }
	result, err := r.Reconnect(context.Background(), before.Token)
	if err == nil || err.Error() != "routed launch retry exhausted" || result.Intent == nil || result.Intent.Status != IntentDisconnected || result.Code != "disconnected" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if remoteCalls != 0 || *local != 0 || result.Intent.Token != before.Token || result.Intent.SessionName != before.SessionName || result.Intent.RetryExpiresAt != before.RetryExpiresAt || result.Intent.Attempt != before.Attempt {
		t.Fatalf("expired retry changed authority or called side effect: before=%+v after=%+v remote=%d local=%d", before, result.Intent, remoteCalls, *local)
	}
	saved, readErr := restored.Read(before.Token)
	if readErr != nil || saved.Status != IntentDisconnected || saved.Token != before.Token || saved.SessionName != before.SessionName || saved.RetryExpiresAt != before.RetryExpiresAt || saved.Attempt != before.Attempt {
		t.Fatalf("stable disconnected state not persisted: saved=%+v err=%v", saved, readErr)
	}
}

func TestBackoffCrossingAbsoluteDeadlineCannotIssueNextRemoteCall(t *testing.T) {
	r, store, local, calls, _ := baseRouter(t, true)
	now := time.Date(2036, 2, 3, 4, 5, 6, 0, time.UTC)
	r.Now = func() time.Time { return now }
	r.RetryWindow = 5 * time.Second
	r.InitialBackoff = 10 * time.Second
	r.MaxBackoff = 10 * time.Second
	r.MaxAttempts = 3
	r.Remote = remoteFunc(func(_ context.Context, request slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, request)
		return slicerpc.Response{}, errors.New("offline")
	})
	var slept time.Duration
	r.Sleep = func(_ context.Context, duration time.Duration) error {
		slept = duration
		now = now.Add(duration)
		return nil
	}
	result, err := r.Route(context.Background())
	if err == nil || err.Error() != "routed launch retry exhausted" || result.Intent == nil || result.Intent.Status != IntentDisconnected || result.Code != "disconnected" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(*calls) != 1 || result.Intent.Attempt != 1 || slept != 10*time.Second || !now.After(result.Intent.RetryExpiresAt) || *local != 0 {
		t.Fatalf("deadline-crossing sleep escaped bound: calls=%d attempt=%d slept=%s now=%s deadline=%s local=%d", len(*calls), result.Intent.Attempt, slept, now, result.Intent.RetryExpiresAt, *local)
	}
	saved, readErr := store.Read(result.Intent.Token)
	if readErr != nil || saved.Status != IntentDisconnected || saved.Attempt != 1 {
		t.Fatalf("deadline exhaustion not durable: saved=%+v err=%v", saved, readErr)
	}
}

func TestMissingDestinationAfterLostResponseNeverSynthesizesNoncreation(t *testing.T) {
	r, store, local, _, _ := baseRouter(t, true)
	r.MaxAttempts = 1
	r.Remote = remoteFunc(func(context.Context, slicerpc.Request) (slicerpc.Response, error) {
		return slicerpc.Response{}, errors.New("response lost")
	})
	first, err := r.Route(context.Background())
	if err == nil || first.Intent.Status != IntentDisconnected || *local != 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	token := first.Intent.Token
	r.Remote = remoteFunc(func(context.Context, slicerpc.Request) (slicerpc.Response, error) {
		return slicerpc.Response{}, errors.New("missing SSH destination")
	})
	again, err := r.Reconnect(context.Background(), token)
	if err == nil || again.Intent.Status != IntentDisconnected {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	saved, _ := store.Read(token)
	if saved.Status == IntentFailed {
		t.Fatalf("uncertain host creation became failed: %+v", saved)
	}
}

func TestDefiniteHostAbsenceBeforeCreationFailsWithoutFallback(t *testing.T) {
	r, _, local, calls, _ := baseRouter(t, true)
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return slicerpc.Response{SchemaVersion: 1, RequestID: q.RequestID, Outcome: slicerpc.Outcome{Status: slicerpc.StatusUnavailable, Code: "launch_unavailable"}}, nil
	})
	result, err := r.Route(context.Background())
	if err == nil || result.Intent == nil || result.Intent.Status != IntentFailed || *local != 0 || len(*calls) != 1 {
		t.Fatalf("result=%+v local=%d calls=%d err=%v", result, *local, len(*calls), err)
	}
}

func TestDeterministicRoutedLaunchRejectionsAreTerminalWithoutFallback(t *testing.T) {
	for _, outcome := range []slicerpc.Outcome{
		{Status: slicerpc.StatusFailed, Code: "launch_identity_conflict"},
		{Status: slicerpc.StatusInvalid, Code: "invalid_launch_metadata"},
	} {
		t.Run(outcome.Code, func(t *testing.T) {
			r, _, local, calls, _ := baseRouter(t, true)
			r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
				*calls = append(*calls, q)
				return slicerpc.Response{SchemaVersion: slicerpc.SchemaVersion, RequestID: q.RequestID, Outcome: outcome}, nil
			})
			result, err := r.Route(context.Background())
			if err == nil || !result.Routed || result.Local || result.Intent == nil || result.Intent.Status != IntentFailed || result.Code != "failed" || *local != 0 || len(*calls) != 1 {
				t.Fatalf("result=%+v local=%d calls=%d err=%v", result, *local, len(*calls), err)
			}
		})
	}
}

func TestDefiniteHostNonCreationFailsAndNeverFallsBack(t *testing.T) {
	r, _, local, calls, _ := baseRouter(t, true)
	r.Remote = remoteFunc(func(_ context.Context, q slicerpc.Request) (slicerpc.Response, error) {
		*calls = append(*calls, q)
		return response(q, slicerpc.StatusFailed, string(slicerpc.TokenFailed), false), nil
	})
	result, err := r.Route(context.Background())
	if err == nil || result.Intent.Status != IntentFailed || *local != 0 {
		t.Fatalf("result=%+v local=%d err=%v", result, *local, err)
	}
}
func TestCancellationPersistsPendingAndNeverFallsBack(t *testing.T) {
	r, store, local, _, _ := baseRouter(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := r.Route(ctx)
	if err == nil || result.Intent == nil || result.Intent.Status != IntentPending || *local != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, _ := store.Read(result.Intent.Token)
	if saved.Status != IntentPending {
		t.Fatalf("saved=%+v", saved)
	}
}
func TestEnrollmentRefusesMissingAfterUseAndSymlinkedAuthority(t *testing.T) {
	state := t.TempDir()
	store, err := NewStore(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetMode(true); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(store.modePath()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Mode(false); err == nil {
		t.Fatal("missing mode authority was reinitialized")
	}
	if err = os.RemoveAll(store.root); err != nil {
		t.Fatal(err)
	}
	if _, err = NewStore(state); err == nil {
		t.Fatal("missing-after-use authority was reinitialized")
	}
	other := t.TempDir()
	if err = os.Symlink(other, store.root); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Mode(false); err == nil {
		t.Fatal("symlinked authority accepted")
	}
}

func TestModeAndIntentFilesArePrivateAndSessionBounded(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Mode(false)
	if err != nil || m.Enabled {
		t.Fatalf("mode=%+v err=%v", m, err)
	}
	i, err := store.Create("Work", time.Minute, time.Now())
	if err != nil || len(i.SessionName) != 35 {
		t.Fatalf("intent=%+v err=%v", i, err)
	}
	path, _ := store.intentPath(i.Token)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}
