package mirror

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSessionLocalOriginRejectsOfflineConnectingAndDelayedPaste(t *testing.T) {
	h := sessionTerminalFixture(t)
	const token = "0123456789abcdef0123456789abcdef"
	status := func() SessionLocalState {
		t.Helper()
		s, pid, err := SessionLocalExchange(context.Background(), h.remote, h.root, token, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid != os.Getpid() || s.Session != "original" || s.SessionID == "" {
			t.Fatalf("wrong helper: %+v pid=%d", s, pid)
		}
		return s
	}
	send := func(origin SessionInputOrigin, text string) error {
		_, _, err := SessionLocalExchange(context.Background(), h.remote, h.root, token, &origin, []byte(text))
		return err
	}
	offline := status()
	if offline.Origin.Generation != 0 || send(offline.Origin, "offline") == nil {
		t.Fatal("offline input admitted")
	}
	h.control.change(func() { h.control.permits = 1 })
	first := h.control.attempt(t, 1)
	if send(status().Origin, "connecting") == nil {
		t.Fatal("connecting input admitted")
	}
	h.ready(t, first)
	original := status()
	if original.State != "ready" || original.Origin.Generation == 0 {
		t.Fatalf("missing ready origin: %+v", original)
	}
	if err := send(original.Origin, "fresh-one"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool { return strings.Contains(h.received(first), "fresh-one") })
	h.mark(t, first, ".exit")
	awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return len(h.control.lost) > 0 })
	h.control.change(func() { h.control.permits = 2 })
	second := h.control.attempt(t, 2)
	if send(original.Origin, "late-during-connect") == nil {
		t.Fatal("old origin admitted while connecting")
	}
	h.ready(t, second)
	current := status()
	if current.Origin == original.Origin {
		t.Fatal("attachment retained input generation")
	}
	// These represent upload completion and text fallback after reconnection.
	for _, text := range []string{"/tmp/delayed-image.png", "\x16"} {
		if send(original.Origin, text) == nil {
			t.Fatal("stale clipboard operation admitted")
		}
	}
	foreign := current.Origin
	foreign.Client = "ffffffffffffffffffffffffffffffff"
	if send(foreign, "wrong-helper") == nil {
		t.Fatal("foreign helper instance admitted")
	}
	if err := send(current.Origin, "fresh-two"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, func() bool { return strings.Contains(h.received(second), "fresh-two") })
	if got := h.received(second); got != "fresh-two" {
		t.Fatalf("stale input reached replacement: %q", got)
	}
	if duplicate, err := StartSessionLocal(context.Background(), h.remote, h.root, token); err == nil {
		duplicate.Close()
		t.Fatal("duplicate helper endpoint admitted")
	}
	_ = status() // a rejected duplicate must not unlink the original endpoint
	other := h.remote
	other.SSHOptions = []string{"-p", "2222"}
	if _, _, err := SessionLocalExchange(context.Background(), other, h.root, token, nil, nil); err == nil {
		t.Fatal("transport identity leaked across endpoint namespaces")
	}
}
