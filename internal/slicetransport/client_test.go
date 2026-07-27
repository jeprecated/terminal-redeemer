package slicetransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/sliceattach"
	"github.com/jmo/terminal-redeemer/internal/slicerpc"
	"github.com/jmo/terminal-redeemer/internal/sourceinventory"
)

func baseClient() Client {
	return Client{Command: "/nix/store/ssh", Host: "operator@host.example", RPCCommand: []string{"/nix/store/redeem", "slice", "rpc"}, Timeout: time.Second, KeepaliveInterval: 15 * time.Second, KeepaliveCount: 3, MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond}
}
func rpcRequest(verb slicerpc.Verb) slicerpc.Request {
	return slicerpc.Request{SchemaVersion: 1, AcceptSchemaVersions: []uint32{1}, RequestID: "req-1", Verb: verb}
}
func okResponse() []byte {
	payload, _ := json.Marshal(slicerpc.Response{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: map[string]any{"alive": true, "schema_versions": []uint32{1}}})
	return append(payload, '\n')
}
func TestClientUsesExactArgvJSONStdinAndNoAuthOverrides(t *testing.T) {
	client := baseClient()
	client.Options = []string{"-p", "2222", "-o", "IdentityAgent=/operator/agent"}
	var command string
	var args []string
	var input []byte
	client.Run = func(_ context.Context, c string, a []string, in []byte) ([]byte, error) {
		command = c
		args = append([]string(nil), a...)
		input = append([]byte(nil), in...)
		return okResponse(), nil
	}
	response, err := client.Call(context.Background(), rpcRequest(slicerpc.VerbLiveness))
	if err != nil || response.Outcome.Status != slicerpc.StatusOK {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	want := []string{"-T", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-p", "2222", "-o", "IdentityAgent=/operator/agent", "--", "operator@host.example", "/nix/store/redeem", "slice", "rpc"}
	if command != "/nix/store/ssh" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command=%q args=%q", command, args)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"sh -lc", "StrictHostKeyChecking", "UserKnownHostsFile", "BatchMode"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("transport changed operator auth boundary: %q", joined)
		}
	}
	var decoded slicerpc.Request
	if err := json.Unmarshal(input, &decoded); err != nil || decoded.RequestID != "req-1" {
		t.Fatalf("stdin=%q err=%v", input, err)
	}
}

type wireHostTransaction struct{}

func (wireHostTransaction) EnsureSession(context.Context, slicerpc.TokenRecord) (bool, error) {
	return false, errors.New("not reached")
}
func (wireHostTransaction) PlanKitty(context.Context, slicerpc.TokenRecord) (sliceattach.ExactSocketIdentity, error) {
	return sliceattach.ExactSocketIdentity{}, errors.New("not reached")
}
func (wireHostTransaction) PrepareKitty(context.Context, slicerpc.TokenRecord) (sliceattach.ExactSocketIdentity, error) {
	return sliceattach.ExactSocketIdentity{}, errors.New("not reached")
}
func (wireHostTransaction) EnsureKitty(context.Context, slicerpc.TokenRecord) (int, uint64, bool, error) {
	return 0, 0, false, errors.New("not reached")
}
func (wireHostTransaction) Place(context.Context, slicerpc.TokenRecord, uint64) error {
	return errors.New("not reached")
}
func (wireHostTransaction) CleanupKitty(context.Context, slicerpc.TokenRecord) error {
	return errors.New("not reached")
}

func TestServerDeterministicLaunchRejectionsTraverseClientWithoutAmbiguity(t *testing.T) {
	store, err := slicerpc.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	epoch := "11111111-1111-4111-8111-111111111111"
	fingerprint := strings.Repeat("a", 64)
	token := "wire-conflict"
	if _, _, err = store.CreatePendingRouted("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", epoch, fingerprint, token, slicerpc.StableSessionName(token), "Work", time.Now()); err != nil {
		t.Fatal(err)
	}
	server := slicerpc.Server{SourceHostID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", SourceEpoch: epoch, SourceFingerprint: fingerprint, Tokens: store, HostTransaction: wireHostTransaction{}}
	for _, tc := range []struct {
		name    string
		payload slicerpc.LaunchPayload
		status  slicerpc.OutcomeStatus
		code    string
	}{
		{name: "identity_conflict", payload: slicerpc.LaunchPayload{Token: token, SessionName: slicerpc.StableSessionName(token), WorkspaceName: "Else"}, status: slicerpc.StatusFailed, code: "launch_identity_conflict"},
		{name: "invalid_metadata", payload: slicerpc.LaunchPayload{Token: "wire-invalid", SessionName: "wrong", WorkspaceName: "Work"}, status: slicerpc.StatusInvalid, code: "invalid_launch_metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := rpcRequest(slicerpc.VerbLaunch)
			request.Payload, _ = json.Marshal(tc.payload)
			client := baseClient()
			client.Run = func(_ context.Context, _ string, _ []string, input []byte) ([]byte, error) {
				decoded, err := slicerpc.DecodeRequest(bytes.NewReader(input))
				if err != nil {
					return nil, err
				}
				var output bytes.Buffer
				if err := slicerpc.EncodeResponse(&output, server.Handle(context.Background(), decoded)); err != nil {
					return nil, err
				}
				return output.Bytes(), nil
			}
			response, err := client.Call(context.Background(), request)
			var ambiguous *AmbiguousError
			if err != nil || errors.As(err, &ambiguous) || response.Outcome.Status != tc.status || response.Outcome.Code != tc.code || response.Result != nil {
				t.Fatalf("response=%#v err=%v", response, err)
			}
		})
	}
}

func TestLaunchAmbiguityIsPendingAndNeverAutomaticallyRetried(t *testing.T) {
	client := baseClient()
	calls := 0
	client.Run = func(context.Context, string, []string, []byte) ([]byte, error) {
		calls++
		return nil, errors.New("connection lost")
	}
	_, err := client.Call(context.Background(), rpcRequest(slicerpc.VerbLaunch))
	var ambiguous *AmbiguousError
	if !errors.As(err, &ambiguous) || ambiguous.Status != slicerpc.StatusPending || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
func TestReadOnlyRetryIsBoundedThenDisconnected(t *testing.T) {
	client := baseClient()
	calls := 0
	sleeps := []time.Duration{}
	client.Run = func(context.Context, string, []string, []byte) ([]byte, error) {
		calls++
		return nil, errors.New("offline")
	}
	client.Sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }
	_, err := client.Call(context.Background(), rpcRequest(slicerpc.VerbSnapshot))
	var ambiguous *AmbiguousError
	if !errors.As(err, &ambiguous) || ambiguous.Status != slicerpc.StatusDisconnected || calls != 3 || !reflect.DeepEqual(sleeps, []time.Duration{time.Millisecond, 2 * time.Millisecond}) {
		t.Fatalf("err=%v calls=%d sleeps=%v", err, calls, sleeps)
	}
}
func TestCancellationStopsBackoff(t *testing.T) {
	client := baseClient()
	client.Run = func(context.Context, string, []string, []byte) ([]byte, error) { return nil, errors.New("offline") }
	client.Sleep = func(ctx context.Context, d time.Duration) error { <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Call(ctx, rpcRequest(slicerpc.VerbSnapshot)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
func TestResponseCorrelationTypingAndResultInvariants(t *testing.T) {
	request := rpcRequest(slicerpc.VerbLiveness)
	cases := []slicerpc.Response{
		{SchemaVersion: 1, Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: map[string]any{"alive": true, "schema_versions": []uint32{1}}},
		{SchemaVersion: 1, RequestID: "stale", Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: map[string]any{"alive": true, "schema_versions": []uint32{1}}},
		{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: "invented"}},
		{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK, Code: "invented"}, Result: map[string]any{"alive": true, "schema_versions": []uint32{1}}},
		{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: map[string]any{"alive": false, "schema_versions": []uint32{1}}},
		{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: slicerpc.StatusUnavailable, Code: "snapshot_unavailable"}, Result: map[string]any{"leak": true}},
		{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: slicerpc.StatusUnavailable, Code: "snapshot_unavailable"}},
	}
	for _, response := range cases {
		payload, _ := json.Marshal(response)
		if _, err := decodeResponse(payload, request); err == nil {
			t.Fatalf("accepted invalid response %#v", response)
		}
	}
	launch := rpcRequest(slicerpc.VerbLaunch)
	raw, _ := json.Marshal(slicerpc.LaunchPayload{Token: "expected"})
	launch.Payload = raw
	wrong := slicerpc.Response{SchemaVersion: 1, RequestID: "req-1", Outcome: slicerpc.Outcome{Status: slicerpc.StatusPending, Code: "launch_pending"}, Result: map[string]any{"token": "stale", "host_terminal_id": "term_x", "launch_status": "pending"}}
	payload, _ := json.Marshal(wrong)
	if _, err := decodeResponse(payload, launch); err == nil {
		t.Fatal("accepted token result for another request")
	}
}

func TestRoutedSuccessRequiresCompleteConsistentTupleForLaunchAndReplay(t *testing.T) {
	token := "route-token"
	session := slicerpc.StableSessionName(token)
	workspace := "Work"
	epoch := "11111111-1111-4111-8111-111111111111"
	sourceID, _ := sourceinventory.SourceID(epoch, 9)
	for _, verb := range []slicerpc.Verb{slicerpc.VerbLaunch, slicerpc.VerbTokenReplay} {
		request := rpcRequest(verb)
		if verb == slicerpc.VerbLaunch {
			request.Payload, _ = json.Marshal(slicerpc.LaunchPayload{Token: token, SessionName: session, WorkspaceName: workspace})
		} else {
			request.Payload, _ = json.Marshal(slicerpc.TokenPayload{Token: token, SessionName: session, WorkspaceName: workspace})
		}
		base := map[string]any{"token": token, "host_terminal_id": "term_route", "launch_status": "launched"}
		five := map[string]any{"token": token, "host_terminal_id": "term_route", "launch_status": "launched", "session_name": session, "workspace_name": workspace}
		for _, result := range []map[string]any{base, five} {
			payload, _ := json.Marshal(slicerpc.Response{SchemaVersion: 1, RequestID: request.RequestID, Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: result})
			if _, err := decodeResponse(payload, request); err == nil {
				t.Fatalf("%s accepted incomplete result %#v", verb, result)
			}
		}
		good := map[string]any{"token": token, "host_terminal_id": "term_route", "launch_status": "launched", "session_name": session, "workspace_name": workspace, "source_id": sourceID, "source_epoch": epoch, "runtime_window_id": 9}
		payload, _ := json.Marshal(slicerpc.Response{SchemaVersion: 1, RequestID: request.RequestID, Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: good})
		if _, err := decodeResponse(payload, request); err != nil {
			t.Fatalf("%s good err=%v", verb, err)
		}
		good["source_id"] = "src_inconsistent"
		payload, _ = json.Marshal(slicerpc.Response{SchemaVersion: 1, RequestID: request.RequestID, Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: good})
		if _, err := decodeResponse(payload, request); err == nil {
			t.Fatalf("%s accepted inconsistent tuple", verb)
		}
		good["source_id"] = sourceID
		good["runtime_window_id"] = 0
		payload, _ = json.Marshal(slicerpc.Response{SchemaVersion: 1, RequestID: request.RequestID, Outcome: slicerpc.Outcome{Status: slicerpc.StatusOK}, Result: good})
		if _, err := decodeResponse(payload, request); err == nil {
			t.Fatalf("%s accepted zero runtime id", verb)
		}
	}
}

func TestWireDisconnectedIsReadOnlyAndMutationsSynthesizePending(t *testing.T) {
	wire := func(requestID string) []byte {
		payload, _ := json.Marshal(slicerpc.Response{SchemaVersion: 1, RequestID: requestID, Outcome: slicerpc.Outcome{Status: slicerpc.StatusDisconnected, Code: "transport_disconnected"}})
		return payload
	}
	for _, verb := range []slicerpc.Verb{slicerpc.VerbLiveness, slicerpc.VerbSnapshot, slicerpc.VerbTokenQuery, slicerpc.VerbTokenReplay} {
		request := rpcRequest(verb)
		if verb == slicerpc.VerbTokenQuery || verb == slicerpc.VerbTokenReplay {
			request.Payload, _ = json.Marshal(slicerpc.TokenPayload{Token: "token"})
		}
		if response, err := decodeResponse(wire(request.RequestID), request); err != nil || response.Outcome.Status != slicerpc.StatusDisconnected {
			t.Fatalf("read verb %s response=%#v err=%v", verb, response, err)
		}
	}
	for _, verb := range []slicerpc.Verb{slicerpc.VerbLaunch, slicerpc.VerbWorkspaceEnsure, slicerpc.Verb("future_unknown")} {
		request := rpcRequest(verb)
		if _, err := decodeResponse(wire(request.RequestID), request); err == nil {
			t.Fatalf("accepted disconnected wire outcome for %s", verb)
		}
	}
	for _, verb := range []slicerpc.Verb{slicerpc.VerbLaunch, slicerpc.VerbWorkspaceEnsure} {
		client := baseClient()
		request := rpcRequest(verb)
		client.Run = func(context.Context, string, []string, []byte) ([]byte, error) { return wire(request.RequestID), nil }
		_, err := client.Call(context.Background(), request)
		var ambiguous *AmbiguousError
		if !errors.As(err, &ambiguous) || ambiguous.Status != slicerpc.StatusPending {
			t.Fatalf("mutation %s err=%v", verb, err)
		}
	}
}

func TestHostileDestinationAndRemoteArgvRejected(t *testing.T) {
	for _, mutate := range []func(*Client){func(c *Client) { c.Host = "host;touch /tmp/pwn" }, func(c *Client) { c.RPCCommand = []string{"redeem;evil", "slice", "rpc"} }, func(c *Client) { c.Timeout = 0 }, func(c *Client) { c.Options = []string{"ok\nInjected"} }, func(c *Client) { c.Options = []string{string([]byte{0xff})} }} {
		client := baseClient()
		mutate(&client)
		if _, err := client.Call(context.Background(), rpcRequest(slicerpc.VerbLiveness)); err == nil {
			t.Fatalf("accepted invalid client: %#v", client)
		}
	}
}
