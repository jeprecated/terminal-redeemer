package slicerpc

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func fuzzTokenRecord() TokenRecord {
	now := time.Unix(1, 0).UTC()
	return TokenRecord{StorageVersion: 1, Token: "token-fuzz", HostTerminalID: StableTerminalID("host", "token-fuzz"), Status: TokenPending, CreatedAt: now, UpdatedAt: now}
}

func FuzzHostTokenJournal(f *testing.F) {
	valid, err := json.Marshal(fuzzTokenRecord())
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid,
		[]byte(`{"storage_version":1,"storage_version":1,"token":"token-fuzz"}`),
		[]byte(`{"storage_version":0,"token":"token-fuzz","host_terminal_id":"legacy","status":"pending"}`),
		[]byte(`{"storage_version":1,"token":"../unsafe","host_terminal_id":"term","status":"pending"}`),
		[]byte(`{"storage_version":1,"unknown":true}`),
		[]byte(`{"storage_version":1`), append([]byte(`{"token":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > MaxTokenRecordBytes+1 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 3 {
		case 1:
			payload = bytes.Repeat([]byte{' '}, MaxTokenRecordBytes)
		case 2:
			payload = bytes.Repeat([]byte{' '}, MaxTokenRecordBytes+1)
		}
		root := t.TempDir()
		store, err := NewTokenStore(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.CreatePending("host", "token-fuzz", time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
		path, _ := store.path("token-fuzz")
		markerBefore, _ := os.ReadFile(store.marker)
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), payload...)
		record, readErr := store.Read("token-fuzz")
		after, _ := os.ReadFile(path)
		markerAfter, _ := os.ReadFile(store.marker)
		if !bytes.Equal(before, after) || !bytes.Equal(markerBefore, markerAfter) {
			t.Fatal("token journal read rewrote or freshened persisted input")
		}
		if readErr != nil {
			if _, created, err := store.CreatePending("host", "token-fuzz", time.Now()); err == nil || created {
				t.Fatal("rejected token journal was silently re-enrolled")
			}
			again, _ := os.ReadFile(path)
			if !bytes.Equal(before, again) {
				t.Fatal("rejected token journal changed during create refusal")
			}
			return
		}
		if mode%3 == 2 {
			t.Fatal("accepted oversized token journal")
		}
		if err := record.Validate(); err != nil || record.Token != "token-fuzz" {
			t.Fatalf("store returned invalid token journal: %v", err)
		}
	})
}

func FuzzTokenRecordValidation(f *testing.F) {
	for _, seed := range []string{"", "/run/user/1000/exact", "relative", "/tmp/../unsafe", "/tmp/nul\x00path", "/tmp/line\npath", string([]byte{'/', 0xff})} {
		f.Add(seed, uint8(0))
	}
	f.Add("", uint8(1))
	f.Add("", uint8(2))
	f.Fuzz(func(t *testing.T, path string, mode uint8) {
		if len(path) > 8192 {
			return
		}
		record := fuzzTokenRecord()
		record.SessionName = StableSessionName(record.Token)
		record.WorkspaceName = "Work"
		record.Stage = "socket_planned"
		record.PreparedSocketDevice = 1
		record.PreparedSocketInode = 1
		switch mode % 3 {
		case 1:
			path = "/" + strings.Repeat("x", 4095)
		case 2:
			path = "/" + strings.Repeat("x", 4096)
		}
		record.PreparedSocketPath = path
		first := record.Validate()
		second := record.Validate()
		if (first == nil) != (second == nil) {
			t.Fatal("token validation is not deterministic")
		}
		if mode%3 == 2 && first == nil {
			t.Fatal("accepted over-limit prepared path")
		}
		if first == nil && (len(path) > 4096 || !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n")) {
			t.Fatal("accepted unsafe prepared path")
		}
	})
}
