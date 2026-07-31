package slicecontroller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jmo/terminal-redeemer/internal/niriipc"
	"github.com/jmo/terminal-redeemer/internal/sliceattach"
	"github.com/jmo/terminal-redeemer/internal/sliceenv"
	"github.com/jmo/terminal-redeemer/internal/slicelayout"
)

func RelayAttachReadiness(reader io.Reader, writer io.Writer, token string, ready chan<- struct{}) error {
	if !safeTokenPattern.MatchString(token) {
		return errors.New("invalid readiness token")
	}
	marker := []byte(sliceattach.ReadyMarker(token))
	pending := make([]byte, 0, len(marker)*2)
	chunk := make([]byte, 4096)
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			pending = append(pending, chunk[:n]...)
			if index := bytes.Index(pending, marker); index >= 0 {
				if index > 0 {
					if _, err := writer.Write(pending[:index]); err != nil {
						return err
					}
				}
				close(ready)
				if tail := pending[index+len(marker):]; len(tail) > 0 {
					if _, err := writer.Write(tail); err != nil {
						return err
					}
				}
				_, err := io.Copy(writer, reader)
				return err
			}
			keep := len(marker) - 1
			if len(pending) > keep {
				flush := len(pending) - keep
				if _, err := writer.Write(pending[:flush]); err != nil {
					return err
				}
				pending = append(pending[:0], pending[flush:]...)
			}
		}
		if readErr != nil {
			if len(pending) > 0 {
				if _, err := writer.Write(pending); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

type LocalNiri interface {
	Snapshot(context.Context) (niriipc.State, error)
	Action(context.Context, any) error
}

type ProcessReader interface {
	Exe(int) (string, error)
	Cmdline(int) ([]string, error)
}
type ProcFS struct{}

func (ProcFS) Exe(pid int) (string, error) {
	return os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
}
func (ProcFS) Cmdline(pid int) ([]string, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSuffix(string(payload), "\x00"), "\x00")
	return parts, nil
}

// VerifyOwnedWindows requires persisted mapping, exact app ID, exact PID, Kitty
// configured executable identity, and exact full argv evidence. Titles are ignored.
func VerifyOwnedWindows(state State, niri niriipc.State, processes ProcessReader) []OwnedWindow {
	owned, _ := VerifyOwnedWindowsWithConflicts(state, niri, processes)
	return owned
}
func VerifyOwnedWindowsWithConflicts(state State, niri niriipc.State, processes ProcessReader) ([]OwnedWindow, map[string]string) {
	if processes == nil {
		processes = ProcFS{}
	}
	byApp := map[string]Projection{}
	for _, p := range state.Projections {
		byApp[p.AppID] = p
	}
	candidates := map[string][]OwnedWindow{}
	conflicts := map[string]string{}
	for _, window := range niri.Windows {
		p, ok := byApp[window.AppID]
		if !ok {
			continue
		}
		if window.ID == 0 || window.PID <= 0 {
			conflicts[p.SourceID] = "projection_process_evidence_incomplete"
			continue
		}
		if p.ExpectedPID > 0 && window.PID != p.ExpectedPID {
			conflicts[p.SourceID] = "projection_pid_mismatch"
			continue
		}
		exe, err := processes.Exe(window.PID)
		if err != nil {
			conflicts[p.SourceID] = "projection_process_evidence_incomplete"
			continue
		}
		argv, err := processes.Cmdline(window.PID)
		if err != nil {
			conflicts[p.SourceID] = "projection_process_evidence_incomplete"
			continue
		}
		if p.ExpectedKittyExecutable == "" || len(p.ExpectedKittyArgv) < 2 || filepath.Clean(exe) != filepath.Clean(p.ExpectedKittyExecutable) || !reflect.DeepEqual(argv, p.ExpectedKittyArgv) {
			conflicts[p.SourceID] = "projection_process_mismatch"
			continue
		}
		candidates[p.SourceID] = append(candidates[p.SourceID], OwnedWindow{SourceID: p.SourceID, WindowID: window.ID, PID: window.PID, AppID: window.AppID, Focused: window.IsFocused})
	}
	var out []OwnedWindow
	for sourceID, items := range candidates {
		if conflicts[sourceID] != "" {
			continue
		}
		if len(items) == 1 {
			out = append(out, items[0])
		} else {
			conflicts[sourceID] = "projection_ownership_ambiguous"
		}
	}
	return out, conflicts
}

type ProjectionCommand struct {
	KittyCommand string
	KittyArgs    []string
	Environment  []string
}
type ProjectionCommandConfig struct {
	KittyCommand, SelfCommand, TransportCommand, SourceHost, ControlSocket, RemoteSelfCommand string
	TransportOptions                                                                          []string
	GraphicalContext                                                                          map[string]string
}

func BuildProjectionCommand(cfg ProjectionCommandConfig, source TrackedSource, p Projection) (ProjectionCommand, error) {
	if err := sliceenv.ValidateContext(cfg.GraphicalContext); err != nil {
		return ProjectionCommand{}, err
	}
	for _, v := range []string{cfg.KittyCommand, cfg.SelfCommand, cfg.TransportCommand, cfg.SourceHost, cfg.ControlSocket, cfg.RemoteSelfCommand, source.SourceID, source.SessionName, p.AppID, p.AttachToken} {
		if v == "" || len(v) > MaxProjectionArgvEntryBytes || strings.ContainsAny(v, "\x00\r\n") {
			return ProjectionCommand{}, errors.New("invalid projection command value")
		}
	}
	if !safeName(source.SourceID) || !safeName(source.SessionName) || !safeName(p.AttachToken) || p.ProcessSourceID != source.SourceID || p.ExpectedSessionName != source.SessionName {
		return ProjectionCommand{}, errors.New("unsafe or changed projection identity")
	}
	if err := ValidateProjectionTransportOptions(cfg.TransportOptions); err != nil {
		return ProjectionCommand{}, err
	}
	args := []string{"--config", "NONE", "--class", p.AppID, "--override", "confirm_os_window_close=0", "--title", "terminal-redeemer-slice", "-e", cfg.SelfCommand, "slice", "projection-run", "--source-id", source.SourceID, "--session", source.SessionName, "--token", p.AttachToken, "--control-socket", cfg.ControlSocket, "--transport-command", cfg.TransportCommand, "--host", cfg.SourceHost, "--remote-self-command", cfg.RemoteSelfCommand}
	for _, option := range cfg.TransportOptions {
		args = append(args, "--transport-option", option)
	}
	if err := ValidateProjectionArgv(append([]string{cfg.KittyCommand}, args...)); err != nil {
		return ProjectionCommand{}, err
	}
	keys := []string{"NIRI_SOCKET", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR"}
	env := make([]string, 0, 3)
	for _, key := range keys {
		env = append(env, key+"="+cfg.GraphicalContext[key])
	}
	return ProjectionCommand{KittyCommand: cfg.KittyCommand, KittyArgs: args, Environment: env}, nil
}

func ResolveProjectionCommand(plan ProjectionCommand) (string, []string, error) {
	resolved, err := exec.LookPath(plan.KittyCommand)
	if err != nil {
		return "", nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", nil, err
	}
	return filepath.Clean(resolved), append([]string{plan.KittyCommand}, plan.KittyArgs...), nil
}
func StartProjectionCommand(ctx context.Context, plan ProjectionCommand) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	cmd := exec.Command(plan.KittyCommand, plan.KittyArgs...)
	cmd.Env = plan.Environment
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

type CommandLauncher struct {
	Build func(TrackedSource, Projection) (ProjectionCommand, error)
	Start func(context.Context, ProjectionCommand) (int, error)
}

func (l CommandLauncher) Launch(ctx context.Context, source TrackedSource, p Projection) (int, error) {
	if l.Build == nil {
		return 0, errors.New("projection command builder unavailable")
	}
	plan, err := l.Build(source, p)
	if err != nil {
		return 0, err
	}
	if l.Start != nil {
		return l.Start(ctx, plan)
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	cmd := exec.Command(plan.KittyCommand, plan.KittyArgs...)
	cmd.Env = plan.Environment
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

func CloseOwnedWindow(ctx context.Context, client LocalNiri, windowID uint64) error {
	if windowID == 0 {
		return errors.New("owned window id is required")
	}
	return client.Action(ctx, map[string]any{"CloseWindow": niriipc.WindowIDAction{ID: windowID}})
}
func CloseOwnedWindowVerified(ctx context.Context, client LocalNiri, windowID uint64, poll time.Duration) error {
	if err := CloseOwnedWindow(ctx, client, windowID); err != nil {
		return err
	}
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, err := client.Snapshot(ctx)
			if err != nil {
				continue
			}
			found := false
			for _, window := range state.Windows {
				if window.ID == windowID {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}
	}
}

func FocusedCloseFallback(ctx context.Context, store *Store, cfg ControllerConfig, client LocalNiri, processes ProcessReader, initial OwnedWindow, poll time.Duration) error {
	lock, err := store.Acquire()
	if err != nil {
		return err
	}
	defer lock.Close()
	state, err := store.Read()
	if err != nil {
		return err
	}
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	owned, conflicts := VerifyOwnedWindowsWithConflicts(state, snapshot, processes)
	var exact *OwnedWindow
	for i := range owned {
		candidate := owned[i]
		if candidate.SourceID == initial.SourceID && candidate.WindowID == initial.WindowID && candidate.PID == initial.PID && candidate.AppID == initial.AppID && candidate.Focused {
			copy := candidate
			exact = &copy
			break
		}
	}
	if exact == nil {
		if code := conflicts[initial.SourceID]; code != "" {
			return fmt.Errorf("focused ownership reproof failed: %s", code)
		}
		return errors.New("focused projection changed before fallback lock")
	}
	engine := &Engine{Store: store, Config: cfg}
	_, effects, rollbackToken, err := engine.CloseFocused(initial.SourceID)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		if rollbackToken == nil {
			return cause
		}
		if _, rollbackErr := engine.RollbackFocusedClose(rollbackToken); rollbackErr != nil {
			return errors.Join(cause, fmt.Errorf("rollback failed focused close: %w", rollbackErr))
		}
		return cause
	}
	for _, effect := range effects {
		if effect.Kind == EffectCloseProjection && effect.FocusRequired && effect.SourceID == initial.SourceID && effect.WindowID == initial.WindowID {
			committed, readErr := store.Read()
			if readErr != nil {
				return rollback(readErr)
			}
			fresh, snapshotErr := client.Snapshot(ctx)
			if snapshotErr != nil {
				return rollback(snapshotErr)
			}
			reproved, _ := VerifyOwnedWindowsWithConflicts(committed, fresh, processes)
			matched := false
			for _, candidate := range reproved {
				if candidate.SourceID == initial.SourceID && candidate.WindowID == initial.WindowID && candidate.PID == initial.PID && candidate.AppID == initial.AppID && candidate.Focused {
					matched = true
					break
				}
			}
			if !matched {
				return rollback(errors.New("focused projection changed after close commit; close effect deferred"))
			}
			if closeErr := CloseOwnedWindowVerified(ctx, client, initial.WindowID, poll); closeErr != nil {
				return rollback(closeErr)
			}
			return nil
		}
	}
	return rollback(errors.New("focused close intent did not produce exact owned close"))
}

func ReproveLeechSpatial(ctx context.Context, store *Store, client LocalNiri, processes ProcessReader, proposal slicelayout.Proposal, currentEpoch string) error {
	state, err := store.Read()
	if err != nil {
		return err
	}
	mapping, ok := state.Projections[proposal.SourceID]
	if !ok || mapping.Status != ProjectionOwned || mapping.NiriWindowID != proposal.RuntimeWindowID || mapping.LeechCompositorEpoch != currentEpoch || proposal.TargetCompositorEpoch != currentEpoch {
		return errors.New("leech spatial ownership mapping changed")
	}
	record := state.Spatial[proposal.SourceID]
	if record.LastApplied == nil || record.LastApplied.Target != proposal.Target || record.LastApplied.Origin.ControllerID != proposal.Origin.ControllerID || record.LastApplied.Origin.Generation != proposal.Origin.Generation {
		return errors.New("leech spatial origin is no longer current")
	}
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	owned, conflicts := VerifyOwnedWindowsWithConflicts(state, snapshot, processes)
	for _, window := range owned {
		if window.SourceID == proposal.SourceID && window.WindowID == proposal.RuntimeWindowID && window.PID == mapping.ExpectedPID && window.AppID == mapping.AppID {
			return nil
		}
	}
	if code := conflicts[proposal.SourceID]; code != "" {
		return fmt.Errorf("leech spatial ownership reproof failed: %s", code)
	}
	return errors.New("leech spatial target is no longer positively owned")
}
func ExecuteLeechSpatial(ctx context.Context, store *Store, client LocalNiri, processes ProcessReader, proposal slicelayout.Proposal, currentEpoch string, poll time.Duration) error {
	return applyLocalProposal(ctx, client, proposal, poll, func(snapshot niriipc.State) error {
		return reproveLeechSpatialSnapshot(store, processes, proposal, currentEpoch, snapshot)
	})
}
func reproveLeechSpatialSnapshot(store *Store, processes ProcessReader, proposal slicelayout.Proposal, currentEpoch string, snapshot niriipc.State) error {
	state, err := store.Read()
	if err != nil {
		return err
	}
	mapping, ok := state.Projections[proposal.SourceID]
	if !ok || mapping.Status != ProjectionOwned || mapping.NiriWindowID != proposal.RuntimeWindowID || mapping.LeechCompositorEpoch != currentEpoch || proposal.TargetCompositorEpoch != currentEpoch {
		return errors.New("leech spatial ownership mapping changed")
	}
	record := state.Spatial[proposal.SourceID]
	if record.LastApplied == nil || record.LastApplied.Target != proposal.Target || record.LastApplied.Origin.ControllerID != proposal.Origin.ControllerID || record.LastApplied.Origin.Generation != proposal.Origin.Generation {
		return errors.New("leech spatial origin is no longer current")
	}
	owned, conflicts := VerifyOwnedWindowsWithConflicts(state, snapshot, processes)
	for _, window := range owned {
		if window.SourceID == proposal.SourceID && window.WindowID == proposal.RuntimeWindowID && window.PID == mapping.ExpectedPID && window.AppID == mapping.AppID {
			return nil
		}
	}
	if code := conflicts[proposal.SourceID]; code != "" {
		return fmt.Errorf("leech spatial ownership reproof failed: %s", code)
	}
	return errors.New("leech spatial target is no longer positively owned")
}

// ApplyLocalProposal translates only the exact leech-side non-focus proposal.
// Every action is followed by a complete snapshot verification.
func ApplyLocalProposal(ctx context.Context, client LocalNiri, proposal slicelayout.Proposal, poll time.Duration) error {
	return applyLocalProposal(ctx, client, proposal, poll, nil)
}
func applyLocalProposal(ctx context.Context, client LocalNiri, proposal slicelayout.Proposal, poll time.Duration, validate func(niriipc.State) error) error {
	if err := proposal.ValidateNonDisruptive(); err != nil {
		return err
	}
	if proposal.Target != slicelayout.Leech {
		return errors.New("local executor only accepts leech targets")
	}
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	before, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	focusedBefore := uint64(0)
	for _, w := range before.Windows {
		if w.IsFocused {
			focusedBefore = w.ID
		}
	}
	for _, change := range proposal.Changes {
		if change.Kind == slicelayout.ChangeEnsureWorkspace || change.Kind == slicelayout.ChangeInitialProjection {
			return errors.New("workspace ensure/initial projection requires controller sequencing")
		}
		var action any
		switch change.Kind {
		case slicelayout.ChangeWorkspace:
			action = map[string]any{"MoveWindowToWorkspace": niriipc.MoveWindowToWorkspaceAction{WindowID: proposal.RuntimeWindowID, Reference: niriipc.WorkspaceReference{ID: change.WorkspaceRuntimeID}, Focus: false}}
		case slicelayout.ChangeLayoutMode:
			if change.Mode == slicelayout.Floating {
				action = map[string]any{"MoveWindowToFloating": niriipc.WindowIDAction{ID: proposal.RuntimeWindowID}}
			} else {
				action = map[string]any{"MoveWindowToTiling": niriipc.WindowIDAction{ID: proposal.RuntimeWindowID}}
			}
		case slicelayout.ChangeWidth:
			action = map[string]any{"SetWindowWidth": niriipc.SetWindowSizeAction{ID: proposal.RuntimeWindowID, Change: niriipc.SetProportionChange{SetProportion: change.Percent}}}
		case slicelayout.ChangeHeight:
			action = map[string]any{"SetWindowHeight": niriipc.SetWindowSizeAction{ID: proposal.RuntimeWindowID, Change: niriipc.SetProportionChange{SetProportion: change.Percent}}}
		default:
			return errors.New("unsupported spatial change")
		}
		if validate != nil {
			fresh, err := client.Snapshot(ctx)
			if err != nil {
				return err
			}
			if err := validate(fresh); err != nil {
				return err
			}
			focusedFresh := uint64(0)
			for _, window := range fresh.Windows {
				if window.IsFocused {
					focusedFresh = window.ID
				}
			}
			if focusedFresh != focusedBefore {
				return errors.New("unrelated focus changed before spatial mutation")
			}
		}
		if err := client.Action(ctx, action); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, err := client.Snapshot(ctx)
			if err != nil {
				continue
			}
			focusedAfter := uint64(0)
			var target *niriipc.Window
			for i := range state.Windows {
				if state.Windows[i].IsFocused {
					focusedAfter = state.Windows[i].ID
				}
				if state.Windows[i].ID == proposal.RuntimeWindowID {
					target = &state.Windows[i]
				}
			}
			if target == nil {
				return errors.New("target window disappeared during verification")
			}
			if focusedAfter != focusedBefore {
				return errors.New("unrelated focus changed during spatial mutation")
			}
			workspaces := map[uint64]niriipc.Workspace{}
			for _, ws := range state.Workspaces {
				workspaces[ws.ID] = ws
			}
			verified := true
			for _, change := range proposal.Changes {
				switch change.Kind {
				case slicelayout.ChangeWorkspace:
					verified = verified && target.WorkspaceID != nil && *target.WorkspaceID == change.WorkspaceRuntimeID
				case slicelayout.ChangeLayoutMode:
					verified = verified && (target.IsFloating == (change.Mode == slicelayout.Floating))
				case slicelayout.ChangeWidth, slicelayout.ChangeHeight:
					if target.WorkspaceID == nil || len(target.Layout.WindowSize) != 2 {
						verified = false
						continue
					}
					ws, ok := workspaces[*target.WorkspaceID]
					if !ok || ws.Output == nil {
						verified = false
						continue
					}
					output, ok := state.Outputs[*ws.Output]
					if !ok {
						verified = false
						continue
					}
					actual := float64(target.Layout.WindowSize[0]) / float64(output.Logical.Width) * 100
					if change.Kind == slicelayout.ChangeHeight {
						actual = float64(target.Layout.WindowSize[1]) / float64(output.Logical.Height) * 100
					}
					verified = verified && math.Abs(actual-change.Percent) <= 0.02
				}
			}
			if verified {
				return nil
			}
		}
	}
}
