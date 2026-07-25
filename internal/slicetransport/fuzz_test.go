package slicetransport

import (
	"bytes"
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/jmo/terminal-redeemer/internal/slicerpc"
)

const fuzzLivenessResponse = `{"schema_version":1,"request_id":"fuzz-1","outcome":{"status":"ok"},"result":{"alive":true,"schema_versions":[1]}}`

func FuzzRPCResponse(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(fuzzLivenessResponse),
		[]byte(`{"schema_version":1,"request_id":"fuzz-1","outcome":{"status":"unavailable","code":"niri_version_unavailable"}}`),
		[]byte(`{"schema_version":0,"request_id":"fuzz-1","outcome":{"status":"ok"},"result":{"alive":true,"schema_versions":[1]}}`),
		[]byte(`{"schema_version":1,"request_id":"fuzz-1","request_id":"fuzz-1","outcome":{"status":"ok"}}`),
		[]byte(`{"schema_version":1,"request_id":"fuzz-1","unknown":true,"outcome":{"status":"ok"}}`),
		[]byte(`{"schema_version":1,"request_id":"fuzz-1","outcome":{"status":"mystery"}}`),
		[]byte(`{"schema_version":1`), append([]byte(`{"request_id":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Add([]byte{}, uint8(3))
	request := slicerpc.Request{SchemaVersion: slicerpc.SchemaVersion, AcceptSchemaVersions: []uint32{slicerpc.SchemaVersion}, RequestID: "fuzz-1", Verb: slicerpc.VerbLiveness, Payload: json.RawMessage(`{}`)}
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > MaxResponseBytes+1 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 4 {
		case 1:
			payload = append([]byte(fuzzLivenessResponse), bytes.Repeat([]byte{' '}, MaxResponseBytes-len(fuzzLivenessResponse))...)
		case 2:
			payload = bytes.Repeat([]byte{' '}, MaxResponseBytes+1)
		case 3:
			payload = []byte(`{"schema_version":1,"request_id":"fuzz-1","request_id":"fuzz-1","outcome":{"status":"ok"},"result":{"alive":true,"schema_versions":[1]}}`)
		}
		response, err := decodeResponse(payload, request)
		if (mode%4 == 2 || mode%4 == 3) && err == nil {
			t.Fatal("accepted oversized or duplicate RPC response")
		}
		if err != nil {
			return
		}
		if !utf8.Valid(payload) || response.RequestID != request.RequestID || response.SchemaVersion != slicerpc.SchemaVersion {
			t.Fatal("accepted unsafe RPC response envelope")
		}
		encoded, err := json.Marshal(response)
		if err != nil || len(encoded) > MaxResponseBytes {
			t.Fatalf("accepted response escaped output bound: %v %d", err, len(encoded))
		}
	})
}
