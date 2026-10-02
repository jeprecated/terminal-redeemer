package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredLauncherAdapterDryRunsAndExplicitKittyOverride(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("mirror:\n  sourceHost: source-example\n  launcherAdapter: ['/missing/test-adapter', '--group', 'two words']\n  clipboard:\n    enabled: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(root, "snapshot.json")
	payload := `{"host":"source-example","generated_at":"2026-07-10T12:00:00Z","active_zellij_sessions":["existing"],"active_zellij_session_ids":{"existing":"ses_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},"windows":[{"order":0,"source_window_id":1,"app_id":"kitty","zellij_session":"existing"}]}`
	if err := os.WriteFile(snapshotPath, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{{"new", "--dry-run"}, {"open", "--snapshot-file", snapshotPath, "--all", "--dry-run"}}
	for _, command := range commands {
		for _, override := range []bool{false, true} {
			args := append([]string{"--config", configPath, "mirror"}, command...)
			if override {
				args = append(args, "--launcher-command", "explicit-kitty")
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("%v: %d %s", args, code, &stderr)
			}
			if strings.Contains(stdout.String(), "/missing/test-adapter") == override || !strings.Contains(stdout.String(), "session-supervisor") {
				t.Fatalf("wrong launcher: %s", &stdout)
			}
			if !override && (!strings.Contains(stdout.String(), "'--protocol-version=1'") || strings.Contains(stdout.String(), "'--detach'")) {
				t.Fatalf("invalid adapter invocation: %s", &stdout)
			}
		}
	}
}
