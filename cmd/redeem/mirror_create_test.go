package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmo/terminal-redeemer/internal/config"
	"github.com/jmo/terminal-redeemer/internal/mirror"
)

func TestMirrorNewCreationReceiptPrecedesViewAndNeverReplays(t *testing.T) {
	original := newMirrorSessionName
	defer func() { newMirrorSessionName = original }()
	const session = "redeem-0123456789abcdef0123456789abcdef"
	const id = "ses_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	newMirrorSessionName = func() (string, error) { return session, nil }
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "source-view-fails", true: "creation-receipt-lost"}[lost], func(t *testing.T) {
			root := t.TempDir()
			events := filepath.Join(root, "events")
			viewArgs := filepath.Join(root, "view-args")
			transport := filepath.Join(root, "transport")
			launcher := filepath.Join(root, "launcher")
			createReply := "printf '%s\\n' " + mirror.ShellQuote(`{"session":"`+session+`","session_id":"`+id+`"}`) + "\n"
			if lost {
				createReply = "exit 1\n"
			}
			script := "#!/bin/sh\ncase \"$*\" in\n *session-create*) printf 'create\\n' >> " + mirror.ShellQuote(events) + "\n" + createReply + ";;\n *attach-local*) printf 'source-view\\n' >> " + mirror.ShellQuote(events) + "\nexit 1;;\n *) exit 99;;\nesac\n"
			if err := os.WriteFile(transport, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			script = "#!/bin/sh\nprintf 'view\\n' >> " + mirror.ShellQuote(events) + "\nprintf '%s\\n' \"$@\" > " + mirror.ShellQuote(viewArgs) + "\n"
			if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults()
			cfg.Mirror.SourceHost = "source"
			cfg.Mirror.SSHCommand = transport
			cfg.Mirror.LauncherCommand = launcher
			var out, diagnostic bytes.Buffer
			code := runMirrorNew([]string{"--no-clipboard"}, cfg, &out, &diagnostic)
			data, err := os.ReadFile(events)
			if err != nil {
				t.Fatal(err)
			}
			if lost {
				if code != 1 || string(data) != "create\n" || !strings.Contains(diagnostic.String(), "not retried") {
					t.Fatalf("lost receipt replayed or opened view: code=%d events=%q diagnostic=%s", code, data, diagnostic.String())
				}
				if _, err := os.Stat(viewArgs); !os.IsNotExist(err) {
					t.Fatal("view opened without receipt")
				}
			} else {
				if code != 0 || string(data) != "create\nview\nsource-view\n" || !strings.Contains(diagnostic.String(), "warning:") {
					t.Fatalf("creation/view ordering: code=%d events=%q diagnostic=%s", code, data, diagnostic.String())
				}
				args, err := os.ReadFile(viewArgs)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(args), "session-supervisor\n") || !strings.Contains(string(args), "--session-id\n"+id+"\n") || strings.Contains(string(args), "--create") {
					t.Fatalf("view not bound to creation receipt: %s", args)
				}
			}
		})
	}
}
