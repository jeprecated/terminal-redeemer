package main

import (
	"bytes"
	"github.com/jmo/terminal-redeemer/internal/config"
	"strings"
	"testing"
)

func TestMirrorSessionAttachRejectsCreationAndIncompleteIdentity(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--create"}, {"--session", "s"}, {"--attempt", "../unsafe"}, {"extra"}} {
		var out, diagnostic bytes.Buffer
		code := runMirror(append([]string{"session-attach"}, args...), config.Defaults(), &out, &diagnostic)
		if args[0] == "--help" {
			if code != 0 {
				t.Fatalf("help: %d %s", code, &diagnostic)
			}
			continue
		}
		if code == 0 || strings.Contains(out.String(), ":ready") || out.Len() != 0 {
			t.Fatalf("unsafe helper acceptance: args=%v code=%d stdout=%q", args, code, &out)
		}
	}
}
