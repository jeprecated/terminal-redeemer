package mirror

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestPasteBridgeUploadAndFallbackKeepOriginalAttachment(t *testing.T) {
	h := sessionTerminalFixture(t)
	const token = "0123456789abcdef0123456789abcdef"
	if _, err := BindSessionInput(context.Background(), h.remote, h.root, token); err == nil {
		t.Fatal("offline clipboard operation began")
	}
	h.control.change(func() { h.control.permits = 1 })
	first := h.control.attempt(t, 1)
	h.ready(t, first)
	input, err := BindSessionInput(context.Background(), h.remote, h.root, token)
	if err != nil {
		t.Fatal(err)
	}
	var second string
	runner := &recordingRunner{outputs: []outputResult{{data: []byte("image/png\n")}, {data: []byte("png")}}}
	runner.onRun = func(command Command) {
		if command.Name != "scp" {
			return
		}
		// The upload completes only after the physical view has reattached.
		h.mark(t, first, ".exit")
		awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return len(h.control.lost) > 0 })
		h.control.change(func() { h.control.permits = 2 })
		second = h.control.attempt(t, 2)
		h.ready(t, second)
	}
	cfg := PasteConfig{SourceHost: h.remote.Host, SSHCommand: "ssh", SCPCommand: "scp", ClipboardCommand: "wl-paste", TempDir: t.TempDir(), MIMETypes: []string{"image/png"}}
	_, err = (PasteBridge{Runner: runner, Input: input}).Paste(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "paste discarded") {
		t.Fatalf("late upload result: %v", err)
	}
	for _, call := range runner.runCalls {
		if call.Name == "kitty" {
			t.Fatal("persistent paste escaped through Kitty")
		}
	}
	files, err := os.ReadDir(cfg.TempDir)
	if err != nil || len(files) != 0 {
		t.Fatalf("local upload not cleaned up: %v %v", files, err)
	}
	fallback := &recordingRunner{outputs: []outputResult{{data: []byte("text/plain\n")}}}
	if _, err := (PasteBridge{Runner: fallback, Input: input}).Paste(context.Background(), cfg); err == nil {
		t.Fatal("stale Ctrl-V fallback admitted")
	}
	if len(fallback.runCalls) != 0 {
		t.Fatal("fallback bypassed the generation-bound sink")
	}
	if got := h.received(second); got != "" {
		t.Fatalf("old clipboard work reached new transport: %q", got)
	}
	fresh, err := BindSessionInput(context.Background(), h.remote, h.root, token)
	if err != nil {
		t.Fatal(err)
	}
	fallback.outputs = []outputResult{{data: []byte("text/plain\n")}}
	result, err := (PasteBridge{Runner: fallback, Input: fresh}).Paste(context.Background(), cfg)
	if err != nil || !result.FellBack {
		t.Fatalf("fresh fallback: %+v %v", result, err)
	}
	awaitSession(t, func() bool { return h.received(second) == "\x16" })
}
