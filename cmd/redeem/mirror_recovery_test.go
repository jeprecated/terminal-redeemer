package main

import (
	"bytes"
	"testing"
)

func TestPrivateRecoveryFlagsFailBeforeEffects(t *testing.T) {
	for _, supervisor := range []bool{false, true} {
		var out, err bytes.Buffer
		if code := runMirrorRecovery([]string{"--help"}, supervisor, &out, &err); code != 0 {
			t.Fatalf("help=%d %s", code, err.String())
		}
		for _, args := range [][]string{nil, {"--remote-json", "{"}, {"--remote-json", "{}", "unexpected"}} {
			err.Reset()
			if code := runMirrorRecovery(args, supervisor, &out, &err); code != 2 {
				t.Fatalf("args %v code=%d", args, code)
			}
		}
	}
	var out, err bytes.Buffer
	if code := runMirrorRecovery([]string{"--remote-json", "{}", "--lock-fd", "0", "--ready-fd", "1"}, false, &out, &err); code != 2 {
		t.Fatal("accepted physical descriptors as startup authority")
	}
}
