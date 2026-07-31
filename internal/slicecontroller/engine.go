package slicecontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jmo/terminal-redeemer/internal/slicelayout"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

type EffectKind string

const (
	EffectLaunchProjection EffectKind = "launch_projection"
	EffectCloseProjection  EffectKind = "close_projection"
	EffectApplySpatial     EffectKind = "apply_spatial"
)

type Effect struct {
	Kind          EffectKind            `json:"kind"`
	SourceID      string                `json:"source_id"`
	SessionName   string                `json:"session_name,omitempty"`
	Projection    Projection            `json:"projection,omitempty"`
	WindowID      uint64                `json:"window_id,omitempty"`
	FocusRequired bool                  `json:"focus_required,omitempty"`
	Proposal      *slicelayout.Proposal `json:"proposal,omitempty"`
}

type Engine struct {
	mu        sync.Mutex
	effectsMu sync.Mutex
	Store     *Store
	Config    ControllerConfig
	Now       func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}
func (e *Engine) load() (State, error) {
	if e.Store == nil {
		return State{}, errors.New("controller store unavailable")
	}
	return e.Store.Read()
}
func (e *Engine) commit(state *State, kind, detail string) error {
	if err := state.Compact(); err != nil {
		return err
	}
	state.Generation++
	state.Audit = append(state.Audit, AuditEntry{Generation: state.Generation, At: e.now(), Kind: kind, Detail: detail})
	if len(state.Audit) > MaxAuditEntries {
		state.Audit = append([]AuditEntry(nil), state.Audit[len(state.Audit)-MaxAuditEntries:]...)
	}
	return e.Store.Write(*state)
}

func (e *Engine) ExecuteEffects(ctx context.Context, effects []Effect, execute func(context.Context, []Effect) error) error {
	if len(effects) == 0 || execute == nil {
		return nil
	}
	e.effectsMu.Lock()
	defer e.effectsMu.Unlock()
	if err := execute(ctx, effects); err != nil {
		for _, effect := range effects {
			if effect.Kind == EffectLaunchProjection {
				if recoveryErr := e.RecoverUnstartedLaunches(effects); recoveryErr != nil {
					return errors.Join(err, fmt.Errorf("recover unstarted launch effects: %w", recoveryErr))
				}
				break
			}
		}
		return err
	}
	return nil
}

// RecoverUnstartedLaunches removes only launch mappings that an interrupted
// effect batch never prepared, allowing normal bounded retry to converge them.
func (e *Engine) RecoverUnstartedLaunches(effects []Effect) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return err
	}
	changed := 0
	for _, effect := range effects {
		if effect.Kind != EffectLaunchProjection {
			continue
		}
		projection, ok := state.Projections[effect.SourceID]
		if !ok || projection.Status != ProjectionLaunching || projection.AppID != effect.Projection.AppID || projection.ExpectedKittyExecutable != "" || len(projection.ExpectedKittyArgv) != 0 || projection.ExpectedPID != 0 {
			continue
		}
		delete(state.Projections, effect.SourceID)
		source := state.Sources[effect.SourceID]
		e.startRecovery(&source, e.now())
		state.Sources[effect.SourceID] = source
		changed++
	}
	if changed == 0 {
		return nil
	}
	return e.commit(&state, "projection_effect_interrupted", fmt.Sprintf("unstarted=%d", changed))
}

func (e *Engine) Status() (State, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.load() }

func (e *Engine) SelectWorkspace(name string, selected bool) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	key, err := sliceprotocol.NormalizeWorkspaceName(name)
	if err != nil {
		return State{}, nil, err
	}
	previous := state.SelectedWorkspaces[key] != ""
	if previous == selected {
		return state, nil, nil
	}
	if selected {
		state.SelectedWorkspaces[key] = strings.TrimSpace(name)
	} else {
		delete(state.SelectedWorkspaces, key)
	}
	e.pushUndo(&state, UndoAction{Kind: "workspace", WorkspaceKey: key, Previous: previous, At: e.now()})
	effects := e.reconcileDesired(&state, e.now())
	if err := e.commit(&state, "workspace_selection", fmt.Sprintf("%s=%t", key, selected)); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}
func (e *Engine) SelectAll(selected bool) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	if state.AllEligible == selected {
		return state, nil, nil
	}
	state.AllEligible = selected
	effects := e.reconcileDesired(&state, e.now())
	if err := e.commit(&state, "all_eligible_selection", fmt.Sprintf("enabled=%t", selected)); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}

func (e *Engine) Pickup(sourceID string, enabled bool) (State, []Effect, error) {
	return e.override(sourceID, "pickup", enabled)
}
func (e *Engine) Close(sourceID string) (State, []Effect, error) {
	return e.override(sourceID, "close", true)
}

type FocusedCloseRollback struct {
	sourceID            string
	sessionID           string
	committedGeneration uint64
	createdDrop         SessionDrop
	previousSource      TrackedSource
	committedSource     TrackedSource
	previousProjection  Projection
	committedProjection Projection
	previousUndo        []UndoAction
	committedUndo       []UndoAction
}

func (e *Engine) CloseFocused(sourceID string) (State, []Effect, *FocusedCloseRollback, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, nil, err
	}
	source, ok := state.Sources[sourceID]
	if !ok {
		return State{}, nil, nil, errors.New("unknown source")
	}
	if _, excluded := state.ClosedByUser[source.SessionID]; excluded {
		return state, nil, nil, nil
	}
	projection, ok := state.Projections[sourceID]
	if !ok || projection.Status != ProjectionOwned || projection.NiriWindowID == 0 {
		return State{}, nil, nil, errors.New("focused close requires an owned projection mapping")
	}

	now := e.now()
	rollback := &FocusedCloseRollback{
		sourceID:           sourceID,
		sessionID:          source.SessionID,
		previousSource:     source,
		previousProjection: projection,
		previousUndo:       append([]UndoAction(nil), state.Undo...),
	}
	createdDrop := SessionDrop{SessionID: source.SessionID, SessionName: source.SessionName, CreatedAt: now}
	state.ClosedByUser[source.SessionID] = createdDrop
	e.pushUndo(&state, UndoAction{Kind: "close", SourceID: sourceID, SessionID: source.SessionID, SessionName: source.SessionName, Previous: false, At: now})
	effects := e.reconcileDesired(&state, now)
	focusedEffect := false
	for i := range effects {
		if effects[i].Kind == EffectCloseProjection && effects[i].SourceID == sourceID && effects[i].WindowID == projection.NiriWindowID {
			effects[i].FocusRequired = true
			focusedEffect = true
		}
	}
	if !focusedEffect {
		return State{}, nil, nil, errors.New("focused close did not produce an exact close effect")
	}
	committedSource := state.Sources[sourceID]
	committedSource.Connection = ""
	// Stop attempts but retain an already-running absolute recovery deadline so
	// bounded cross-epoch lineage remains possible after a successful close.
	state.Sources[sourceID] = committedSource
	if err := e.commit(&state, "close", sourceID); err != nil {
		return State{}, nil, nil, err
	}
	rollback.committedGeneration = state.Generation
	rollback.createdDrop = createdDrop
	rollback.committedSource = state.Sources[sourceID]
	rollback.committedProjection = state.Projections[sourceID]
	rollback.committedUndo = append([]UndoAction(nil), state.Undo...)
	return state, effects, rollback, nil
}

func (e *Engine) RollbackFocusedClose(token *FocusedCloseRollback) (State, error) {
	if token == nil {
		return State{}, errors.New("focused close rollback token is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	drop, dropped := state.ClosedByUser[token.sessionID]
	projection, projected := state.Projections[token.sourceID]
	// An executor may durably recover an unrelated launch before returning its
	// error. Preserve that newer state, but roll back only while every field
	// owned by this focused-close token is still exact.
	if state.Generation < token.committedGeneration || !dropped || !reflect.DeepEqual(drop, token.createdDrop) || !reflect.DeepEqual(state.Sources[token.sourceID], token.committedSource) || !projected || !reflect.DeepEqual(projection, token.committedProjection) || !reflect.DeepEqual(state.Undo, token.committedUndo) {
		return State{}, errors.New("focused close rollback token is no longer current")
	}
	delete(state.ClosedByUser, token.sessionID)
	state.Sources[token.sourceID] = token.previousSource
	state.Projections[token.sourceID] = token.previousProjection
	state.Undo = append([]UndoAction(nil), token.previousUndo...)
	if err := e.commit(&state, "focused_close_rollback", token.sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}
func (e *Engine) Reopen(sourceID string) (State, []Effect, error) {
	return e.override(sourceID, "close", false)
}
func (e *Engine) override(sourceID, kind string, enabled bool) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	source, ok := state.Sources[sourceID]
	if !ok {
		return State{}, nil, errors.New("unknown source")
	}
	now := e.now()
	if kind == "close" {
		previousDrop, previous := state.ClosedByUser[source.SessionID]
		if previous == enabled {
			return state, nil, nil
		}
		action := UndoAction{Kind: kind, SourceID: sourceID, SessionID: source.SessionID, SessionName: source.SessionName, Previous: previous, At: now}
		if previous {
			copy := previousDrop
			action.PreviousDrop = &copy
		}
		if enabled {
			state.ClosedByUser[source.SessionID] = SessionDrop{SessionID: source.SessionID, SessionName: source.SessionName, CreatedAt: now}
		} else {
			delete(state.ClosedByUser, source.SessionID)
		}
		e.pushUndo(&state, action)
	} else {
		previous := state.Pickups[sourceID]
		if previous == enabled {
			return state, nil, nil
		}
		if enabled {
			state.Pickups[sourceID] = true
		} else {
			delete(state.Pickups, sourceID)
		}
		e.pushUndo(&state, UndoAction{Kind: kind, SourceID: sourceID, Previous: previous, At: now})
	}
	effects := e.reconcileDesired(&state, e.now())
	if kind == "close" && enabled {
		source := state.Sources[sourceID]
		source.Connection = ""
		// Stop attempts but retain an already-running absolute recovery
		// deadline so bounded cross-epoch lineage remains possible.
		state.Sources[sourceID] = source
	}
	if err := e.commit(&state, kind, sourceID); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}
func (e *Engine) pushUndo(state *State, a UndoAction) {
	state.Undo = append(state.Undo, a)
	if len(state.Undo) > MaxUndoEntries {
		state.Undo = append([]UndoAction(nil), state.Undo[len(state.Undo)-MaxUndoEntries:]...)
	}
}
func (e *Engine) Undo() (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	var a UndoAction
	found := false
	for len(state.Undo) > 0 {
		a = state.Undo[len(state.Undo)-1]
		state.Undo = state.Undo[:len(state.Undo)-1]
		if a.Kind == "workspace" || (a.Kind == "close" && sliceprotocol.ValidSessionID(a.SessionID)) {
			found = true
			break
		}
		source, ok := state.Sources[a.SourceID]
		if ok && source.Lifecycle != SourceClosed && source.Lifecycle != SourceReplaced {
			found = true
			break
		}
	}
	if !found {
		if err := e.commit(&state, "undo_no_target", ""); err != nil {
			return State{}, nil, err
		}
		return state, nil, errors.New("nothing to undo")
	}
	switch a.Kind {
	case "workspace":
		if a.Previous {
			if state.SelectedWorkspaces[a.WorkspaceKey] == "" {
				state.SelectedWorkspaces[a.WorkspaceKey] = a.WorkspaceKey
			}
		} else {
			delete(state.SelectedWorkspaces, a.WorkspaceKey)
		}
	case "pickup":
		if a.Previous {
			state.Pickups[a.SourceID] = true
		} else {
			delete(state.Pickups, a.SourceID)
		}
	case "close":
		if a.Previous {
			if a.PreviousDrop == nil {
				return State{}, nil, errors.New("undo close entry lacks prior session drop")
			}
			state.ClosedByUser[a.SessionID] = *a.PreviousDrop
		} else {
			delete(state.ClosedByUser, a.SessionID)
		}
	default:
		return State{}, nil, errors.New("undo entry unsupported")
	}
	effects := e.reconcileDesired(&state, e.now())
	if err := e.commit(&state, "undo", a.Kind); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}

func (e *Engine) PrepareStartup() (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	changed := false
	for id, source := range state.Sources {
		if source.Connection == ConnectionConnected {
			source.Connection = ConnectionReconnecting
			if source.Recovery == nil {
				e.startRecovery(&source, e.now())
			}
			state.Sources[id] = source
			changed = true
		}
	}
	if changed {
		if err := e.commit(&state, "startup_reobservation", ""); err != nil {
			return State{}, err
		}
	}
	return state, nil
}

func (e *Engine) RecordObservationFailure(code string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	if code == "" || len(code) > 128 {
		return State{}, errors.New("invalid observation failure code")
	}
	state.ObservationQuality = sliceprotocol.QualityDegraded
	state.ObservationCode = code
	if err := e.commit(&state, "observation_degraded", code); err != nil {
		return State{}, err
	}
	return state, nil
}

func (e *Engine) ApplyEnvelope(envelope sliceprotocol.Envelope, receivedAt time.Time) (State, []Effect, sliceprotocol.AcceptanceDecision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, "", err
	}
	accepted, err := sliceprotocol.Accept(state.Acceptance, envelope, receivedAt)
	if err != nil {
		return State{}, nil, "", err
	}
	state.ObservationQuality = envelope.Observation.Quality
	state.ObservationCode = ""
	if envelope.Observation.Quality == sliceprotocol.QualityDegraded {
		if len(envelope.Observation.DegradedReasons) > 0 {
			state.ObservationCode = string(envelope.Observation.DegradedReasons[0].Code)
		}
		state.Acceptance = accepted.State
		if err := e.commit(&state, "observation_degraded", state.ObservationCode); err != nil {
			return State{}, nil, "", err
		}
		return state, nil, accepted.Decision, nil
	}
	if accepted.Decision != sliceprotocol.DecisionAccepted && accepted.Decision != sliceprotocol.DecisionFullResync {
		if accepted.Decision != sliceprotocol.DecisionDuplicate {
			state.ObservationQuality = sliceprotocol.QualityDegraded
			state.ObservationCode = "snapshot_" + string(accepted.Decision)
		}
		state.Acceptance = accepted.State
		if err := e.commit(&state, "snapshot_"+string(accepted.Decision), ""); err != nil {
			return State{}, nil, "", err
		}
		return state, nil, accepted.Decision, nil
	}
	oldInventory := state.Inventory
	state.Acceptance = accepted.State
	authority := *envelope.Authoritative
	state.Inventory = &authority
	e.reconcileSessionDrops(&state, authority, receivedAt)
	var effects []Effect
	if accepted.Decision == sliceprotocol.DecisionFullResync && oldInventory != nil {
		effects = e.handleEpochReplacement(&state, *oldInventory, authority, receivedAt)
	}
	effects = append(effects, e.reconcileInventory(&state, authority, receivedAt)...)
	for token, handoff := range state.LaunchHandoffs {
		if handoff.Status == "launch_pending" && handoff.SourceID != "" && launchHandoffMatches(state, handoff) {
			handoff.Status = "launched"
			handoff.UpdatedAt = e.now()
			state.LaunchHandoffs[token] = handoff
		}
	}
	if err := e.commit(&state, "snapshot_"+string(accepted.Decision), fmt.Sprintf("revision=%d", authority.Revision)); err != nil {
		return State{}, nil, "", err
	}
	return state, effects, accepted.Decision, nil
}

func (e *Engine) reconcileInventory(state *State, authority sliceprotocol.Authoritative, now time.Time) []Effect {
	present := map[string]sliceprotocol.Source{}
	conflicted := map[string]string{}
	for _, conflict := range authority.Conflicts {
		if conflict.SourceID != "" {
			conflicted[conflict.SourceID] = string(conflict.Code)
		}
	}
	for _, source := range authority.Sources {
		present[source.SourceID] = source
		tracked := state.Sources[source.SourceID]
		tracked.SourceID = source.SourceID
		tracked.SourceEpoch = authority.SourceEpoch
		tracked.SessionID = source.Session.ID
		tracked.SessionName = source.Session.Name
		tracked.WorkspaceKey = source.Workspace.Key
		tracked.Lifecycle = SourceEligible
		tracked.Conflict = ""
		tracked.AbsenceCount = 0
		tracked.AbsenceSince = time.Time{}
		tracked.AbsenceDeadline = time.Time{}
		state.Sources[source.SourceID] = tracked
	}
	e.resolveUnresolvedLineage(state, authority, now)
	var effects []Effect
	for id, tracked := range state.Sources {
		if tracked.SourceEpoch != authority.SourceEpoch || tracked.Lifecycle == SourceReplaced || tracked.Lifecycle == SourceClosed {
			continue
		}
		if _, ok := present[id]; ok {
			continue
		}
		if code := conflicted[id]; code != "" {
			tracked.Lifecycle = SourceConflict
			tracked.Conflict = code
			tracked.AbsenceCount = 0
			tracked.AbsenceSince = time.Time{}
			tracked.AbsenceDeadline = time.Time{}
			state.Sources[id] = tracked
			continue
		}
		tracked.AbsenceCount++
		if tracked.AbsenceSince.IsZero() {
			tracked.AbsenceSince = now
			tracked.AbsenceDeadline = now.Add(e.config().SourceGoneGrace)
		}
		tracked.Lifecycle = SourceGoneGrace
		if tracked.AbsenceCount >= e.config().SourceGoneConfirmations || !now.Before(tracked.AbsenceDeadline) {
			tracked.Lifecycle = SourceClosed
			tracked.Connection = ""
			tracked.Recovery = nil
			delete(state.Pickups, id)
			delete(state.SuccessorGates, id)
			if p, ok := state.Projections[id]; ok {
				p.Status = ProjectionClosing
				state.Projections[id] = p
				effects = append(effects, Effect{Kind: EffectCloseProjection, SourceID: id, WindowID: p.NiriWindowID, Projection: p})
			}
		}
		state.Sources[id] = tracked
	}
	return append(effects, e.reconcileDesired(state, now)...)
}
func (e *Engine) reconcileSessionDrops(state *State, authority sliceprotocol.Authoritative, now time.Time) {
	live := make(map[string]bool, len(authority.LiveSessionIDs))
	for _, id := range authority.LiveSessionIDs {
		live[id] = true
	}
	// A complete inventory may still carry per-source conflicts. Those conflicts
	// make this revision unsuitable as session-drop evidence: do not advance,
	// expire, or reset any independently persisted drop record from it.
	if len(authority.Conflicts) != 0 {
		return
	}
	c := e.config()
	for id, drop := range state.ClosedByUser {
		if live[id] {
			drop.AbsenceCount = 0
			drop.AbsenceSince = time.Time{}
			drop.AbsenceDeadline = time.Time{}
			state.ClosedByUser[id] = drop
			continue
		}
		drop.AbsenceCount++
		if drop.AbsenceSince.IsZero() {
			drop.AbsenceSince = now
			drop.AbsenceDeadline = now.Add(c.SourceGoneGrace)
		}
		if drop.AbsenceCount >= c.SourceGoneConfirmations && !now.Before(drop.AbsenceDeadline) {
			delete(state.ClosedByUser, id)
			continue
		}
		state.ClosedByUser[id] = drop
	}
}

func (e *Engine) config() ControllerConfig {
	c := e.Config
	if c.RetryWindow <= 0 {
		c.RetryWindow = 30 * time.Second
	}
	if c.RetryInitialBackoff <= 0 {
		c.RetryInitialBackoff = 200 * time.Millisecond
	}
	if c.RetryMaxBackoff <= 0 {
		c.RetryMaxBackoff = 2 * time.Second
	}
	if c.RetryMaxAttempts <= 0 {
		c.RetryMaxAttempts = 5
	}
	if c.SourceGoneGrace <= 0 {
		c.SourceGoneGrace = 5 * time.Second
	}
	if c.SourceGoneConfirmations < 2 {
		c.SourceGoneConfirmations = 2
	}
	return c
}

func (e *Engine) reconcileDesired(state *State, now time.Time) []Effect {
	var effects []Effect
	for _, id := range desiredSourceOrder(*state) {
		source := state.Sources[id]
		if source.Lifecycle == SourceConflict {
			continue
		}
		wanted := state.Wanted(id)
		if !wanted {
			if p, ok := state.Projections[id]; ok && p.Status != ProjectionClosing {
				p.Status = ProjectionClosing
				state.Projections[id] = p
				effects = append(effects, Effect{Kind: EffectCloseProjection, SourceID: id, WindowID: p.NiriWindowID, Projection: p})
			}
			continue
		}
		if successorGated(*state, source) {
			continue
		}
		if source.Recovery != nil && !now.Before(source.Recovery.ExpiresAt) {
			source.Connection = ConnectionDisconnected
			source.Recovery = nil
			state.Sources[id] = source
			state.SuccessorGates[id] = SuccessorGate{OldSourceID: id, SessionID: source.SessionID, CreatedAt: now}
			continue
		}
		if source.Connection == ConnectionDisconnected {
			continue
		}
		if source.Recovery != nil && !source.Recovery.NextAttemptAt.IsZero() && now.Before(source.Recovery.NextAttemptAt) {
			continue
		}
		if len(state.PendingCleanups) > 0 {
			continue
		}
		if _, ok := state.Projections[id]; !ok && source.Lifecycle == SourceEligible {
			p := newProjection(id, source.SessionName, now)
			state.Projections[id] = p
			if source.Connection == "" {
				if source.Recovery != nil {
					source.Connection = ConnectionReconnecting
				} else {
					source.Connection = ConnectionLaunching
				}
			}
			state.Sources[id] = source
			effects = append(effects, Effect{Kind: EffectLaunchProjection, SourceID: id, SessionName: source.SessionName, Projection: p})
		}
	}
	return effects
}
func desiredSourceOrder(state State) []string {
	if state.Inventory == nil {
		return state.SortedSourceIDs()
	}
	items := make([]slicelayout.OrderItem, 0, len(state.Inventory.Sources))
	seen := map[string]bool{}
	for _, source := range state.Inventory.Sources {
		items = append(items, slicelayout.OrderItem{SourceID: source.SourceID, Position: source.Layout.Position})
		seen[source.SourceID] = true
	}
	orderedItems := slicelayout.InitialLaunchOrder(items)
	ids := make([]string, 0, len(state.Sources))
	for _, item := range orderedItems {
		ids = append(ids, item.SourceID)
	}
	var extra []string
	for id := range state.Sources {
		if !seen[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	return append(ids, extra...)
}

func successorGated(state State, source TrackedSource) bool {
	for oldID, gate := range state.SuccessorGates {
		if oldID == source.SourceID || (gate.SessionID != "" && gate.SessionID == source.SessionID) {
			return true
		}
	}
	return false
}
func newProjection(sourceID, sessionName string, now time.Time) Projection {
	sum := sha256.Sum256([]byte(sourceID))
	short := hex.EncodeToString(sum[:8])
	return Projection{SourceID: sourceID, AppID: "terminal-redeemer-slice-" + short, AttachToken: "att_" + short, ExpectedSessionName: sessionName, ProcessSourceID: sourceID, Status: ProjectionLaunching, CreatedAt: now}
}

func (e *Engine) PrepareProjection(sourceID, executable string, argv []string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	p, ok := state.Projections[sourceID]
	if !ok || p.Status != ProjectionLaunching {
		return State{}, errors.New("projection mapping is not launchable")
	}
	if !filepath.IsAbs(executable) || len(argv) < 2 {
		return State{}, errors.New("exact packaged Kitty identity and argv are required")
	}
	p.ExpectedKittyExecutable = filepath.Clean(executable)
	p.ExpectedKittyArgv = append([]string(nil), argv...)
	state.Projections[sourceID] = p
	if err := e.commit(&state, "projection_prepared", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}

func (e *Engine) RecordLaunch(sourceID string, pid int, launchErr error) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	p, ok := state.Projections[sourceID]
	if !ok {
		return State{}, errors.New("projection mapping missing")
	}
	source := state.Sources[sourceID]
	if launchErr == nil && pid > 0 {
		p.ExpectedPID = pid
		state.Projections[sourceID] = p
	} else {
		delete(state.Projections, sourceID)
		e.startRecovery(&source, e.now())
	}
	state.Sources[sourceID] = source
	if err := e.commit(&state, "projection_launch_result", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}
func (e *Engine) startRecovery(source *TrackedSource, now time.Time) {
	c := e.config()
	if source.Connection == ConnectionDisconnected {
		return
	}
	source.Connection = ConnectionReconnecting
	if source.Recovery == nil {
		source.Recovery = &Recovery{Generation: uint64(now.UnixNano()), StartedAt: now, ExpiresAt: now.Add(c.RetryWindow), NextAttemptAt: now.Add(c.RetryInitialBackoff)}
	}
}
func (e *Engine) AttachmentConnected(sourceID string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	source, ok := state.Sources[sourceID]
	p, mapped := state.Projections[sourceID]
	_, dropped := state.ClosedByUser[source.SessionID]
	if !ok || !mapped || dropped {
		return State{}, errors.New("projection is not connectable")
	}
	p.AttachConfirmed = true
	state.Projections[sourceID] = p
	if p.Status == ProjectionOwned {
		source.Connection = ConnectionConnected
		source.Recovery = nil
		state.Sources[sourceID] = source
	}
	if err := e.commit(&state, "attachment_connected", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}

func (e *Engine) AttachmentLost(sourceID string) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	source, ok := state.Sources[sourceID]
	if !ok {
		return State{}, nil, errors.New("unknown source")
	}
	if _, dropped := state.ClosedByUser[source.SessionID]; dropped {
		return state, nil, nil
	}
	e.startRecovery(&source, e.now())
	state.Sources[sourceID] = source
	var effects []Effect
	if p, ok := state.Projections[sourceID]; ok {
		p.Status = ProjectionClosing
		state.Projections[sourceID] = p
		effects = append(effects, Effect{Kind: EffectCloseProjection, SourceID: sourceID, WindowID: p.NiriWindowID, Projection: p})
	}
	if err := e.commit(&state, "attachment_lost", sourceID); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}

func (e *Engine) CompleteCleanup(sourceID string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	if _, ok := state.PendingCleanups[sourceID]; !ok {
		return state, nil
	}
	delete(state.PendingCleanups, sourceID)
	delete(state.Projections, sourceID)
	if err := e.commit(&state, "projection_cleanup_complete", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}
func (e *Engine) RecordCleanupFailure(sourceID, code string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	gate, ok := state.PendingCleanups[sourceID]
	if !ok {
		return State{}, errors.New("cleanup gate missing")
	}
	gate.Conflict = code
	gate.UpdatedAt = e.now()
	state.PendingCleanups[sourceID] = gate
	if err := e.commit(&state, "projection_cleanup_conflict", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}

func (e *Engine) Tick() (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	now := e.now()
	var effects []Effect
	changed := false
	c := e.config()
	for id, drop := range state.ClosedByUser {
		if drop.AbsenceCount >= c.SourceGoneConfirmations && !drop.AbsenceDeadline.IsZero() && !now.Before(drop.AbsenceDeadline) {
			delete(state.ClosedByUser, id)
			changed = true
		}
	}
	for oldID, lineage := range state.Lineage {
		if lineage.Status != "unresolved" {
			continue
		}
		source, ok := state.Sources[oldID]
		if !ok || source.Recovery == nil {
			continue
		}
		if !now.Before(source.Recovery.ExpiresAt) || source.Recovery.Attempt >= c.RetryMaxAttempts {
			source.Connection = ConnectionDisconnected
			source.Recovery = nil
			state.Sources[oldID] = source
			state.SuccessorGates[oldID] = SuccessorGate{OldSourceID: oldID, SessionID: lineage.SessionID, CreatedAt: now}
			changed = true
		}
	}
	for id, source := range state.Sources {
		if source.Lifecycle == SourceGoneGrace && !source.AbsenceDeadline.IsZero() && !now.Before(source.AbsenceDeadline) {
			source.Lifecycle = SourceClosed
			source.Connection = ""
			source.Recovery = nil
			delete(state.Pickups, id)
			delete(state.SuccessorGates, id)
			if p, ok := state.Projections[id]; ok && p.Status != ProjectionClosing {
				p.Status = ProjectionClosing
				state.Projections[id] = p
				effects = append(effects, Effect{Kind: EffectCloseProjection, SourceID: id, WindowID: p.NiriWindowID, Projection: p})
			}
			state.Sources[id] = source
			changed = true
			continue
		}
		if source.Connection != ConnectionReconnecting || source.Recovery == nil {
			continue
		}
		r := source.Recovery
		if !now.Before(r.ExpiresAt) || r.Attempt >= c.RetryMaxAttempts {
			source.Connection = ConnectionDisconnected
			source.Recovery = nil
			state.SuccessorGates[id] = SuccessorGate{OldSourceID: id, SessionID: source.SessionID, CreatedAt: now}
			state.Sources[id] = source
			changed = true
			continue
		}
		// Recovery authority may be retained while source evidence is
		// temporarily conflicted, but conflict/replacement/closure can never
		// authorize a fresh projection launch. Exhaustion above remains
		// unconditional so every bounded episode reaches a stable gate.
		if source.Lifecycle == SourceConflict || source.Lifecycle == SourceClosed || source.Lifecycle == SourceReplaced || !state.Wanted(id) {
			continue
		}
		if now.Before(r.NextAttemptAt) {
			continue
		}
		if len(state.PendingCleanups) > 0 {
			continue
		}
		if _, exists := state.Projections[id]; exists {
			continue
		}
		p := newProjection(id, source.SessionName, now)
		state.Projections[id] = p
		r.Attempt++
		backoff := c.RetryInitialBackoff * time.Duration(1<<min(r.Attempt, 20))
		if backoff > c.RetryMaxBackoff {
			backoff = c.RetryMaxBackoff
		}
		r.NextAttemptAt = now.Add(backoff)
		source.Recovery = r
		state.Sources[id] = source
		effects = append(effects, Effect{Kind: EffectLaunchProjection, SourceID: id, SessionName: source.SessionName, Projection: p})
		changed = true
	}
	desiredEffects := e.reconcileDesired(&state, now)
	if len(desiredEffects) > 0 {
		effects = append(effects, desiredEffects...)
		changed = true
	}
	if changed {
		if err := e.commit(&state, "retry_tick", ""); err != nil {
			return State{}, nil, err
		}
	}
	return state, effects, nil
}
func (e *Engine) Reconnect(sourceID string) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	source, exists := state.Sources[sourceID]
	if !exists {
		return State{}, nil, errors.New("unknown source")
	}
	if len(state.PendingCleanups) > 0 {
		return State{}, nil, errors.New("reconnect blocked until pending projection cleanup is proven complete")
	}
	gateID := sourceID
	gate, gated := state.SuccessorGates[gateID]
	if !gated {
		for oldID, candidateGate := range state.SuccessorGates {
			if candidateGate.SessionID == source.SessionID {
				gateID = oldID
				gate = candidateGate
				gated = true
				break
			}
		}
	}
	if gated {
		if _, dropped := state.ClosedByUser[gate.SessionID]; dropped {
			return State{}, nil, errors.New("source is closed_by_user; reopen first")
		}
		candidates := matchingDesiredSources(state, gate.SessionID, gateID)
		if len(candidates) != 1 {
			return State{}, nil, fmt.Errorf("successor resolution requires exactly one eligible desired candidate, got %d", len(candidates))
		}
		candidate := candidates[0]
		source = state.Sources[candidate]
		if state.Pickups[gateID] {
			state.Pickups[candidate] = true
			delete(state.Pickups, gateID)
		}
		if candidate != gateID {
			state.Lineage[gateID] = LineageRecord{OldSourceID: gateID, SuccessorSourceID: candidate, SessionID: gate.SessionID, Status: "rebound", UpdatedAt: e.now()}
		}
		delete(state.SuccessorGates, gateID)
		sourceID = candidate
	} else {
		if _, dropped := state.ClosedByUser[source.SessionID]; dropped {
			return State{}, nil, errors.New("source is closed_by_user; reopen first")
		}
		if source.Lifecycle != SourceEligible || !state.Wanted(sourceID) {
			return State{}, nil, errors.New("reconnect requires an eligible desired source")
		}
	}
	source.Connection = ConnectionReconnecting
	source.Recovery = nil
	e.startRecovery(&source, e.now())
	state.Sources[sourceID] = source
	effects := e.reconcileDesired(&state, e.now())
	if err := e.commit(&state, "explicit_reconnect", sourceID); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}
func matchingDesiredSources(state State, sessionID, oldID string) []string {
	var ids []string
	for id, source := range state.Sources {
		_, dropped := state.ClosedByUser[source.SessionID]
		desired := state.Wanted(id) || (state.Pickups[oldID] && !dropped)
		if source.Lifecycle == SourceEligible && source.SessionID == sessionID && desired {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
func (e *Engine) handleEpochReplacement(state *State, old, new sliceprotocol.Authoritative, now time.Time) []Effect {
	newBySession := map[string][]sliceprotocol.Source{}
	for _, source := range new.Sources {
		newBySession[source.Session.ID] = append(newBySession[source.Session.ID], source)
	}
	var effects []Effect
	for _, oldSource := range old.Sources {
		tracked, ok := state.Sources[oldSource.SourceID]
		if !ok {
			continue
		}
		candidates := newBySession[tracked.SessionID]
		if tracked.Recovery != nil && now.Before(tracked.Recovery.ExpiresAt) {
			switch len(candidates) {
			case 1:
				e.rebindSuccessor(state, oldSource.SourceID, candidates[0], new.SourceEpoch, now, "rebound")
			case 0:
				tracked.Lifecycle = SourceReplaced
				tracked.Connection = ""
				tracked.Conflict = "lineage_unresolved"
				state.Sources[oldSource.SourceID] = tracked
				state.Lineage[oldSource.SourceID] = LineageRecord{OldSourceID: oldSource.SourceID, SessionID: tracked.SessionID, Status: "unresolved", UpdatedAt: now}
			default:
				tracked.Lifecycle = SourceReplaced
				tracked.Connection = ""
				tracked.Conflict = "ambiguous_successor"
				state.Sources[oldSource.SourceID] = tracked
				state.Lineage[oldSource.SourceID] = LineageRecord{OldSourceID: oldSource.SourceID, SessionID: tracked.SessionID, Status: "conflict", UpdatedAt: now}
			}
			continue
		}
		if tracked.Connection == ConnectionDisconnected || state.SuccessorGates[oldSource.SourceID].SessionID != "" {
			tracked.Lifecycle = SourceReplaced
			state.Sources[oldSource.SourceID] = tracked
			gate := state.SuccessorGates[oldSource.SourceID]
			if gate.SessionID == "" {
				gate = SuccessorGate{OldSourceID: oldSource.SourceID, SessionID: tracked.SessionID, CreatedAt: now}
			}
			if len(candidates) > 1 {
				gate.Conflict = "ambiguous_successor"
			}
			state.SuccessorGates[oldSource.SourceID] = gate
			continue
		}
		tracked.Lifecycle = SourceReplaced
		tracked.Connection = ""
		tracked.Recovery = nil
		state.Sources[oldSource.SourceID] = tracked
		delete(state.Pickups, oldSource.SourceID)
		if p, ok := state.Projections[oldSource.SourceID]; ok {
			p.Status = ProjectionClosing
			state.Projections[oldSource.SourceID] = p
			state.PendingCleanups[oldSource.SourceID] = CleanupGate{SourceID: oldSource.SourceID, Epoch: new.SourceEpoch, UpdatedAt: now}
			effects = append(effects, Effect{Kind: EffectCloseProjection, SourceID: oldSource.SourceID, WindowID: p.NiriWindowID, Projection: p})
		}
	}
	return effects
}
func (e *Engine) rebindSuccessor(state *State, oldID string, candidate sliceprotocol.Source, epoch string, now time.Time, status string) {
	tracked := state.Sources[oldID]
	next := tracked
	next.SourceID = candidate.SourceID
	next.SourceEpoch = epoch
	next.WorkspaceKey = candidate.Workspace.Key
	next.SessionName = candidate.Session.Name
	next.Lifecycle = SourceEligible
	next.Conflict = ""
	state.Sources[candidate.SourceID] = next
	delete(state.Sources, oldID)
	if state.Pickups[oldID] {
		state.Pickups[candidate.SourceID] = true
		delete(state.Pickups, oldID)
	}
	if p, ok := state.Projections[oldID]; ok {
		delete(state.Projections, oldID)
		p.SourceID = candidate.SourceID
		state.Projections[candidate.SourceID] = p
	}
	state.Lineage[oldID] = LineageRecord{OldSourceID: oldID, SuccessorSourceID: candidate.SourceID, SessionID: tracked.SessionID, Status: status, UpdatedAt: now}
}
func (e *Engine) resolveUnresolvedLineage(state *State, authority sliceprotocol.Authoritative, now time.Time) {
	bySession := map[string][]sliceprotocol.Source{}
	for _, source := range authority.Sources {
		bySession[source.Session.ID] = append(bySession[source.Session.ID], source)
	}
	for oldID, record := range state.Lineage {
		if record.Status != "unresolved" {
			continue
		}
		tracked, ok := state.Sources[oldID]
		if !ok || tracked.Recovery == nil || !now.Before(tracked.Recovery.ExpiresAt) {
			continue
		}
		candidates := bySession[record.SessionID]
		if len(candidates) == 1 {
			e.rebindSuccessor(state, oldID, candidates[0], authority.SourceEpoch, now, "rebound")
		} else if len(candidates) > 1 {
			record.Status = "conflict"
			record.UpdatedAt = now
			state.Lineage[oldID] = record
			tracked.Conflict = "ambiguous_successor"
			state.Sources[oldID] = tracked
		}
	}
}

func (e *Engine) ObserveLocal(epoch string, windows []OwnedWindow) (State, []Effect, error) {
	return e.ObserveLocalWithConflicts(epoch, windows, nil)
}
func (e *Engine) ObserveLocalWithConflicts(epoch string, windows []OwnedWindow, conflicts map[string]string) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	bySource := map[string]OwnedWindow{}
	for _, w := range windows {
		bySource[w.SourceID] = w
	}
	changed := false
	var effects []Effect
	for id, p := range state.Projections {
		w, present := bySource[id]
		source := state.Sources[id]
		if conflict := conflicts[id]; conflict != "" {
			if gate, cleanup := state.PendingCleanups[id]; cleanup {
				gate.Conflict = conflict
				gate.UpdatedAt = e.now()
				state.PendingCleanups[id] = gate
			} else {
				source.Lifecycle = SourceConflict
				source.Conflict = conflict
				state.Sources[id] = source
			}
			changed = true
			continue
		}
		if present {
			if source.Lifecycle == SourceConflict && strings.HasPrefix(source.Conflict, "projection_") {
				source.Lifecycle = SourceEligible
				source.Conflict = ""
			}
			if p.ExpectedPID != 0 && p.ExpectedPID != w.PID {
				continue
			}
			p.ExpectedPID = w.PID
			p.NiriWindowID = w.WindowID
			p.LeechCompositorEpoch = epoch
			p.Status = ProjectionOwned
			state.Projections[id] = p
			if p.AttachConfirmed && source.Connection != ConnectionConnected {
				source.Connection = ConnectionConnected
				source.Recovery = nil
				state.Sources[id] = source
			}
			changed = true
			continue
		}
		if p.Status == ProjectionClosing {
			delete(state.Projections, id)
			delete(state.PendingCleanups, id)
			changed = true
			continue
		}
		if p.Status == ProjectionLaunching {
			// Exact remote attachment may legitimately consume most of the
			// configured recovery window while proving the pinned socket and
			// interactive client. Do not retire its durable PID on an unrelated
			// five-second compositor-publication race.
			launchGrace := e.Config.RetryWindow
			if launchGrace <= 0 {
				launchGrace = 5 * time.Second
			}
			if e.now().Sub(p.CreatedAt) < launchGrace {
				continue
			}
			delete(state.Projections, id)
			e.startRecovery(&source, e.now())
			state.Sources[id] = source
			changed = true
			continue
		}
		if state.ObservationQuality == sliceprotocol.QualityComplete && authorityContainsSource(state.Inventory, id) && source.Lifecycle == SourceEligible {
			if source.Connection == ConnectionReconnecting || source.Connection == ConnectionLaunching || !p.AttachConfirmed {
				delete(state.Projections, id)
				if source.Recovery == nil {
					e.startRecovery(&source, e.now())
					state.Sources[id] = source
				}
				changed = true
			} else {
				state.ClosedByUser[source.SessionID] = SessionDrop{SessionID: source.SessionID, SessionName: source.SessionName, CreatedAt: e.now()}
				source.Connection = ""
				source.Recovery = nil
				state.Sources[id] = source
				delete(state.Projections, id)
				e.pushUndo(&state, UndoAction{Kind: "close", SourceID: id, SessionID: source.SessionID, SessionName: source.SessionName, Previous: false, At: e.now()})
				changed = true
			}
		}
	}
	if changed {
		effects = e.reconcileDesired(&state, e.now())
		if err := e.commit(&state, "local_observation", ""); err != nil {
			return State{}, nil, err
		}
	}
	return state, effects, nil
}

func authorityContainsSource(authority *sliceprotocol.Authoritative, sourceID string) bool {
	if authority == nil {
		return false
	}
	for _, source := range authority.Sources {
		if source.SourceID == sourceID {
			return true
		}
	}
	return false
}

type OwnedWindow struct {
	SourceID string
	WindowID uint64
	PID      int
	AppID    string
	Focused  bool
}

func (e *Engine) RecordSpatial(sourceID string, result slicelayout.Result) (State, []Effect, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, nil, err
	}
	record := state.Spatial[sourceID]
	record.OrderDrift = result.OrderDrift
	if len(result.Conflicts) > 0 {
		record.Conflict = string(result.Conflicts[0].Code)
	} else {
		record.Conflict = ""
	}
	var effects []Effect
	for i := range result.Proposals {
		p := result.Proposals[i]
		if p.Target == slicelayout.Host {
			return State{}, nil, errors.New("v1 rejects host-target spatial proposals")
		}
		record.LastApplied = &slicelayout.AppliedWrite{Origin: p.Origin, Target: p.Target}
		copy := p
		effects = append(effects, Effect{Kind: EffectApplySpatial, SourceID: sourceID, Proposal: &copy})
	}
	state.Spatial[sourceID] = record
	if err := e.commit(&state, "spatial_plan", sourceID); err != nil {
		return State{}, nil, err
	}
	return state, effects, nil
}

func (e *Engine) RecordSpatialFailure(sourceID, code string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	record := state.Spatial[sourceID]
	record.LastApplied = nil
	record.Conflict = code
	now := e.now()
	c := e.config()
	if record.Recovery == nil {
		record.Recovery = &SpatialRecovery{StartedAt: now, ExpiresAt: now.Add(c.RetryWindow)}
	}
	record.Recovery.Attempt++
	if record.Recovery.Attempt >= c.RetryMaxAttempts || !now.Before(record.Recovery.ExpiresAt) {
		record.Recovery.Stable = true
		record.Recovery.NextAttemptAt = time.Time{}
		record.Conflict = "spatial_retry_exhausted"
	} else {
		backoff := c.RetryInitialBackoff * time.Duration(1<<min(record.Recovery.Attempt-1, 20))
		if backoff > c.RetryMaxBackoff {
			backoff = c.RetryMaxBackoff
		}
		record.Recovery.NextAttemptAt = now.Add(backoff)
	}
	state.Spatial[sourceID] = record
	if err := e.commit(&state, "spatial_execution_failed", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}

func (e *Engine) CompleteSpatial(sourceID string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	record := state.Spatial[sourceID]
	record.LastApplied = nil
	record.Recovery = nil
	record.Conflict = ""
	state.Spatial[sourceID] = record
	if err := e.commit(&state, "spatial_verified", sourceID); err != nil {
		return State{}, err
	}
	return state, nil
}

func (e *Engine) SetLaunchHandoff(h LaunchHandoff) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.load()
	if err != nil {
		return State{}, err
	}
	if retiredHandoffContains(state, h.Token) {
		return State{}, errors.New("retired routed launch token cannot be reused")
	}
	if !safeTokenPattern.MatchString(h.Token) || (h.HostTerminalID != "" && !safeTokenPattern.MatchString(h.HostTerminalID)) || (h.SourceID != "" && !safeTokenPattern.MatchString(h.SourceID)) || (h.SourceEpoch != "" && !safeTokenPattern.MatchString(h.SourceEpoch)) || (h.SessionName != "" && !safeTokenPattern.MatchString(h.SessionName)) || len(h.WorkspaceName) > 255 || strings.ContainsAny(h.WorkspaceName, "\x00\r\n") || (h.Status != "launch_pending" && h.Status != "launched" && h.Status != "failed" && h.Status != "not_created") {
		return State{}, errors.New("invalid routed launch handoff")
	}
	if old, ok := state.LaunchHandoffs[h.Token]; ok {
		if old.HostTerminalID != "" {
			if h.HostTerminalID != "" && h.HostTerminalID != old.HostTerminalID {
				return State{}, errors.New("routed launch identity conflict")
			}
			h.HostTerminalID = old.HostTerminalID
		}
		for _, pair := range [][2]string{{old.SessionName, h.SessionName}, {old.WorkspaceName, h.WorkspaceName}, {old.SourceID, h.SourceID}, {old.SourceEpoch, h.SourceEpoch}} {
			if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
				return State{}, errors.New("routed launch metadata conflict")
			}
		}
		if h.SessionName == "" {
			h.SessionName = old.SessionName
		}
		if h.WorkspaceName == "" {
			h.WorkspaceName = old.WorkspaceName
		}
		if h.SourceID == "" {
			h.SourceID = old.SourceID
		}
		if h.SourceEpoch == "" {
			h.SourceEpoch = old.SourceEpoch
		}
		if old.RuntimeWindowID != 0 && h.RuntimeWindowID != 0 && old.RuntimeWindowID != h.RuntimeWindowID {
			return State{}, errors.New("routed launch runtime identity conflict")
		}
		if h.RuntimeWindowID == 0 {
			h.RuntimeWindowID = old.RuntimeWindowID
		}
		if old.Status == "launched" || old.Status == "failed" || old.Status == "not_created" {
			if h.Status != old.Status {
				return State{}, errors.New("routed launch status cannot regress or change terminal outcome")
			}
		}
	}
	if (h.Status == "launched" || h.Status == "failed") && h.HostTerminalID == "" {
		return State{}, errors.New("resolved routed launch requires stable host identity")
	}
	h.UpdatedAt = e.now()
	state.LaunchHandoffs[h.Token] = h
	if err := e.commit(&state, "launch_handoff", h.Token); err != nil {
		return State{}, err
	}
	return state, nil
}
