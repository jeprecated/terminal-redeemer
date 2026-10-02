package mirror

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

const testAttachmentAttempt = "0123456789abcdef0123456789abcdef"

func TestManualSessionRequiresExplicitRetry(t *testing.T) {
	control := &manualSessionControl{}
	first := strings.Repeat("a", 32)
	next := strings.Repeat("b", 32)
	request := SessionControlRequest{Attempt: first}
	reply, err := control.Exchange(context.Background(), request)
	if err != nil || reply.Grant == nil || reply.Grant.Attempt != first || !reply.Grant.Deadline.After(time.Now()) {
		t.Fatalf("initial grant: %+v %v", reply, err)
	}
	request.Event = "lost"
	if reply, err = control.Exchange(context.Background(), request); err != nil || reply.Grant != nil {
		t.Fatalf("loss restarted: %+v %v", reply, err)
	}
	request = SessionControlRequest{Attempt: next}
	for count := 0; count < 10; count++ {
		if reply, err = control.Exchange(context.Background(), request); err != nil || reply.Grant != nil || reply.Ended {
			t.Fatalf("automatic retry or fabricated end: %+v %v", reply, err)
		}
	}
	request.Retry = true
	if reply, err = control.Exchange(context.Background(), request); err != nil || reply.Grant == nil || reply.Grant.Attempt != next {
		t.Fatalf("explicit retry: %+v %v", reply, err)
	}
	deadline := reply.Grant.Deadline
	if reply, err = control.Exchange(context.Background(), request); err != nil || reply.Grant == nil || reply.Grant.Attempt != next || !reply.Grant.Deadline.Equal(deadline) {
		t.Fatalf("duplicate retry: %+v %v", reply, err)
	}
}

func TestManualSessionReplaysLostGrantWithoutRenewingDeadline(t *testing.T) {
	control := &manualSessionControl{}
	request := SessionControlRequest{Attempt: strings.Repeat("a", 32)}
	initial, _ := control.Exchange(context.Background(), request)
	replayed, err := control.Exchange(context.Background(), request)
	if err != nil || replayed.Grant == nil || *replayed.Grant != *initial.Grant {
		t.Fatalf("lost grant could not be recovered: %+v %v", replayed, err)
	}
}

func TestManualSessionRejectsCancelledOrInvalidGrant(t *testing.T) {
	control := &manualSessionControl{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := control.Exchange(ctx, SessionControlRequest{Attempt: strings.Repeat("a", 32)}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := control.Exchange(context.Background(), SessionControlRequest{Attempt: "invalid"}); err == nil {
		t.Fatal("invalid attempt accepted")
	}
}

func TestManualSessionRetryDuringLossReport(t *testing.T) {
	control := &manualSessionControl{}
	first, next := strings.Repeat("a", 32), strings.Repeat("b", 32)
	_, _ = control.Exchange(context.Background(), SessionControlRequest{Attempt: first})
	reply, err := control.Exchange(context.Background(), SessionControlRequest{Attempt: first, Event: "lost", Retry: true})
	if err != nil || reply.Grant != nil {
		t.Fatalf("loss reply: %+v %v", reply, err)
	}
	reply, err = control.Exchange(context.Background(), SessionControlRequest{Attempt: next})
	if err != nil || reply.Grant == nil || reply.Grant.Attempt != next {
		t.Fatalf("lost retry: %+v %v", reply, err)
	}
}

func TestManualSessionPTYReconnectDiscardsOfflineInput(t *testing.T) {
	control := &manualSessionControl{}
	grants := make(chan string, 4)
	ready := make(chan string, 4)
	lastGrant := ""
	fixture := sessionTerminalFixtureWith(t, sessionControlFunc(func(ctx context.Context, request SessionControlRequest) (SessionControlReply, error) {
		if request.Session != "original" || request.SessionID != zellijlive.SessionID("boot", "original", 1, 1) {
			return SessionControlReply{}, errors.New("session identity changed")
		}
		reply, err := control.Exchange(ctx, request)
		if reply.Grant != nil && reply.Grant.Attempt != lastGrant {
			lastGrant = reply.Grant.Attempt
			grants <- reply.Grant.Attempt
		}
		if request.Event == "ready" {
			ready <- request.Attempt
		}
		return reply, err
	}))
	receive := func(channel <-chan string) string {
		t.Helper()
		select {
		case attempt := <-channel:
			return attempt
		case <-time.After(5 * time.Second):
			t.Fatal("missing attachment event")
			return ""
		}
	}
	first := receive(grants)
	fixture.mark(t, first, ".ready")
	if receive(ready) != first {
		t.Fatal("wrong first readiness")
	}
	fixture.mark(t, first, ".exit")
	fixture.waitText(t, "Connection lost")
	fixture.write(t, "offline-typing"+pasteStart+"offline-paste\n"+pasteEnd)
	select {
	case <-grants:
		t.Fatal("automatic reconnect without Enter")
	case <-time.After(600 * time.Millisecond):
	}
	fixture.write(t, "\r")
	next := receive(grants)
	if next == first {
		t.Fatal("attempt reused")
	}
	awaitSession(t, func() bool { _, err := os.Stat(filepath.Join(fixture.root, next+".pid")); return err == nil })
	fixture.mark(t, next, ".ready")
	if receive(ready) != next {
		t.Fatal("wrong retry readiness")
	}
	fixture.write(t, "fresh\n")
	awaitSession(t, func() bool { return strings.Contains(fixture.received(next), "fresh\n") })
	if got := fixture.received(next); got != "fresh\n" {
		t.Fatalf("replayed input: %q", got)
	}
}
