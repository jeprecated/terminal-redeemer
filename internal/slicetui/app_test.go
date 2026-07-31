package slicetui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

type fakeCall struct {
	verb    slicecontroller.ControlVerb
	payload any
}

type fakeClient struct {
	state    slicecontroller.State
	response *slicecontroller.ControlResponse
	err      error
	block    bool
	calls    []fakeCall
}

func (f *fakeClient) Call(ctx context.Context, verb slicecontroller.ControlVerb, payload any) (slicecontroller.ControlResponse, error) {
	f.calls = append(f.calls, fakeCall{verb: verb, payload: payload})
	if f.block {
		<-ctx.Done()
		return slicecontroller.ControlResponse{}, ctx.Err()
	}
	if f.err != nil {
		return slicecontroller.ControlResponse{}, f.err
	}
	if f.response != nil {
		return *f.response, nil
	}
	return okResponse(f.state), nil
}

func okResponse(state slicecontroller.State) slicecontroller.ControlResponse {
	return slicecontroller.ControlResponse{Outcome: slicecontroller.ControlOutcome{Status: "ok"}, State: &state}
}

func appState() slicecontroller.State {
	return slicecontroller.State{
		Namespace:          slicecontroller.Namespace{Host: "lattice", Leech: "overton"},
		ObservationQuality: sliceprotocol.QualityComplete,
		SelectedWorkspaces: map[string]string{"work": "Work"},
		Pickups:            map[string]bool{}, ClosedByUser: map[string]slicecontroller.SessionDrop{},
		Sources: map[string]slicecontroller.TrackedSource{
			"src-a": {SourceID: "src-a", SessionID: "sess-a", SessionName: "alpha", WorkspaceKey: "work", Lifecycle: slicecontroller.SourceEligible, Connection: slicecontroller.ConnectionDisconnected},
			"src-b": {SourceID: "src-b", SessionID: "sess-b", SessionName: "beta", WorkspaceKey: "work", Lifecycle: slicecontroller.SourceEligible, Connection: slicecontroller.ConnectionConnected},
		},
		Projections: map[string]slicecontroller.Projection{}, Spatial: map[string]slicecontroller.SpatialRecord{},
		Inventory: &sliceprotocol.Authoritative{Sources: []sliceprotocol.Source{
			{SourceID: "src-a", Session: sliceprotocol.Session{ID: "sess-a", Name: "alpha"}, Workspace: sliceprotocol.Workspace{Name: "Work", Key: "work"}},
			{SourceID: "src-b", Session: sliceprotocol.Session{ID: "sess-b", Name: "beta"}, Workspace: sliceprotocol.Workspace{Name: "Work", Key: "work"}},
		}},
	}
}

func TestAppLoadsPollsAndKeepsCursorByIdentity(t *testing.T) {
	state := appState()
	client := &fakeClient{state: state}
	app := NewApp(client, time.Millisecond, time.Second)
	msg := app.Init()()
	model, tick := app.Update(msg)
	app = model.(*App)
	if !app.loaded || len(client.calls) != 1 || client.calls[0].verb != slicecontroller.VerbStatus || tick == nil {
		t.Fatalf("initial load app=%+v calls=%+v", app, client.calls)
	}

	for i := range app.rows {
		if app.rows[i].ID == "source:src-b" {
			app.cursor = i
		}
	}
	state.Sources["src-0"] = slicecontroller.TrackedSource{SourceID: "src-0", SessionID: "sess-0", SessionName: "aardvark", WorkspaceKey: "work", Lifecycle: slicecontroller.SourceEligible}
	state.Inventory.Sources = append(state.Inventory.Sources, sliceprotocol.Source{SourceID: "src-0", Session: sliceprotocol.Session{ID: "sess-0", Name: "aardvark"}, Workspace: sliceprotocol.Workspace{Name: "Work", Key: "work"}})
	app.setState(state)
	if app.rows[app.cursor].ID != "source:src-b" {
		t.Fatalf("cursor moved to %q", app.rows[app.cursor].ID)
	}

	model, refresh := app.Update(tickMsg{})
	app = model.(*App)
	if refresh == nil || !app.busy {
		t.Fatal("poll tick did not start bounded refresh")
	}
	_ = refresh()
}

func TestViewportClipsAndKeepsCursorVisibleAcrossResize(t *testing.T) {
	state := appState()
	state.Sources = map[string]slicecontroller.TrackedSource{}
	state.Inventory.Sources = nil
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("src-%02d", i)
		session := fmt.Sprintf("session-%02d", i)
		state.Sources[id] = slicecontroller.TrackedSource{SourceID: id, SessionID: session, SessionName: session, WorkspaceKey: "work", Lifecycle: slicecontroller.SourceEligible}
		state.Inventory.Sources = append(state.Inventory.Sources, sliceprotocol.Source{SourceID: id, Session: sliceprotocol.Session{ID: session, Name: session}, Workspace: sliceprotocol.Workspace{Name: "Work", Key: "work"}})
	}
	app := NewApp(&fakeClient{state: state}, time.Second, time.Second)
	app.loaded = true
	app.setState(state)
	app.cursor = rowIndex(app.rows, "source:src-11")
	model, _ := app.Update(tea.WindowSizeMsg{Width: 48, Height: 6})
	app = model.(*App)
	view := app.View()
	if strings.Count(view, "\n") > 6 || !strings.Contains(view, ">     [discoverable]") {
		t.Fatalf("small viewport lost cursor or overflowed:\n%s", view)
	}
	for _, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if ansi.StringWidth(line) > 48 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}

	model, _ = app.Update(tea.WindowSizeMsg{Width: 48, Height: 4})
	app = model.(*App)
	shrunk := app.View()
	if strings.Count(shrunk, "\n") > 4 || !strings.Contains(shrunk, ">     [discoverable]") {
		t.Fatalf("shrunk viewport lost cursor or overflowed:\n%s", shrunk)
	}
	model, _ = app.Update(tea.WindowSizeMsg{Width: 48, Height: 1})
	app = model.(*App)
	tiny := app.View()
	if strings.Count(tiny, "\n") > 1 || !strings.HasPrefix(tiny, ">     [discoverable]") {
		t.Fatalf("tiny viewport did not reserve the cursor row:\n%s", tiny)
	}
	model, _ = app.Update(tea.WindowSizeMsg{Width: 48, Height: 8})
	app = model.(*App)
	grown := app.View()
	if strings.Count(grown, "\n") <= strings.Count(shrunk, "\n") || strings.Count(grown, "\n") > 8 || !strings.Contains(grown, ">     [discoverable]") {
		t.Fatalf("grown viewport did not expand around cursor:\n%s", grown)
	}
}

func TestNarrowRenderingPreservesFactsBeforeTruncatingMaximumSessionNames(t *testing.T) {
	const width = 80
	longName := strings.Repeat("s", 255)
	state := appState()
	state.Sources = map[string]slicecontroller.TrackedSource{
		"src-long": {
			SourceID: "src-long", SessionID: "long-session", SessionName: longName, WorkspaceKey: "work",
			Lifecycle: slicecontroller.SourceConflict, Connection: slicecontroller.ConnectionConnected, Conflict: "projection_ambiguous",
		},
	}
	state.Inventory.Sources = []sliceprotocol.Source{{
		SourceID: "src-long", Session: sliceprotocol.Session{ID: "long-session", Name: longName}, Workspace: sliceprotocol.Workspace{Name: "Work", Key: "work"},
	}}
	state.Projections = map[string]slicecontroller.Projection{"src-long": {SourceID: "src-long", Status: slicecontroller.ProjectionOwned}}
	state.ClosedByUser = map[string]slicecontroller.SessionDrop{"orphan": {SessionID: "orphan", SessionName: longName}}

	app := NewApp(&fakeClient{state: state}, time.Second, time.Second)
	app.loaded = true
	app.setState(state)
	model, _ := app.Update(tea.WindowSizeMsg{Width: width, Height: 12})
	view := model.(*App).View()
	var orphanLine string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "[source absent]") {
			orphanLine = line
		}
		if ansi.StringWidth(line) > width {
			t.Fatalf("line exceeds %d display cells: %q", width, line)
		}
	}
	for _, fact := range []string{"[discoverable]", "[desired]", "[projection=owned]", "[connection=connected]", "[lifecycle=conflict]", "[conflict:tracked=projection_ambiguous]"} {
		if !strings.Contains(view, fact) {
			t.Fatalf("source fact %q was hidden by the maximum-length name:\n%s", fact, view)
		}
	}
	if !strings.Contains(view, "session ") || !strings.Contains(view, "…") || strings.Contains(view, longName) {
		t.Fatalf("source name was not truncated after its facts:\n%s", view)
	}
	for _, fact := range []string{"[closed]", "[source absent]"} {
		if !strings.Contains(orphanLine, fact) {
			t.Fatalf("orphan fact %q was hidden by the maximum-length name: %q", fact, orphanLine)
		}
	}
	if !strings.Contains(orphanLine, "…") || strings.Contains(orphanLine, longName) {
		t.Fatalf("orphan name was not truncated after its facts: %q", orphanLine)
	}
}

func TestWideUnicodeWorkspaceNameIsClippedByDisplayCells(t *testing.T) {
	const width = 24
	wideName := strings.Repeat("界", 40)
	state := appState()
	state.SelectedWorkspaces = map[string]string{"work": wideName}
	for i := range state.Inventory.Sources {
		state.Inventory.Sources[i].Workspace.Name = wideName
	}
	app := NewApp(&fakeClient{state: state}, time.Second, time.Second)
	app.loaded = true
	app.setState(state)
	model, _ := app.Update(tea.WindowSizeMsg{Width: width, Height: 8})
	view := model.(*App).View()
	var workspaceLine string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "workspace") {
			workspaceLine = line
		}
		if ansi.StringWidth(line) > width {
			t.Fatalf("line exceeds %d display cells: width=%d line=%q", width, ansi.StringWidth(line), line)
		}
	}
	if workspaceLine == "" || !strings.Contains(workspaceLine, "…") || strings.Contains(workspaceLine, wideName) {
		t.Fatalf("wide workspace name was not cell-aware truncated: %q", workspaceLine)
	}
	if strings.Count(view, "\n") > 8 {
		t.Fatalf("wide characters wrapped beyond viewport height:\n%s", view)
	}
}

func TestCursorIdentitySurvivesUnnamedWorkspaceNameCollision(t *testing.T) {
	state := appState()
	state.SelectedWorkspaces = map[string]string{"(unnamed)": "(unnamed)"}
	state.Sources = map[string]slicecontroller.TrackedSource{
		"named":     {SourceID: "named", SessionID: "named-session", SessionName: "named", WorkspaceKey: "(unnamed)", Lifecycle: slicecontroller.SourceEligible},
		"synthetic": {SourceID: "synthetic", SessionID: "synthetic-session", SessionName: "synthetic", WorkspaceKey: "", Lifecycle: slicecontroller.SourceEligible},
	}
	state.Inventory.Sources = []sliceprotocol.Source{
		{SourceID: "named", Session: sliceprotocol.Session{ID: "named-session", Name: "named"}, Workspace: sliceprotocol.Workspace{Name: "(unnamed)", Key: "(unnamed)"}},
		{SourceID: "synthetic", Session: sliceprotocol.Session{ID: "synthetic-session", Name: "synthetic"}, Workspace: sliceprotocol.Workspace{}},
	}
	app := NewApp(&fakeClient{state: state}, time.Second, time.Second)
	app.setState(state)
	app.cursor = rowIndex(app.rows, "group:unnamed-workspace")
	app.setState(state)
	if app.rows[app.cursor].ID != "group:unnamed-workspace" {
		t.Fatalf("cursor restored to colliding named row: %+v", app.rows[app.cursor])
	}
}

func TestAppActionsUseOnlyControlVerbs(t *testing.T) {
	state := appState()
	client := &fakeClient{state: state}
	app := NewApp(client, time.Second, time.Second)
	app.setState(state)

	run := func(key tea.KeyMsg, want slicecontroller.ControlVerb) {
		t.Helper()
		before := len(client.calls)
		model, cmd := app.Update(key)
		app = model.(*App)
		if cmd == nil {
			t.Fatalf("key %q produced no command", key.String())
		}
		model, _ = app.Update(cmd())
		app = model.(*App)
		if len(client.calls) != before+1 || client.calls[len(client.calls)-1].verb != want {
			t.Fatalf("key %q calls=%+v want=%s", key.String(), client.calls[before:], want)
		}
	}

	app.cursor = rowIndex(app.rows, "all")
	run(runeKey("a"), slicecontroller.VerbAllEnable)
	state.AllEligible = true
	app.setState(state)
	run(runeKey("a"), slicecontroller.VerbAllDisable)

	app.cursor = rowIndex(app.rows, "workspace:work")
	run(tea.KeyMsg{Type: tea.KeySpace}, slicecontroller.VerbWorkspaceRemove)
	state.SelectedWorkspaces = map[string]string{}
	app.setState(state)
	app.cursor = rowIndex(app.rows, "workspace:work")
	run(tea.KeyMsg{Type: tea.KeySpace}, slicecontroller.VerbWorkspaceAdd)
	state.SelectedWorkspaces = map[string]string{"work": "Work"}
	app.setState(state)

	app.cursor = rowIndex(app.rows, "source:src-a")
	run(runeKey("p"), slicecontroller.VerbPickup)
	state.Pickups["src-a"] = true
	app.setState(state)
	app.cursor = rowIndex(app.rows, "source:src-a")
	run(runeKey("p"), slicecontroller.VerbPickupRemove)
	run(runeKey("x"), slicecontroller.VerbClose)
	state.ClosedByUser["sess-a"] = slicecontroller.SessionDrop{SessionID: "sess-a", SessionName: "alpha", CreatedAt: time.Now()}
	app.setState(state)
	app.cursor = rowIndex(app.rows, "source:src-a")
	run(runeKey("o"), slicecontroller.VerbReopen)
	run(runeKey("r"), slicecontroller.VerbReconnect)
	run(runeKey("u"), slicecontroller.VerbUndo)

	before := len(client.calls)
	model, cmd := app.Update(runeKey("g"))
	app = model.(*App)
	if cmd == nil {
		t.Fatal("manual refresh produced no command")
	}
	model, _ = app.Update(cmd())
	app = model.(*App)
	if len(client.calls) != before+1 || client.calls[len(client.calls)-1].verb != slicecontroller.VerbStatus {
		t.Fatalf("refresh calls=%+v", client.calls[before:])
	}
}

func TestAppSurfacesErrorsResizeAndQuit(t *testing.T) {
	client := &fakeClient{err: errors.New("socket missing")}
	app := NewApp(client, time.Millisecond, 10*time.Millisecond)
	model, _ := app.Update(app.Init()())
	app = model.(*App)
	if !strings.Contains(app.View(), "controller unavailable: socket missing") {
		t.Fatalf("view=%q", app.View())
	}

	client.err = nil
	client.response = &slicecontroller.ControlResponse{Outcome: slicecontroller.ControlOutcome{Status: "error", Code: "response_too_large"}}
	model, _ = app.Update(app.statusCmd()())
	app = model.(*App)
	if !strings.Contains(app.View(), "controller error: response_too_large") {
		t.Fatalf("view=%q", app.View())
	}

	model, _ = app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = model.(*App)
	if app.width != 120 || app.height != 40 {
		t.Fatalf("size=%dx%d", app.width, app.height)
	}
	model, cmd := app.Update(runeKey("q"))
	app = model.(*App)
	if !app.quitting || cmd == nil {
		t.Fatal("quit did not terminate")
	}
}

func TestStatusRequestIsBounded(t *testing.T) {
	client := &fakeClient{block: true}
	app := NewApp(client, time.Second, 5*time.Millisecond)
	started := time.Now()
	msg := app.statusCmd()()
	if time.Since(started) > time.Second {
		t.Fatal("bounded request did not return promptly")
	}
	result := msg.(statusMsg)
	if !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", result.err)
	}
}

func rowIndex(rows []row, id string) int {
	for i := range rows {
		if rows[i].ID == id {
			return i
		}
	}
	return -1
}

func runeKey(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}
