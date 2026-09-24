package mirror

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/bootid"
)

func TestSessionAttachmentRequiresPositiveReadinessNotElapsedTimeOrStdout(t *testing.T) {
	boot, err := bootid.Current()
	if err != nil {
		t.Fatal(err)
	}
	base, id, _ := attachmentSocketFixture(t, "s", boot)
	command := filepath.Join(t.TempDir(), "zellij")
	// A live process emitting terminal output is not evidence of attachment.
	if err := os.WriteFile(command, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'zellij 0.44.3'; exit; fi\necho ordinary-startup-output\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var output bytes.Buffer
	start := time.Now()
	status, err := RunSessionAttachment(context.Background(), SessionAttachConfig{Command: command, SocketBase: base, Session: "s", SessionID: id, Attempt: testAttachmentAttempt, StartupTimeout: 350 * time.Millisecond, Stdin: input, Stdout: &output, Stderr: &output})
	if err == nil || status != "failed" || !strings.Contains(err.Error(), "readiness deadline") || time.Since(start) > 2*time.Second {
		t.Fatalf("status=%s err=%v duration=%s", status, err, time.Since(start))
	}
	if !strings.Contains(output.String(), "ordinary-startup-output") || strings.Contains(output.String(), AttachmentMarker(testAttachmentAttempt, "ready")) {
		t.Fatalf("false readiness: %q", output.String())
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 {
		t.Fatalf("attempt cleanup: %v %v", entries, err)
	}
}

func TestSessionAttachmentSeparatesUnsupportedAndUnverifiable(t *testing.T) {
	boot, err := bootid.Current()
	if err != nil {
		t.Fatal(err)
	}
	base, id, _ := attachmentSocketFixture(t, "s", boot)
	command := filepath.Join(t.TempDir(), "zellij")
	cfg := SessionAttachConfig{Command: command, SocketBase: base, Session: "s", SessionID: id, Attempt: testAttachmentAttempt, StartupTimeout: time.Second}
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'zellij unsupported'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if status, err := RunSessionAttachment(context.Background(), cfg); status != "unsupported" || err == nil {
		t.Fatalf("unsupported version: %s %v", status, err)
	}
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'zellij 0.44.3'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "contract_version_1", "s")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if status, err := RunSessionAttachment(context.Background(), cfg); status != "unverifiable" || err == nil {
		t.Fatalf("unsafe socket: %s %v", status, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if status, err := RunSessionAttachment(ctx, cfg); status != "cancelled" || err == nil {
		t.Fatalf("cancelled: %s %v", status, err)
	}
}

func TestAttachmentEnvironmentScrubsContextAndResurrectionCache(t *testing.T) {
	base, id, _ := attachmentSocketFixture(t, "s", "boot")
	pin, err := pinSessionSocket(base, "s", id, "boot")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	env := attachmentEnvironment([]string{"ZELLIJ=1", "ZELLIJ_SESSION_NAME=wrong", "ZELLIJ_PANE_ID=99", "ZELLIJ_CONFIG_DIR=/untrusted", "ZELLIJ_SOCKET_DIR=/wrong", "XDG_CACHE_HOME=/old", "TERM=xterm", "PATH=/bin"}, pin)
	text := strings.Join(env, "\n")
	for _, bad := range []string{"ZELLIJ=1", "ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID", "ZELLIJ_CONFIG_DIR", "/wrong", "/old"} {
		if strings.Contains(text, bad) {
			t.Fatalf("inherited context %s", bad)
		}
	}
	if !strings.Contains(text, "TERM=xterm") || !strings.Contains(text, "ZELLIJ_SOCKET_DIR="+pin.view) {
		t.Fatalf("bad environment: %v", env)
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(pin.view), "cache"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("cache not isolated: %v %v", entries, err)
	}
}

func TestPlanSessionAttachmentPreservesPrefixAndHasNoCreationAuthority(t *testing.T) {
	cfg := RemoteConfig{Host: "source", SSHCommand: "custom-ssh", SSHOptions: []string{"-o", "BatchMode=no"}, SnapshotCommand: []string{"wrapper", "/absolute/redeem", "--profile", "source-profile", "mirror", "snapshot"}}
	for _, name := range []string{"-Leading", "Case Sensitive"} {
		id := activeCatalog(name).Sessions[name].ExactID
		command, err := PlanSessionAttachment(cfg, name, id, testAttachmentAttempt)
		if err != nil {
			t.Fatal(err)
		}
		argv := strings.Join(command.Args, " ")
		if command.Name != "custom-ssh" || !strings.Contains(argv, "-tt") || strings.Contains(argv, "--create") || strings.Contains(argv, " -n ") {
			t.Fatalf("unsafe command: %+v", command)
		}
		remote := command.Args[len(command.Args)-1]
		want := QuoteCommand([]string{"wrapper", "/absolute/redeem", "--profile", "source-profile", "mirror", "session-attach", "--session", name, "--session-id", id, "--attempt", testAttachmentAttempt})
		if remote != want {
			t.Fatalf("lost wrapper/name: %s", remote)
		}
		if strings.Index(argv, "BatchMode=yes") > strings.Index(argv, "BatchMode=no") {
			t.Fatal("interactive SSH option won")
		}
	}
	cfg.SnapshotCommand = []string{"custom-snapshot"}
	if _, err := PlanSessionAttachment(cfg, "s", activeCatalog("s").Sessions["s"].ExactID, testAttachmentAttempt); err == nil {
		t.Fatal("guessed helper executable")
	}
}
