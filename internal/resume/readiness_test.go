package resume

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type readinessSequence struct {
	mu        sync.Mutex
	responses []readinessResponse
	calls     int
}

type readinessResponse struct {
	raw []byte
	err error
}

func (s *readinessSequence) Snapshot(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	index := s.calls - 1
	if index >= len(s.responses) {
		index = len(s.responses) - 1
	}
	response := s.responses[index]
	return response.raw, response.err
}

func TestWaitForNiriDelayedReadyReturnsSuccessfulSnapshot(t *testing.T) {
	t.Parallel()

	source := &readinessSequence{responses: []readinessResponse{
		{err: errors.New("socket absent")},
		{raw: []byte(`not-json`)},
		{raw: []byte(`{"workspaces":[],"windows":[]}`)},
	}}
	raw, err := WaitForNiri(context.Background(), source, 100*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if string(raw) != `{"workspaces":[],"windows":[]}` {
		t.Fatalf("successful payload = %q", raw)
	}
	if source.calls != 3 {
		t.Fatalf("calls = %d, want 3", source.calls)
	}
}

func TestWaitForNiriTimeoutIsBoundedAndActionable(t *testing.T) {
	t.Parallel()

	source := &readinessSequence{responses: []readinessResponse{{err: errors.New("connection refused")}}}
	started := time.Now()
	_, err := WaitForNiri(context.Background(), source, 15*time.Millisecond, 2*time.Millisecond)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "Niri IPC was not ready") || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("readiness wait exceeded bound: %s", elapsed)
	}
	if source.calls < 2 {
		t.Fatalf("expected polling, calls=%d", source.calls)
	}
}
