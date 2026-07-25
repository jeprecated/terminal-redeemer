package sourceinventory

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func FuzzSourceInventoryStore(f *testing.F) {
	valid, err := json.Marshal(State{StorageVersion: StorageVersion, Initialized: true, SourceHostID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid,
		[]byte(`{"storage_version":1,"storage_version":1,"initialized":true,"source_host_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}`),
		[]byte(`{"storage_Version":1,"initialized":true,"source_host_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}`),
		[]byte(`{"storage_version":0,"initialized":true,"source_host_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}`),
		[]byte(`{"storage_version":1,"initialized":true,"source_host_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","unknown":true}`),
		[]byte(`{"storage_version":1`), append([]byte(`{"source_host_id":"`), 0xff, '"', '}'),
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
			payload = bytes.Repeat([]byte{' '}, MaxStateBytes)
		case 2:
			payload = bytes.Repeat([]byte{' '}, MaxStateBytes+1)
		}
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CreateEnrollmentMarker(); err != nil {
			t.Fatal(err)
		}
		markerBefore, _ := os.ReadFile(store.MarkerPath())
		if err := os.WriteFile(store.Path(), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), payload...)
		state, readErr := store.Read()
		after, _ := os.ReadFile(store.Path())
		markerAfter, _ := os.ReadFile(store.MarkerPath())
		if !bytes.Equal(before, after) || !bytes.Equal(markerBefore, markerAfter) {
			t.Fatal("inventory read rewrote or freshened persisted authority")
		}
		publisher := Publisher{Store: store, UUID: func() (string, error) { return "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", nil }}
		if _, err := publisher.Initialize(); err == nil {
			t.Fatal("used inventory namespace was silently re-enrolled")
		}
		if mode%3 == 2 && readErr == nil {
			t.Fatal("accepted oversized inventory state")
		}
		if readErr == nil {
			if err := state.Validate(); err != nil {
				t.Fatalf("store returned invalid inventory: %v", err)
			}
			encoded, err := json.Marshal(state)
			if err != nil || len(encoded)+1 > MaxStateBytes {
				t.Fatalf("inventory state escaped bound: %v %d", err, len(encoded))
			}
		}
	})
}
