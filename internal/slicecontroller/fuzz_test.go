package slicecontroller

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

const fuzzControlRequest = `{"schema_version":2,"request_id":"fuzz-1","verb":"status","payload":{}}`
const fuzzControlResponse = `{"schema_version":2,"request_id":"fuzz-1","outcome":{"status":"error","code":"controller_unavailable"}}`

func FuzzControlRequestAndResponse(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(fuzzControlRequest), []byte(fuzzControlResponse),
		[]byte(`{"schema_version":1,"request_id":"legacy","verb":"status"}`),
		[]byte(`{"schema_version":2,"request_id":"fuzz-1","request_id":"other","verb":"status"}`),
		[]byte(`{"schema_version":2,"request_Id":"fuzz-1","verb":"status"}`),
		[]byte(`{"schema_version":2,"request_id":"fuzz-1","verb":"status","unknown":true}`),
		[]byte(`{"schema_version":2,"request_id":"bad\u0000id","verb":"status"}`),
		[]byte(`{"schema_version":2`), append([]byte(`{"request_id":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Add([]byte{}, uint8(3))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > MaxControlResponseBytes+1 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 4 {
		case 1:
			payload = []byte(`{"schema_version":2,"request_id":"fuzz-1","request_id":"other","verb":"status"}`)
		case 2:
			payload = []byte(`{"schema_version":2,"request_id":"fuzz-1","verb":"status","unknown":true}`)
		case 3:
			payload = append([]byte(fuzzControlResponse), []byte(` {}`)...)
		}
		request, requestErr := DecodeControlRequest(bytes.NewReader(payload))
		if requestErr == nil {
			if !utf8.Valid(payload) || request.SchemaVersion != SchemaVersion || !safeRequestID.MatchString(request.RequestID) {
				t.Fatal("accepted unsafe control request")
			}
			encoded, err := json.Marshal(request)
			if err != nil || len(encoded) > MaxControlBytes {
				t.Fatalf("control request escaped bound: %v %d", err, len(encoded))
			}
		}
		response, responseErr := decodeControlResponse(payload, "fuzz-1")
		if mode%4 != 0 && (requestErr == nil || responseErr == nil) {
			t.Fatal("accepted duplicate, unknown, or trailing control JSON")
		}
		if responseErr == nil {
			encoded, err := json.Marshal(response)
			if err != nil || len(encoded) > MaxControlResponseBytes {
				t.Fatalf("control response escaped bound: %v %d", err, len(encoded))
			}
		}
	})
}

func FuzzControllerStateStore(f *testing.F) {
	validState := NewState(Namespace{Host: "host", Leech: "leech"}, "controller-fuzz")
	valid, err := json.Marshal(validState)
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid,
		[]byte(`{"schema_version":2,"schema_version":2}`),
		[]byte(`{"schema_version":1,"namespace":{"host":"host","leech":"leech"}}`),
		[]byte(`{"schema_version":2,"unknown":true}`),
		[]byte(`{"schema_version":2`), append([]byte(`{"controller_id":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > 1<<20 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 3 {
		case 1:
			payload = bytes.Repeat([]byte{' '}, MaxControllerStateBytes)
		case 2:
			payload = bytes.Repeat([]byte{' '}, MaxControllerStateBytes+1)
		}
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Initialize(Namespace{Host: "host", Leech: "leech"}); err != nil {
			t.Fatal(err)
		}
		markerBefore, _ := os.ReadFile(store.marker)
		if err := os.WriteFile(store.current, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), payload...)
		state, readErr := store.Read()
		after, _ := os.ReadFile(store.current)
		markerAfter, _ := os.ReadFile(store.marker)
		if !bytes.Equal(before, after) || !bytes.Equal(markerBefore, markerAfter) {
			t.Fatal("persisted state read rewrote or freshened rejected authority")
		}
		if _, err := store.Initialize(Namespace{Host: "host", Leech: "leech"}); !errors.Is(err, ErrAlreadyInitialized) {
			t.Fatalf("persisted input was silently re-enrolled: %v", err)
		}
		if mode%3 == 2 && readErr == nil {
			t.Fatal("accepted oversized controller state")
		}
		if readErr != nil {
			return
		}
		if err := state.Validate(); err != nil {
			t.Fatalf("store returned invalid state: %v", err)
		}
		encoded, err := json.Marshal(state)
		if err != nil || len(encoded)+1 > MaxControllerStateBytes {
			t.Fatalf("controller state escaped bound: %v %d", err, len(encoded))
		}
	})
}

func FuzzProjectionArgv(f *testing.F) {
	f.Add([]byte("kitty\x00--class"), uint8(0))
	f.Add([]byte("kitty\x00--session\x00safe"), uint8(0))
	f.Add([]byte("kitty\x00../unsafe"), uint8(0))
	f.Add(append([]byte("kitty\x00"), 0xff), uint8(0))
	f.Add([]byte("kitty\x00"+strings.Repeat("x", MaxProjectionArgvEntryBytes)), uint8(0))
	f.Add([]byte("kitty\x00"+strings.Repeat("x", MaxProjectionArgvEntryBytes+1)), uint8(0))
	f.Add([]byte("kitty\x00bad\x00arg"), uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Add([]byte{}, uint8(3))
	f.Fuzz(func(t *testing.T, raw []byte, mode uint8) {
		if len(raw) > MaxProjectionArgvTotalBytes+1 {
			return
		}
		argv := strings.Split(string(raw), "\x00")
		switch mode % 4 {
		case 1:
			argv = []string{"kitty", "bad\x00arg"}
		case 2:
			argv = projectionArgvWithBytes(MaxProjectionArgvTotalBytes)
		case 3:
			argv = projectionArgvWithBytes(MaxProjectionArgvTotalBytes + 1)
		}
		first := ValidateProjectionArgv(argv)
		second := ValidateProjectionArgv(append([]string(nil), argv...))
		if (first == nil) != (second == nil) {
			t.Fatal("argv validation is not deterministic")
		}
		if mode%4 == 1 && first == nil || mode%4 == 3 && first == nil {
			t.Fatal("unsafe or over-limit argv accepted")
		}
		if first != nil {
			return
		}
		if len(argv) < 2 || len(argv) > MaxProjectionArgvEntries {
			t.Fatal("accepted argv count outside bound")
		}
		total := 0
		for _, entry := range argv {
			total += len(entry)
			if entry == "" || !utf8.ValidString(entry) || len(entry) > MaxProjectionArgvEntryBytes || strings.ContainsAny(entry, "\x00\r\n") {
				t.Fatal("accepted unsafe argv entry")
			}
		}
		if total > MaxProjectionArgvTotalBytes {
			t.Fatal("accepted aggregate argv over bound")
		}
	})
}
