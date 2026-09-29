package mirrortui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jmo/terminal-redeemer/internal/mirror"
)

func finishLoad(m *LoadingModel) tea.Cmd {
	model, err := m.load()
	_, cmd := m.Update(loadResult{model, err})
	return cmd
}

func TestLoadingPickerPaintsBeforeDiscoveryAndThenPopulates(t *testing.T) {
	calls := 0
	m := newSessionLoadingModel(context.Background(), "lattice", func(context.Context) ([]mirror.Window, error) {
		calls++
		return pickerWindows(), nil
	}, false)
	view := m.View()
	if calls != 0 || !strings.Contains(view, "Loading sessions from lattice…") || !strings.Contains(view, "Esc/q cancel") {
		t.Fatalf("first frame waited on discovery or lacks loading state: calls=%d view=%q", calls, view)
	}
	if m.Init() == nil {
		t.Fatal("loading model must start discovery and spinner commands")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	finishLoad(m)
	picker, ok := m.Loaded().(*Model)
	if !ok || calls != 1 {
		t.Fatalf("loaded=%T calls=%d", m.Loaded(), calls)
	}
	if picker.width != 60 || picker.height != 12 {
		t.Fatalf("size before load was not forwarded: %dx%d", picker.width, picker.height)
	}
	if view := m.View(); !strings.Contains(view, "Mirror sessions") || !strings.Contains(view, "alpha") {
		t.Fatalf("loaded picker not shown: %q", view)
	}
	m.Update(key("down"))
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter in the loaded picker must quit")
	}
	selected, cancelled, err := sessionLoadingResult(m)
	if err != nil || cancelled || len(selected) != 1 || mirror.SessionName(selected[0]) != "gamma" {
		t.Fatalf("selected=%v cancelled=%t err=%v", selected, cancelled, err)
	}
}

func TestLoadingPickerShowsFailureUntilDismissed(t *testing.T) {
	failure := errors.New("acquire mirror snapshot from lattice: exit status 255\nssh: connect to host lattice: No route to host")
	m := newSessionLoadingModel(context.Background(), "lattice", func(context.Context) ([]mirror.Window, error) {
		return nil, failure
	}, false)
	if cmd := finishLoad(m); cmd != nil {
		t.Fatal("a failure must stay on screen instead of quitting")
	}
	view := m.View()
	for _, want := range []string{"Could not load sessions from lattice:", "exit status 255", "No route to host", "Enter/Esc/q close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("failure view lacks %q: %q", want, view)
		}
	}
	if _, cmd := m.Update(spinnerTick{}); cmd != nil {
		t.Fatal("spinner must stop after a failure")
	}
	if _, cmd := m.Update(key("enter")); cmd == nil {
		t.Fatal("enter must dismiss the failure")
	}
	_, cancelled, err := sessionLoadingResult(m)
	if cancelled || !errors.Is(err, failure) {
		t.Fatalf("cancelled=%t err=%v", cancelled, err)
	}
}

func TestLoadingPickerReportsNoSessionsInTUI(t *testing.T) {
	m := newSessionLoadingModel(context.Background(), "lattice", func(context.Context) ([]mirror.Window, error) {
		return nil, nil
	}, true)
	finishLoad(m)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "no live Zellij sessions found on lattice") {
		t.Fatalf("empty discovery not shown: %q", view)
	}
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q must close the empty result")
	}
	_, cancelled, err := sessionLoadingResult(m)
	if cancelled || !errors.Is(err, ErrNoSessions) {
		t.Fatalf("cancelled=%t err=%v", cancelled, err)
	}
}

func TestLoadingPickerCancelIgnoresLateDiscovery(t *testing.T) {
	for _, value := range []string{"esc", "q", "ctrl+c"} {
		m := newSessionLoadingModel(context.Background(), "", func(context.Context) ([]mirror.Window, error) {
			return pickerWindows(), nil
		}, false)
		if !strings.Contains(m.View(), "Loading sessions…") {
			t.Fatalf("hostless label: %q", m.View())
		}
		if _, cmd := m.Update(key(value)); cmd == nil {
			t.Fatalf("%s must quit while loading", value)
		}
		finishLoad(m)
		if m.Loaded() != nil || m.View() != "" {
			t.Fatalf("%s: late discovery replaced a cancelled picker", value)
		}
		if _, cancelled, err := sessionLoadingResult(m); !cancelled || err != nil {
			t.Fatalf("%s: cancelled=%t err=%v", value, cancelled, err)
		}
	}
}

func TestWorkspaceLoadingResultUsesLoadedPicker(t *testing.T) {
	choices := []mirror.WorkspaceChoice{{Workspace: mirror.Workspace{ID: "1", Index: 1, Name: "dev"}}}
	m := NewLoadingModel("Mirror workspace follow", "workspaces from lattice", false, func() (tea.Model, error) {
		return NewWorkspaceModel(choices, false), nil
	})
	if !strings.Contains(m.View(), "Loading workspaces from lattice…") {
		t.Fatalf("view=%q", m.View())
	}
	finishLoad(m)
	m.Update(key("enter"))
	choice, cancelled, err := workspaceLoadingResult(m)
	if err != nil || cancelled || choice.Workspace.Name != "dev" {
		t.Fatalf("choice=%#v cancelled=%t err=%v", choice, cancelled, err)
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The real program must render the loading frame while discovery is blocked.
func TestLoadingProgramRendersWhileDiscoveryBlocks(t *testing.T) {
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newSessionLoadingModel(ctx, "lattice", func(ctx context.Context) ([]mirror.Window, error) {
		select {
		case <-release:
			return pickerWindows(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, false)
	var out lockedBuffer
	program := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(&out), tea.WithoutSignalHandler())
	done := make(chan tea.Model, 1)
	go func() {
		final, _ := program.Run()
		done <- final
	}()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !strings.Contains(out.String(), want) {
			if time.Now().After(deadline) {
				program.Kill()
				t.Fatalf("never rendered %q: %q", want, out.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor("Loading sessions from lattice")
	close(release)
	waitFor("alpha")
	program.Send(key("enter"))
	select {
	case final := <-done:
		selected, cancelled, err := sessionLoadingResult(final)
		if err != nil || cancelled || len(selected) != 1 || mirror.SessionName(selected[0]) != "alpha" {
			t.Fatalf("selected=%v cancelled=%t err=%v", selected, cancelled, err)
		}
	case <-time.After(3 * time.Second):
		program.Kill()
		t.Fatal("program did not exit")
	}
}
