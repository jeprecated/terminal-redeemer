package niri

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommandSnapshotterDefaultUsesDirectArgv(t *testing.T) {
	t.Parallel()

	runner := &stubRunner{responses: map[string]stubResult{
		"niri\x00msg\x00-j\x00windows":    {out: []byte(`[{"id":1}]`)},
		"niri\x00msg\x00-j\x00workspaces": {out: []byte(`[{"id":2,"idx":1,"name":null}]`)},
	}}
	s := CommandSnapshotter{Command: DefaultSnapshotCommand, Runner: runner}

	got, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if _, ok := payload["workspaces"]; !ok {
		t.Fatalf("expected combined payload to contain workspaces, got %q", got)
	}
	if _, ok := payload["windows"]; !ok {
		t.Fatalf("expected combined payload to contain windows, got %q", got)
	}
	wantCalls := [][]string{{"niri", "msg", "-j", "windows"}, {"niri", "msg", "-j", "workspaces"}}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("default query calls = %#v, want direct argv %#v", runner.calls, wantCalls)
	}
}

func TestCommandSnapshotterCustomUsesNonLoginShellCompatibility(t *testing.T) {
	t.Parallel()

	runner := &stubRunner{responses: map[string]stubResult{
		"sh\x00-c\x00 custom-niri --combined ": {out: []byte(`{"workspaces":[],"windows":[]}`)},
	}}
	got, err := (CommandSnapshotter{Command: " custom-niri --combined ", Runner: runner}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"workspaces":[],"windows":[]}` {
		t.Fatalf("custom output = %q", got)
	}
	want := [][]string{{"sh", "-c", " custom-niri --combined "}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("custom query calls = %#v, want %#v", runner.calls, want)
	}
}

func TestCommandSnapshotterError(t *testing.T) {
	t.Parallel()

	s := CommandSnapshotter{Command: DefaultSnapshotCommand, Runner: &stubRunner{out: []byte("socket refused"), err: errors.New("boom")}}
	if _, err := s.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "socket refused") {
		t.Fatalf("expected actionable command output, got %v", err)
	}
}

func TestCommandSnapshotterRejectsIncompleteDefaultSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		workspaces stubResult
	}{
		{name: "workspace command fails", workspaces: stubResult{err: errors.New("nope")}},
		{name: "workspace JSON is invalid", workspaces: stubResult{out: []byte(`not-json`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &stubRunner{responses: map[string]stubResult{
				"niri\x00msg\x00-j\x00windows":    {out: []byte(`[{"id":1}]`)},
				"niri\x00msg\x00-j\x00workspaces": tt.workspaces,
			}}
			s := CommandSnapshotter{Command: DefaultSnapshotCommand, Runner: runner}

			if _, err := s.Snapshot(context.Background()); err == nil {
				t.Fatal("expected incomplete snapshot to fail")
			}
		})
	}
}

type stubRunner struct {
	out       []byte
	err       error
	responses map[string]stubResult
	calls     [][]string
}

type stubResult struct {
	out []byte
	err error
}

func (s *stubRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	s.calls = append(s.calls, call)
	if s.responses != nil {
		key := strings.Join(call, "\x00")
		if result, ok := s.responses[key]; ok {
			return result.out, result.err
		}
		return nil, errors.New("missing stub response")
	}
	return s.out, s.err
}
