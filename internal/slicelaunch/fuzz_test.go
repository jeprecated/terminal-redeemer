package slicelaunch

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func fuzzIntent() Intent {
	now := time.Unix(1, 0).UTC()
	token := strings.Repeat("a", 64)
	return Intent{StorageVersion: StorageVersion, Token: token, SessionName: SessionName(token), WorkspaceName: "Work", Status: IntentPending, CreatedAt: now, UpdatedAt: now, RetryExpiresAt: now.Add(time.Minute)}
}

func FuzzRoutedIntentJournal(f *testing.F) {
	valid, err := json.Marshal(fuzzIntent())
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid,
		[]byte(`{"storage_version":1,"storage_version":1}`),
		[]byte(`{"storage_version":0,"token":"legacy"}`),
		[]byte(`{"storage_version":1,"unknown":true}`),
		[]byte(`{"storage_version":1,"token":"../unsafe"}`),
		[]byte(`{"storage_version":1`), append([]byte(`{"token":"`), 0xff, '"', '}'),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > MaxIntentBytes+1 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 3 {
		case 1:
			payload = bytes.Repeat([]byte{' '}, MaxIntentBytes)
		case 2:
			payload = bytes.Repeat([]byte{' '}, MaxIntentBytes+1)
		}
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetMode(true); err != nil {
			t.Fatal(err)
		}
		intent := fuzzIntent()
		path, _ := store.intentPath(intent.Token)
		if err := writeExclusive(path, intent); err != nil {
			t.Fatal(err)
		}
		markerBefore, _ := os.ReadFile(store.marker)
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), payload...)
		decoded, readErr := store.Read(intent.Token)
		after, _ := os.ReadFile(path)
		markerAfter, _ := os.ReadFile(store.marker)
		if !bytes.Equal(before, after) || !bytes.Equal(markerBefore, markerAfter) {
			t.Fatal("routed journal read rewrote or freshened persisted input")
		}
		if readErr != nil {
			if err := store.Write(intent); err == nil {
				t.Fatal("rejected routed journal was silently replaced")
			}
			again, _ := os.ReadFile(path)
			if !bytes.Equal(before, again) {
				t.Fatal("rejected routed journal changed during write refusal")
			}
			return
		}
		if mode%3 == 2 {
			t.Fatal("accepted oversized routed journal")
		}
		if err := decoded.Validate(); err != nil || decoded.Token != intent.Token {
			t.Fatalf("store returned invalid routed intent: %v", err)
		}
	})
}

func FuzzRoutedIntentValidation(f *testing.F) {
	for _, workspace := range []string{"Work", " Ｗｏｒｋ ", "\x00", "line\nfeed", strings.Repeat("x", 255), strings.Repeat("x", 256), string([]byte{0xff})} {
		f.Add(workspace, int64(0), uint64(0))
	}
	f.Fuzz(func(t *testing.T, workspace string, attempt int64, runtimeID uint64) {
		if len(workspace) > 1024 {
			return
		}
		intent := fuzzIntent()
		intent.WorkspaceName = workspace
		intent.Attempt = int(attempt)
		intent.RuntimeWindowID = runtimeID
		first := intent.Validate()
		second := intent.Validate()
		if (first == nil) != (second == nil) {
			t.Fatal("intent validation is not deterministic")
		}
		unsafe := !utf8.ValidString(workspace) || strings.TrimSpace(workspace) == "" || len(workspace) > 255 || strings.ContainsAny(workspace, "\x00\r\n") || attempt < 0 || attempt > 100 || runtimeID != 0
		if unsafe && first == nil {
			t.Fatal("accepted unsafe workspace, attempt, or incomplete routed source tuple")
		}
	})
}
