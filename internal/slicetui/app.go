package slicetui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
)

type statusMsg struct {
	response slicecontroller.ControlResponse
	err      error
}

type actionMsg struct {
	response slicecontroller.ControlResponse
	err      error
}

type tickMsg struct{}

type App struct {
	client          Client
	refreshInterval time.Duration
	requestTimeout  time.Duration
	state           slicecontroller.State
	rows            []row
	cursor          int
	viewportStart   int
	busy            bool
	tickerStarted   bool
	loaded          bool
	quitting        bool
	errText         string
	width           int
	height          int
}

func NewApp(client Client, refreshInterval, requestTimeout time.Duration) *App {
	return &App{client: client, refreshInterval: refreshInterval, requestTimeout: requestTimeout}
}

func Run(client Client, refreshInterval, requestTimeout time.Duration) error {
	if client == nil {
		return errors.New("slice management client is required")
	}
	if refreshInterval <= 0 || requestTimeout <= 0 {
		return errors.New("slice management refresh interval and timeout must be positive")
	}
	_, err := tea.NewProgram(NewApp(client, refreshInterval, requestTimeout)).Run()
	return err
}

func (a *App) Init() tea.Cmd {
	a.busy = true
	return a.statusCmd()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			a.quitting = true
			return a, tea.Quit
		case "up", "k":
			if a.cursor > 0 {
				a.cursor--
				a.ensureCursorVisible()
			}
		case "down", "j":
			if a.cursor < len(a.rows)-1 {
				a.cursor++
				a.ensureCursorVisible()
			}
		case "g":
			if !a.busy {
				a.busy = true
				return a, a.statusCmd()
			}
		case "u":
			return a.startAction(slicecontroller.VerbUndo, struct{}{})
		case "a":
			verb := slicecontroller.VerbAllEnable
			if a.state.AllEligible {
				verb = slicecontroller.VerbAllDisable
			}
			return a.startAction(verb, struct{}{})
		case "enter", " ":
			return a.startPrimaryAction()
		case "p":
			if current, ok := a.currentSource(); ok {
				verb := slicecontroller.VerbPickup
				if current.Pickup {
					verb = slicecontroller.VerbPickupRemove
				}
				return a.startAction(verb, sourcePayload(current))
			}
		case "x", "c":
			if current, ok := a.currentSource(); ok && !current.Closed {
				return a.startAction(slicecontroller.VerbClose, sourcePayload(current))
			}
		case "o":
			if current, ok := a.currentSource(); ok && current.Closed {
				return a.startAction(slicecontroller.VerbReopen, sourcePayload(current))
			}
		case "r":
			if current, ok := a.currentSource(); ok {
				return a.startAction(slicecontroller.VerbReconnect, sourcePayload(current))
			}
		}
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.ensureCursorVisible()
	case statusMsg:
		a.busy = false
		a.applyResponse(msg.response, msg.err)
		if !a.tickerStarted {
			a.tickerStarted = true
			return a, a.tickCmd()
		}
	case actionMsg:
		a.busy = false
		a.applyResponse(msg.response, msg.err)
	case tickMsg:
		next := a.tickCmd()
		if a.busy {
			return a, next
		}
		a.busy = true
		return a, tea.Batch(a.statusCmd(), next)
	}
	return a, nil
}

func (a *App) startPrimaryAction() (tea.Model, tea.Cmd) {
	if a.busy || a.cursor < 0 || a.cursor >= len(a.rows) {
		return a, nil
	}
	current := a.rows[a.cursor]
	switch current.Kind {
	case rowAll:
		verb := slicecontroller.VerbAllEnable
		if current.Selected {
			verb = slicecontroller.VerbAllDisable
		}
		return a.startAction(verb, struct{}{})
	case rowWorkspace:
		verb := slicecontroller.VerbWorkspaceAdd
		if current.Selected {
			verb = slicecontroller.VerbWorkspaceRemove
		}
		return a.startAction(verb, workspacePayload(current))
	case rowSource:
		verb := slicecontroller.VerbPickup
		if current.Pickup {
			verb = slicecontroller.VerbPickupRemove
		}
		return a.startAction(verb, sourcePayload(current))
	default:
		return a, nil
	}
}

func (a *App) startAction(verb slicecontroller.ControlVerb, payload any) (tea.Model, tea.Cmd) {
	if a.busy || a.client == nil {
		return a, nil
	}
	a.busy = true
	a.errText = ""
	return a, a.actionCmd(verb, payload)
}

func (a *App) currentSource() (row, bool) {
	if a.cursor < 0 || a.cursor >= len(a.rows) || a.rows[a.cursor].Kind != rowSource {
		return row{}, false
	}
	return a.rows[a.cursor], true
}

func (a *App) statusCmd() tea.Cmd {
	return func() tea.Msg {
		if a.client == nil {
			return statusMsg{err: errors.New("controller unavailable")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), a.requestTimeout)
		defer cancel()
		response, err := a.client.Call(ctx, slicecontroller.VerbStatus, struct{}{})
		return statusMsg{response: response, err: err}
	}
}

func (a *App) actionCmd(verb slicecontroller.ControlVerb, payload any) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), a.requestTimeout)
		defer cancel()
		response, err := a.client.Call(ctx, verb, payload)
		return actionMsg{response: response, err: err}
	}
}

func (a *App) tickCmd() tea.Cmd {
	return tea.Tick(a.refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (a *App) applyResponse(response slicecontroller.ControlResponse, err error) {
	defer a.ensureCursorVisible()
	if err != nil {
		a.errText = "controller unavailable: " + err.Error()
		return
	}
	if response.Outcome.Status != "ok" {
		code := response.Outcome.Code
		if code == "" {
			code = "unknown_error"
		}
		a.errText = "controller error: " + code
		return
	}
	if response.State == nil {
		a.errText = "controller returned no state"
		return
	}
	a.errText = ""
	a.loaded = true
	a.setState(*response.State)
}

func (a *App) setState(state slicecontroller.State) {
	selectedID := ""
	oldCursor := a.cursor
	if a.cursor >= 0 && a.cursor < len(a.rows) {
		selectedID = a.rows[a.cursor].ID
	}
	a.state = state
	a.rows = rowsForState(state)
	found := false
	if selectedID != "" {
		for i := range a.rows {
			if a.rows[i].ID == selectedID {
				a.cursor = i
				found = true
				break
			}
		}
	}
	if !found {
		if len(a.rows) == 0 {
			a.cursor = 0
		} else if oldCursor >= len(a.rows) {
			a.cursor = len(a.rows) - 1
		}
	}
	a.ensureCursorVisible()
}

func (a *App) View() string {
	if a.quitting {
		return ""
	}
	a.ensureCursorVisible()
	var b strings.Builder
	top, showHelp := a.viewportChrome()
	for _, line := range top {
		writeLine(&b, a.width, line)
	}
	remaining := a.viewportCapacity()
	for i := a.viewportStart; i < len(a.rows) && remaining > 0; i++ {
		lines := a.renderedRowLines(i)
		if len(lines) > remaining {
			lines = lines[:remaining]
		}
		for _, line := range lines {
			writeLine(&b, a.width, line)
		}
		remaining -= len(lines)
	}
	if showHelp {
		writeLine(&b, a.width, "Keys: ↑/k ↓/j  space toggle  a all  p pickup  x close  o reopen  r reconnect  u undo  g refresh  q quit")
	}
	return b.String()
}

func (a *App) viewportChrome() ([]string, bool) {
	top := []string{"Terminal Slice Manager", a.statusLine()}
	if a.errText != "" {
		top = append(top, "Error: "+a.errText)
	}
	if a.busy {
		top = append(top, "Refreshing…")
	}
	if a.loaded && countKind(a.rows, rowSource) == 0 {
		top = append(top, "No terminal sources are currently known.")
	}
	if a.height <= 0 {
		return top, true
	}
	budget := a.height
	if len(a.rows) > 0 {
		budget-- // Always reserve one line for the selected row.
	}
	if budget <= 0 {
		return nil, false
	}
	if len(top) > budget {
		return top[:budget], false
	}
	return top, len(top) < budget
}

func (a *App) statusLine() string {
	if !a.loaded {
		return "Waiting for controller state…"
	}
	quality := string(a.state.ObservationQuality)
	if quality == "" {
		quality = "not-observed"
	}
	if a.state.ObservationCode != "" {
		quality += " (" + a.state.ObservationCode + ")"
	}
	return fmt.Sprintf("Host: %s  Leech: %s  Observation: %s", a.state.Namespace.Host, a.state.Namespace.Leech, quality)
}

func (a *App) viewportCapacity() int {
	if a.height <= 0 {
		return len(a.rows)
	}
	top, showHelp := a.viewportChrome()
	capacity := a.height - len(top)
	if showHelp {
		capacity--
	}
	if capacity > 0 {
		return capacity
	}
	return 0
}

func (a *App) ensureCursorVisible() {
	if len(a.rows) == 0 {
		a.cursor, a.viewportStart = 0, 0
		return
	}
	if a.cursor < 0 {
		a.cursor = 0
	} else if a.cursor >= len(a.rows) {
		a.cursor = len(a.rows) - 1
	}
	capacity := a.viewportCapacity()
	if capacity <= 0 {
		a.viewportStart = a.cursor
		return
	}
	if a.cursor < a.viewportStart {
		a.viewportStart = a.cursor
	}
	for a.viewportStart < a.cursor && a.renderedRowsHeight(a.viewportStart, a.cursor) > capacity {
		a.viewportStart++
	}
	if a.viewportStart > a.cursor {
		a.viewportStart = a.cursor
	}
}

func (a *App) renderedRowsHeight(start, end int) int {
	height := 0
	for i := start; i <= end && i < len(a.rows); i++ {
		height += len(a.renderedRowLines(i))
	}
	return height
}

func (a *App) renderedRowLines(index int) []string {
	rowWidth := 0
	if a.width > 2 {
		rowWidth = a.width - 2
	}
	contents := a.rows[index].renderLines(rowWidth)
	lines := make([]string, len(contents))
	for i, content := range contents {
		prefix := "  "
		if index == a.cursor && i == 0 {
			prefix = "> "
		}
		lines[i] = prefix + content
	}
	return lines
}

func writeLine(builder *strings.Builder, width int, line string) {
	fmt.Fprintln(builder, clipLine(line, width))
}

func clipLine(line string, width int) string {
	return clipCells(line, width)
}

func countKind(rows []row, kind rowKind) int {
	count := 0
	for _, item := range rows {
		if item.Kind == kind {
			count++
		}
	}
	return count
}
