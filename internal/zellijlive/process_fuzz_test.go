package zellijlive

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzProcessArgvEnvironmentMetadata(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("zellij\x00attach\x00--\x00safe-session\x00"),
		[]byte("zellij\x00--session\x00-leading\x00"),
		[]byte("zellij\x00attach\x00../unsafe\x00"),
		[]byte("zellij\x00attach\x00nul\x00name\x00"),
		[]byte("zellij\x00attach\x00line\nname\x00"),
		[]byte("legacy --session shell-text"),
		[]byte("zellij\x00attach\x00" + strings.Repeat("s", 255) + "\x00"),
		[]byte("zellij\x00attach\x00" + strings.Repeat("s", 256) + "\x00"),
		append([]byte("zellij\x00attach\x00"), 0xff, 0),
	} {
		f.Add(seed, uint8(0))
	}
	f.Add([]byte{}, uint8(1))
	f.Add([]byte{}, uint8(2))
	f.Fuzz(func(t *testing.T, input []byte, mode uint8) {
		if len(input) > maxProcessMetadataBytes+1 {
			return
		}
		payload := append([]byte(nil), input...)
		switch mode % 3 {
		case 1:
			payload = bytes.Repeat([]byte{'x'}, maxProcessMetadataBytes)
		case 2:
			payload = bytes.Repeat([]byte{'x'}, maxProcessMetadataBytes+1)
		}
		path := t.TempDir() + "/metadata"
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		parts, err := readNull(path)
		if mode%3 == 2 && err == nil {
			t.Fatal("accepted over-limit process metadata")
		}
		if !utf8.Valid(payload) && err == nil {
			t.Fatal("accepted invalid UTF-8 process metadata")
		}
		if err != nil {
			return
		}
		total := 0
		for _, part := range parts {
			total += len(part)
			if part == "" || !utf8.ValidString(part) || strings.ContainsRune(part, 0) {
				t.Fatal("decoded unsafe process argv/environment entry")
			}
		}
		if total > maxProcessMetadataBytes {
			t.Fatal("decoded process metadata escaped byte bound")
		}
		if len(parts) > 0 {
			environ := []string{"ZELLIJ_SESSION_NAME=unsafe\x00session", "ZELLIJ_SESSION_NAME=safe-session"}
			for _, candidate := range candidatesFrom(parts, environ) {
				if SafeSessionName(candidate) && (strings.ContainsAny(candidate, "\x00\r\n/") || len(candidate) > 255) {
					t.Fatalf("unsafe process candidate classified safe: %q", candidate)
				}
			}
		}
	})
}
