package resume

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/model"
)

type fakeProcess struct {
	pid    int
	done   chan error
	kills  int
	killMu sync.Mutex
}

func newFakeProcess(pid int) *fakeProcess { return &fakeProcess{pid: pid, done: make(chan error, 1)} }
func (p *fakeProcess) PID() int           { return p.pid }
func (p *fakeProcess) Done() <-chan error { return p.done }
func (p *fakeProcess) Kill() error {
	p.killMu.Lock()
	defer p.killMu.Unlock()
	p.kills++
	return nil
}
func (p *fakeProcess) killCount() int {
	p.killMu.Lock()
	defer p.killMu.Unlock()
	return p.kills
}

type fakeDesktop struct {
	mu       sync.Mutex
	windows  []ObservedWindow
	attached map[int]string
	moves    []string
	moveErr  error
}

func (d *fakeDesktop) Windows(context.Context) ([]ObservedWindow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ObservedWindow(nil), d.windows...), nil
}
func (d *fakeDesktop) Attached(_ context.Context, pid int, session string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.attached[pid] == session, nil
}
func (d *fakeDesktop) MoveToWorkspace(_ context.Context, id int, target WorkspaceTarget) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.moves = append(d.moves, target.ID)
	if d.moveErr != nil {
		return d.moveErr
	}
	for i := range d.windows {
		if d.windows[i].ID == id {
			d.windows[i].WorkspaceID = target.ID
			return nil
		}
	}
	return errors.New("window missing")
}

type fakeLauncher struct {
	next      int
	desktop   *fakeDesktop
	specs     []LaunchSpec
	processes []*fakeProcess
	noWindow  bool
	noAttach  bool
	zeroPID   bool
	exitSet   bool
	exit      error
}

func (l *fakeLauncher) Start(_ context.Context, spec LaunchSpec) (Process, error) {
	l.specs = append(l.specs, spec)
	l.next++
	pid := 1000 + l.next
	if l.zeroPID {
		pid = 0
	}
	process := newFakeProcess(pid)
	l.processes = append(l.processes, process)
	if !l.noWindow && pid > 0 {
		l.desktop.mu.Lock()
		l.desktop.windows = append(l.desktop.windows, ObservedWindow{ID: 2000 + l.next, PID: pid, AppID: "kitty", WorkspaceID: "current"})
		if !l.noAttach {
			session := spec.Args[len(spec.Args)-1]
			l.desktop.attached[pid] = session
		}
		l.desktop.mu.Unlock()
	}
	if l.exitSet || l.exit != nil {
		process.done <- l.exit
	}
	return process, nil
}

type fakeLayout struct{ result LayoutResult }

func (l fakeLayout) ApplyLayout(context.Context, int, model.Placement) LayoutResult { return l.result }

func testExecutor(desktop *fakeDesktop, launcher *fakeLauncher) Executor {
	return Executor{
		Config:   ExecutorConfig{LauncherCommand: "/usr/bin/kitty", Timeout: 10 * time.Millisecond, PollInterval: time.Millisecond},
		Launcher: launcher,
		Observer: desktop,
		Probe:    desktop,
		Mover:    desktop,
	}
}

func readyItem(key, session, workspace string) Item {
	return Item{WindowKey: key, AppID: "kitty", Session: session, CWD: "/tmp/" + session, Status: StatusReady, Workspace: &WorkspaceTarget{ID: workspace, Name: workspace}}
}

func TestExecutorRestoresMultipleSessionsByExactPIDAndRerunIsIdempotent(t *testing.T) {
	t.Setenv("ZELLIJ", "0")
	t.Setenv("ZELLIJ_SESSION_NAME", "outer")
	desktop := &fakeDesktop{
		windows:  []ObservedWindow{{ID: 77, PID: 777, AppID: "kitty", WorkspaceID: "unrelated"}},
		attached: map[int]string{777: "unrelated-session"},
	}
	launcher := &fakeLauncher{desktop: desktop}
	executor := testExecutor(desktop, launcher)
	plan := Plan{Items: []Item{readyItem("a", "session-a", "ws-a"), readyItem("b", "session-b", "ws-b")}}

	got := executor.Apply(context.Background(), plan)
	if got.Items[0].Status != StatusRestored || got.Items[1].Status != StatusRestored || got.Summary.Restored != 2 {
		t.Fatalf("unexpected results: %#v", got)
	}
	if len(desktop.moves) != 2 || desktop.moves[0] != "ws-a" || desktop.moves[1] != "ws-b" {
		t.Fatalf("moves = %#v", desktop.moves)
	}
	if len(launcher.specs) != 2 {
		t.Fatalf("launch count = %d", len(launcher.specs))
	}
	for i, spec := range launcher.specs {
		wantSession := "session-" + string(rune('a'+i))
		want := []string{"--directory", "/tmp/" + wantSession, "zellij", "attach", wantSession}
		if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("launch argv = %#v, want %#v", spec.Args, want)
		}
		for _, env := range spec.Env {
			if strings.HasPrefix(env, "ZELLIJ=") || strings.HasPrefix(env, "ZELLIJ_SESSION_NAME=") {
				t.Fatalf("nested Zellij environment leaked: %q", env)
			}
		}
	}
	if desktop.windows[0].WorkspaceID != "unrelated" {
		t.Fatal("unrelated concurrent Kitty window was moved")
	}

	rerun := executor.Apply(context.Background(), plan)
	if rerun.Items[0].Status != StatusAlreadyOpen || rerun.Items[1].Status != StatusAlreadyOpen {
		t.Fatalf("rerun results = %#v", rerun.Items)
	}
	if len(launcher.specs) != 2 {
		t.Fatalf("rerun created extra windows: %d launches", len(launcher.specs))
	}
}

func TestKittyLaunchSpecKeepsSessionAndCWDAsArgv(t *testing.T) {
	spec := KittyLaunchSpec("kitty", Item{CWD: "/tmp/a b", Session: "name; touch /tmp/owned"})
	want := []string{"--directory", "/tmp/a b", "zellij", "attach", "name; touch /tmp/owned"}
	if !reflect.DeepEqual(spec.Args, want) {
		t.Fatalf("argv = %#v, want %#v", spec.Args, want)
	}
}

func TestExecutorCorrelationTimeoutKillsOnlyLaunchedProcess(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop, noWindow: true}
	got := testExecutor(desktop, launcher).Apply(context.Background(), Plan{Items: []Item{readyItem("a", "session", "ws")}})
	if got.Items[0].Status != StatusFailed || !strings.Contains(got.Items[0].Reason, "exact launched-PID correlation timed out") {
		t.Fatalf("result = %#v", got.Items[0])
	}
	if launcher.processes[0].killCount() != 1 {
		t.Fatalf("timeout cleanup kills = %d", launcher.processes[0].killCount())
	}
}

func TestExecutorAttachmentEvidenceTimeoutKillsLaunchedProcess(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop, noAttach: true}
	got := testExecutor(desktop, launcher).Apply(context.Background(), Plan{Items: []Item{readyItem("a", "session", "ws")}})
	if got.Items[0].Status != StatusFailed || !strings.Contains(got.Items[0].Reason, "attachment evidence timed out") {
		t.Fatalf("result = %#v", got.Items[0])
	}
	if launcher.processes[0].killCount() != 1 {
		t.Fatal("unverified attachment process was not cleaned up")
	}
}

func TestExecutorFailedAttachExitIsUnavailableAndCleanedUp(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop, noWindow: true, exitSet: true, exit: errors.New("exit status 1")}
	got := testExecutor(desktop, launcher).Apply(context.Background(), Plan{Items: []Item{readyItem("a", "missing", "ws")}})
	if got.Items[0].Status != StatusUnavailable || !strings.Contains(got.Items[0].Reason, "attach process exited") {
		t.Fatalf("result = %#v", got.Items[0])
	}
	if launcher.processes[0].killCount() != 1 {
		t.Fatalf("failed attach cleanup kills = %d", launcher.processes[0].killCount())
	}
}

func TestExecutorMoveFailureLeavesAttachedWindowForSafeRerun(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}, moveErr: errors.New("move denied")}
	launcher := &fakeLauncher{desktop: desktop}
	executor := testExecutor(desktop, launcher)
	plan := Plan{Items: []Item{readyItem("a", "session", "ws")}}
	got := executor.Apply(context.Background(), plan)
	if got.Items[0].Status != StatusFailed || !strings.Contains(got.Items[0].Reason, "left open") {
		t.Fatalf("result = %#v", got.Items[0])
	}
	if launcher.processes[0].killCount() != 0 {
		t.Fatal("successfully attached terminal was killed after required move failure")
	}
	rerun := executor.Apply(context.Background(), plan)
	if rerun.Items[0].Status != StatusAlreadyOpen || len(launcher.specs) != 1 {
		t.Fatalf("unsafe rerun: result=%#v launches=%d", rerun.Items[0], len(launcher.specs))
	}
}

func TestExecutorRejectsDaemonizingLauncherWithoutGuessing(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop, noWindow: true, exitSet: true}
	got := testExecutor(desktop, launcher).Apply(context.Background(), Plan{Items: []Item{readyItem("a", "session", "ws")}})
	if got.Items[0].Status != StatusFailed || !strings.Contains(got.Items[0].Reason, "daemonizing launchers are unsupported") {
		t.Fatalf("result = %#v", got.Items[0])
	}
}

func TestExecutorRejectsLauncherWithoutCorrelationPID(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop, zeroPID: true}
	got := testExecutor(desktop, launcher).Apply(context.Background(), Plan{Items: []Item{readyItem("a", "session", "ws")}})
	if got.Items[0].Status != StatusFailed || !strings.Contains(got.Items[0].Reason, "reliable client PID") {
		t.Fatalf("result = %#v", got.Items[0])
	}
	if launcher.processes[0].killCount() != 1 {
		t.Fatal("unsupported launcher process was not cleaned up")
	}
}

func TestExecutorOptionalLayoutFailureDoesNotFalsifyRequiredSuccess(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop}
	executor := testExecutor(desktop, launcher)
	executor.Layout = fakeLayout{result: LayoutResult{Status: LayoutDegraded, Reason: "column unsupported"}}
	item := readyItem("a", "session", "ws")
	item.CapturedPlacement = &model.Placement{Column: intPointer(2)}
	got := executor.Apply(context.Background(), Plan{Items: []Item{item}})
	if got.Items[0].Status != StatusRestored || got.Items[0].LayoutStatus != LayoutDegraded {
		t.Fatalf("result = %#v", got.Items[0])
	}
}

func TestExecutorSuppressesDuplicateReadyItemsWithinExecution(t *testing.T) {
	desktop := &fakeDesktop{attached: map[int]string{}}
	launcher := &fakeLauncher{desktop: desktop}
	plan := Plan{Items: []Item{readyItem("a", "same", "ws-a"), readyItem("b", "same", "ws-b")}}
	got := testExecutor(desktop, launcher).Apply(context.Background(), plan)
	if got.Items[0].Status != StatusRestored || got.Items[1].Status != StatusAlreadyOpen || len(launcher.specs) != 1 {
		t.Fatalf("result=%#v launches=%d", got.Items, len(launcher.specs))
	}
}

func intPointer(value int) *int { return &value }
