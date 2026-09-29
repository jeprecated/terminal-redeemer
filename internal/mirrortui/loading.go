package mirrortui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jmo/terminal-redeemer/internal/mirror"
)

// ErrNoSessions reports a successful discovery with nothing to open.
var ErrNoSessions = errors.New("no live Zellij sessions found")

type loadResult struct {
	model tea.Model
	err   error
}

type spinnerTick struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 100 * time.Millisecond

// LoadingModel paints on the first frame and runs discovery as a command, so a
// slow source (SSH, remote snapshot) never leaves the terminal blank. Once the
// load succeeds every message belongs to the loaded picker.
type LoadingModel struct {
	title, subject string
	load           func() (tea.Model, error)
	inner          tea.Model
	err            error
	frame          int
	width, height  int
	sized          bool
	color          bool
	cancelled      bool
	done           bool
}

// NewLoadingModel names what is being loaded ("sessions from lattice") and
// builds the real picker from load's result.
func NewLoadingModel(title, subject string, color bool, load func() (tea.Model, error)) *LoadingModel {
	return &LoadingModel{title: title, subject: subject, load: load, width: 80, height: 24, color: color}
}

func (m *LoadingModel) Init() tea.Cmd {
	load := m.load
	return tea.Batch(func() tea.Msg {
		model, err := load()
		return loadResult{model, err}
	}, spinnerCmd())
}

func spinnerCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTick{} })
}

func (m *LoadingModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.inner != nil {
		if _, ok := message.(spinnerTick); ok {
			return m, nil
		}
		next, cmd := m.inner.Update(message)
		m.inner = next
		return m, cmd
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.sized = msg.Width, msg.Height, true
	case loadResult:
		if m.done {
			return m, nil
		}
		if msg.err != nil || msg.model == nil {
			m.err = msg.err
			if m.err == nil {
				m.err = errors.New("loader returned no picker")
			}
			return m, nil
		}
		m.inner = msg.model
		cmds := []tea.Cmd{m.inner.Init()}
		if m.sized {
			next, cmd := m.inner.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			m.inner = next
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	case spinnerTick:
		if m.err == nil && !m.done {
			m.frame = (m.frame + 1) % len(spinnerFrames)
			return m, spinnerCmd()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = m.err == nil
			m.done = true
			return m, tea.Quit
		case "enter":
			if m.err != nil {
				m.done = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// Loaded returns the picker once discovery succeeded.
func (m *LoadingModel) Loaded() tea.Model { return m.inner }

// Err is the discovery failure shown to the user, if any.
func (m *LoadingModel) Err() error { return m.err }

// Cancelled reports a quit before discovery finished.
func (m *LoadingModel) Cancelled() bool { return m.cancelled }

func (m *LoadingModel) View() string {
	if m.inner != nil {
		return m.inner.View()
	}
	if m.done {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	lines := []string{paintRole(m.color, fit(m.title, width), roleAccent), ""}
	if m.err != nil {
		lines = append(lines, paintRole(m.color, fit("Could not load "+m.subject+":", width), roleError))
		for _, line := range strings.Split(strings.TrimSpace(m.err.Error()), "\n") {
			lines = append(lines, fit("  "+strings.TrimRight(line, "\r"), width))
		}
		lines = append(lines, "", paintRole(m.color, fit("Enter/Esc/q close", width), roleDim))
	} else {
		lines = append(lines, fit(spinnerFrames[m.frame]+" Loading "+m.subject+"…", width), "", paintRole(m.color, fit("Esc/q cancel", width), roleDim))
	}
	if m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

func paintRole(color bool, value string, role semanticRole) string {
	if !color || value == "" {
		return value
	}
	return foregroundANSI(roleColor(role)) + value + "\x1b[0m"
}

// SessionLoader returns discovered windows; it owns snapshot acquisition.
type SessionLoader func(context.Context) ([]mirror.Window, error)

// RunLoading opens the session picker immediately with a loading state and
// fills it when load returns. A discovery failure (including no sessions) is
// shown in the picker until dismissed, then returned.
func RunLoading(host string, load SessionLoader) ([]mirror.Window, bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := newSessionLoadingModel(ctx, host, load, colorEnabled())
	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, false, err
	}
	return sessionLoadingResult(final)
}

func newSessionLoadingModel(ctx context.Context, host string, load SessionLoader, color bool) *LoadingModel {
	subject := "sessions"
	if host = strings.TrimSpace(host); host != "" {
		subject += " from " + host
	}
	return NewLoadingModel("Mirror sessions", subject, color, func() (tea.Model, error) {
		windows, err := load(ctx)
		if err != nil {
			return nil, err
		}
		if len(windows) == 0 {
			if host != "" {
				return nil, fmt.Errorf("%w on %s", ErrNoSessions, host)
			}
			return nil, ErrNoSessions
		}
		return NewModel(windows, color), nil
	})
}

func sessionLoadingResult(final tea.Model) ([]mirror.Window, bool, error) {
	loading, ok := final.(*LoadingModel)
	if !ok {
		return nil, false, errors.New("mirror picker returned an unexpected model")
	}
	if loading.Cancelled() {
		return nil, true, nil
	}
	if err := loading.Err(); err != nil {
		return nil, false, err
	}
	model, ok := loading.Loaded().(*Model)
	if !ok {
		return nil, false, errors.New("mirror picker returned an unexpected model")
	}
	if model.Cancelled() {
		return nil, true, nil
	}
	return model.Selection(), false, nil
}

// WorkspaceLoader returns selectable source workspaces.
type WorkspaceLoader func(context.Context) ([]mirror.WorkspaceChoice, error)

// RunWorkspaceLoading is RunWorkspaceContext with discovery inside the picker.
func RunWorkspaceLoading(ctx context.Context, host string, load WorkspaceLoader) (mirror.WorkspaceChoice, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	subject := "workspaces"
	if host = strings.TrimSpace(host); host != "" {
		subject += " from " + host
	}
	color := colorEnabled()
	model := NewLoadingModel("Mirror workspace follow", subject, color, func() (tea.Model, error) {
		choices, err := load(ctx)
		if err != nil {
			return nil, err
		}
		if len(choices) == 0 {
			return nil, errors.New("source has no selectable workspaces")
		}
		return NewWorkspaceModel(choices, color), nil
	})
	final, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return mirror.WorkspaceChoice{}, false, err
	}
	return workspaceLoadingResult(final)
}

func workspaceLoadingResult(final tea.Model) (mirror.WorkspaceChoice, bool, error) {
	loading, ok := final.(*LoadingModel)
	if !ok {
		return mirror.WorkspaceChoice{}, false, errors.New("workspace picker returned an unexpected model")
	}
	if loading.Cancelled() {
		return mirror.WorkspaceChoice{}, true, nil
	}
	if err := loading.Err(); err != nil {
		return mirror.WorkspaceChoice{}, false, err
	}
	model, ok := loading.Loaded().(*WorkspaceModel)
	if !ok {
		return mirror.WorkspaceChoice{}, false, errors.New("workspace picker returned an unexpected model")
	}
	if model.Cancelled() {
		return mirror.WorkspaceChoice{}, true, nil
	}
	choice, ok := model.Selection()
	if !ok {
		return mirror.WorkspaceChoice{}, false, errors.New("workspace picker returned no selection")
	}
	return choice, false, nil
}
