package slicerpc

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"unicode/utf8"
)

const fuzzRPCRequest = `{"schema_version":1,"accept_schema_versions":[1],"request_id":"r-1","verb":"liveness","payload":{}}`

func FuzzRPCRequest(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(fuzzRPCRequest),
		[]byte(`{"schema_version":1,"accept_schema_versions":[1],"request_id":"r-1","verb":"launch","payload":{"token":"token-1"}}`),
		[]byte(`{"schema_version":0,"request_id":"legacy","verb":"liveness"}`),
		[]byte(`{"schema_version":1,"schema_version":1,"request_id":"r","verb":"liveness"}`),
		[]byte(`{"schema_version":1,"request_id":"bad\u0000id","verb":"liveness"}`),
		[]byte(`{"schema_version":1`), append([]byte(`{"request_id":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Add([]byte{}, uint8(3))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > MaxRequestBytes+1 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 4 {
		case 1:
			payload = append([]byte(fuzzRPCRequest), bytes.Repeat([]byte{' '}, MaxRequestBytes-len(fuzzRPCRequest))...)
		case 2:
			payload = bytes.Repeat([]byte{' '}, MaxRequestBytes+1)
		case 3:
			payload = []byte(`{"schema_version":1,"request_id":"r","request_id":"r","verb":"liveness","payload":{}}`)
		}
		request, err := DecodeRequest(bytes.NewReader(payload))
		if (mode%4 == 2 || mode%4 == 3) && err == nil {
			t.Fatal("accepted oversized or duplicate RPC request")
		}
		if err != nil {
			return
		}
		if !utf8.Valid(payload) || len(request.RequestID) > 128 || !safeID.MatchString(request.RequestID) || request.SchemaVersion != SchemaVersion {
			t.Fatal("accepted unsafe RPC envelope")
		}
		encoded, err := json.Marshal(request)
		if err != nil || len(encoded) > MaxRequestBytes {
			t.Fatalf("accepted RPC request escaped output bound: %v %d", err, len(encoded))
		}
		if _, err := DecodeRequest(bytes.NewReader(encoded)); err != nil {
			t.Fatalf("accepted RPC request did not round-trip: %v", err)
		}
	})
}

func FuzzRPCPayload(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"token":"token-1"}`),
		[]byte(`{"token":"token-1","session_name":"tr-safe","workspace_name":"Work"}`),
		[]byte(`{"token":"token-1","session_name":"` + StableSessionName("token-1") + `","workspace_name":" "}`),
		[]byte(`{"token":"token-1","session_name":"` + StableSessionName("token-1") + `"}`),
		[]byte(`{"token":"token-1","workspace_name":"Work"}`),
		[]byte(`{"token":"token-1","token":"token-2"}`),
		[]byte(`{"token":"0","session_nAme":" "}`),
		[]byte(`{"token":"../unsafe"}`),
		[]byte(`{"token":"nul\u0000token"}`),
		[]byte(`{"legacy_token":"token-1"}`),
		[]byte(`{"token":"token-1"`), append([]byte(`{"token":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte("x"), uint8(1))
	f.Add([]byte("x"), uint8(2))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > MaxRequestBytes {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 3 {
		case 1:
			payload = []byte(`{"token":"token-1","token":"token-2"}`)
		case 2:
			payload = []byte(`{"token":"token-1","unknown":true}`)
		}
		var decoded TokenPayload
		err := DecodePayload(payload, &decoded)
		if mode%3 != 0 && err == nil {
			t.Fatal("accepted duplicate or unknown RPC payload field")
		}
		if err != nil {
			return
		}
		response := (Server{TokenStateUnavailable: true}).Handle(context.Background(), Request{SchemaVersion: SchemaVersion, RequestID: "fuzz", Verb: VerbTokenQuery, Payload: payload})
		routed := decoded.SessionName != "" || decoded.WorkspaceName != ""
		unsafe := !ValidToken(decoded.Token) || routed && (validateSessionName(decoded.SessionName) != nil || decoded.SessionName != StableSessionName(decoded.Token) || validateWorkspaceName(decoded.WorkspaceName) != nil)
		if unsafe && response.Outcome.Status != StatusInvalid {
			t.Fatal("server accepted unsafe or incomplete token/session/workspace identity")
		}
		if !unsafe && response.Outcome.Status == StatusInvalid {
			t.Fatal("server rejected syntactically safe token metadata")
		}
	})
}
