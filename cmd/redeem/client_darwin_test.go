package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinClientUsesConfiguredSourceWithoutStartingSSH(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("mirror:\n  sourceHost: source-example\n  sshCommand: /does-not-exist\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--config", path, "mirror", "new", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d: %s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "source-example") || !strings.Contains(stdout.String(), "session-supervisor") {
		t.Fatalf("missing configured source or guarded attach: %s", &stdout)
	}
	if strings.Contains(stdout.String(), "paste-image") {
		t.Fatalf("Linux clipboard mapping on macOS: %s", &stdout)
	}
}

func TestDarwinClientRejectsLinuxOperations(t *testing.T) {
	for _, command := range [][]string{{"capture", "once"}, {"resume", "--all"}, {"mirror", "snapshot"}, {"mirror", "session-create"}, {"mirror", "follow"}, {"mirror", "save"}} {
		var stdout, stderr bytes.Buffer
		if code := run(command, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "require Linux") {
			t.Fatalf("%v: code=%d stderr=%s", command, code, &stderr)
		}
	}
}
