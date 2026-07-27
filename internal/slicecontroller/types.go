package slicecontroller

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmo/terminal-redeemer/internal/slicelayout"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"github.com/jmo/terminal-redeemer/internal/slicerpc"
	"github.com/jmo/terminal-redeemer/internal/sourceinventory"
)

var safeTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

const (
	SchemaVersion                        = 2
	MaxControlBytes                      = 1 << 20
	MaxControlResponseBytes              = 8 << 20
	MaxAuditEntries                      = 256
	MaxUndoEntries                       = 64
	MaxSelectedWorkspaces                = 256
	MaxTerminalSources                   = 2048
	MaxSuccessorGates                    = 256
	MaxLineageRecords                    = 256
	MaxLaunchHandoffs                    = 256
	MaxSpatialRecords                    = 1024
	MaxRetiredHandoffTombstones          = 1024
	MaxProjectionArgvEntries             = 256
	MaxProjectionArgvEntryBytes          = 4096
	MaxProjectionArgvTotalBytes          = 64 << 10
	MaxProjectionTransportOptions        = 64
	MaxProjectionFixedGeneratedArgvBytes = 32 << 10
	MaxProjectionTransportOptionBytes    = MaxProjectionArgvTotalBytes - MaxProjectionFixedGeneratedArgvBytes
)

type ConnectionState string

const (
	ConnectionLaunching    ConnectionState = "launching"
	ConnectionConnected    ConnectionState = "connected"
	ConnectionReconnecting ConnectionState = "reconnecting"
	ConnectionDisconnected ConnectionState = "disconnected"
)

type SourceLifecycle string

const (
	SourceEligible  SourceLifecycle = "eligible"
	SourceGoneGrace SourceLifecycle = "source_gone_grace"
	SourceConflict  SourceLifecycle = "conflict"
	SourceClosed    SourceLifecycle = "closed"
	SourceReplaced  SourceLifecycle = "replaced"
)

type ProjectionStatus string

const (
	ProjectionLaunching ProjectionStatus = "launching"
	ProjectionOwned     ProjectionStatus = "owned"
	ProjectionClosing   ProjectionStatus = "closing"
)

type Namespace struct {
	Host  string `json:"host"`
	Leech string `json:"leech"`
}

type ControllerConfig struct {
	Namespace               Namespace
	RetryWindow             time.Duration
	RetryInitialBackoff     time.Duration
	RetryMaxBackoff         time.Duration
	RetryMaxAttempts        int
	SourceGoneGrace         time.Duration
	SourceGoneConfirmations int
}

type Recovery struct {
	Generation    uint64    `json:"generation"`
	StartedAt     time.Time `json:"started_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Attempt       int       `json:"attempt"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
}

type TrackedSource struct {
	SourceID        string          `json:"source_id"`
	SourceEpoch     string          `json:"source_epoch"`
	SessionID       string          `json:"session_id"`
	SessionName     string          `json:"session_name"`
	WorkspaceKey    string          `json:"workspace_key"`
	Lifecycle       SourceLifecycle `json:"lifecycle"`
	Connection      ConnectionState `json:"connection,omitempty"`
	AbsenceCount    int             `json:"absence_count,omitempty"`
	AbsenceSince    time.Time       `json:"absence_since,omitempty"`
	AbsenceDeadline time.Time       `json:"absence_deadline,omitempty"`
	Recovery        *Recovery       `json:"recovery,omitempty"`
	Conflict        string          `json:"conflict,omitempty"`
}

type Projection struct {
	SourceID                string           `json:"source_id"`
	AppID                   string           `json:"app_id"`
	AttachToken             string           `json:"attach_token"`
	ExpectedSessionName     string           `json:"expected_session_name"`
	ProcessSourceID         string           `json:"process_source_id"`
	ExpectedKittyExecutable string           `json:"expected_kitty_executable,omitempty"`
	ExpectedKittyArgv       []string         `json:"expected_kitty_argv,omitempty"`
	ExpectedPID             int              `json:"expected_pid,omitempty"`
	NiriWindowID            uint64           `json:"niri_window_id,omitempty"`
	LeechCompositorEpoch    string           `json:"leech_compositor_epoch,omitempty"`
	AttachConfirmed         bool             `json:"attach_confirmed"`
	Status                  ProjectionStatus `json:"status"`
	CreatedAt               time.Time        `json:"created_at"`
}

type LineageRecord struct {
	OldSourceID       string    `json:"old_source_id"`
	SuccessorSourceID string    `json:"successor_source_id,omitempty"`
	SessionID         string    `json:"session_id"`
	Status            string    `json:"status"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type CleanupGate struct {
	SourceID  string    `json:"source_id"`
	Epoch     string    `json:"epoch"`
	Conflict  string    `json:"conflict,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SuccessorGate struct {
	OldSourceID string    `json:"old_source_id"`
	SessionID   string    `json:"session_id"`
	CreatedAt   time.Time `json:"created_at"`
	Conflict    string    `json:"conflict,omitempty"`
}

type LaunchHandoff struct {
	Token           string    `json:"token"`
	Status          string    `json:"status"`
	HostTerminalID  string    `json:"host_terminal_id,omitempty"`
	SessionName     string    `json:"session_name,omitempty"`
	WorkspaceName   string    `json:"workspace_name,omitempty"`
	SourceID        string    `json:"source_id,omitempty"`
	SourceEpoch     string    `json:"source_epoch,omitempty"`
	RuntimeWindowID uint64    `json:"runtime_window_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SessionDrop struct {
	SessionID       string    `json:"session_id"`
	SessionName     string    `json:"session_name"`
	CreatedAt       time.Time `json:"created_at"`
	AbsenceCount    int       `json:"absence_count,omitempty"`
	AbsenceSince    time.Time `json:"absence_since,omitempty"`
	AbsenceDeadline time.Time `json:"absence_deadline,omitempty"`
}

type UndoAction struct {
	Kind         string       `json:"kind"`
	SourceID     string       `json:"source_id,omitempty"`
	SessionID    string       `json:"session_id,omitempty"`
	SessionName  string       `json:"session_name,omitempty"`
	WorkspaceKey string       `json:"workspace_key,omitempty"`
	Previous     bool         `json:"previous"`
	PreviousDrop *SessionDrop `json:"previous_drop,omitempty"`
	At           time.Time    `json:"at"`
}

type AuditEntry struct {
	Generation uint64    `json:"generation"`
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail,omitempty"`
}

type SpatialRecovery struct {
	Attempt       int       `json:"attempt"`
	StartedAt     time.Time `json:"started_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	Stable        bool      `json:"stable"`
}

type SpatialRecord struct {
	// Baseline fields remain serialized for schema-v2 compatibility but are not
	// consulted by the host-authoritative planner.
	Baseline        *slicelayout.Spatial      `json:"baseline,omitempty"`
	PendingBaseline *slicelayout.Spatial      `json:"pending_baseline,omitempty"`
	LastApplied     *slicelayout.AppliedWrite `json:"last_applied,omitempty"`
	OrderDrift      []slicelayout.OrderDrift  `json:"order_drift,omitempty"`
	Conflict        string                    `json:"conflict,omitempty"`
	Recovery        *SpatialRecovery          `json:"recovery,omitempty"`
}

type State struct {
	SchemaVersion        int                           `json:"schema_version"`
	Namespace            Namespace                     `json:"namespace"`
	ControllerID         string                        `json:"controller_id"`
	Generation           uint64                        `json:"generation"`
	Acceptance           sliceprotocol.AcceptanceState `json:"acceptance"`
	Inventory            *sliceprotocol.Authoritative  `json:"inventory,omitempty"`
	ObservationQuality   sliceprotocol.Quality         `json:"observation_quality,omitempty"`
	ObservationCode      string                        `json:"observation_code,omitempty"`
	SelectedWorkspaces   map[string]string             `json:"selected_workspaces"`
	Pickups              map[string]bool               `json:"pickups"`
	ClosedByUser         map[string]SessionDrop        `json:"closed_by_user"`
	Sources              map[string]TrackedSource      `json:"sources"`
	Projections          map[string]Projection         `json:"projections"`
	SuccessorGates       map[string]SuccessorGate      `json:"successor_gates"`
	Lineage              map[string]LineageRecord      `json:"lineage"`
	PendingCleanups      map[string]CleanupGate        `json:"pending_cleanups"`
	LaunchHandoffs       map[string]LaunchHandoff      `json:"launch_handoffs"`
	RetiredHandoffTokens []string                      `json:"retired_handoff_tokens"`
	AuthorityMode        string                        `json:"authority_mode"`
	LeechWriteAuthorized bool                          `json:"leech_write_authorized"`
	Spatial              map[string]SpatialRecord      `json:"spatial"`
	Undo                 []UndoAction                  `json:"undo"`
	Audit                []AuditEntry                  `json:"audit"`
}

func NewState(namespace Namespace, controllerID string) State {
	return State{SchemaVersion: SchemaVersion, Namespace: namespace, ControllerID: controllerID, Generation: 1,
		SelectedWorkspaces: map[string]string{}, Pickups: map[string]bool{}, ClosedByUser: map[string]SessionDrop{}, Sources: map[string]TrackedSource{}, Projections: map[string]Projection{}, SuccessorGates: map[string]SuccessorGate{}, Lineage: map[string]LineageRecord{}, PendingCleanups: map[string]CleanupGate{}, LaunchHandoffs: map[string]LaunchHandoff{}, RetiredHandoffTokens: []string{}, AuthorityMode: "host_location", Spatial: map[string]SpatialRecord{}, Undo: []UndoAction{}, Audit: []AuditEntry{}}
}

func (s State) Validate() error {
	if s.SchemaVersion != SchemaVersion || !safeName(s.Namespace.Host) || !safeName(s.Namespace.Leech) || !safeName(s.ControllerID) || s.Generation == 0 {
		return errors.New("invalid controller state identity")
	}
	if s.ObservationQuality != "" && s.ObservationQuality != sliceprotocol.QualityComplete && s.ObservationQuality != sliceprotocol.QualityDegraded {
		return errors.New("invalid observation quality")
	}
	if s.AuthorityMode != "host_location" || s.LeechWriteAuthorized {
		return errors.New("v1 controller state requires host_location authority without leech write authorization")
	}
	if err := sliceprotocol.ValidateAcceptanceState(s.Acceptance); err != nil {
		return err
	}
	if s.Inventory != nil {
		if err := sliceprotocol.ValidateAuthoritative(*s.Inventory); err != nil {
			return fmt.Errorf("invalid retained inventory: %w", err)
		}
		hash, err := sliceprotocol.SemanticHash(*s.Inventory)
		if err != nil {
			return err
		}
		if s.Acceptance.SourceEpoch != s.Inventory.SourceEpoch || s.Acceptance.Revision != s.Inventory.Revision || s.Acceptance.SemanticHash != hash {
			return errors.New("accepted authority does not match retained inventory")
		}
	}
	if s.SelectedWorkspaces == nil || s.Pickups == nil || s.ClosedByUser == nil || s.Sources == nil || s.Projections == nil || s.SuccessorGates == nil || s.Lineage == nil || s.PendingCleanups == nil || s.LaunchHandoffs == nil || s.RetiredHandoffTokens == nil || s.Spatial == nil {
		return errors.New("controller state maps are required")
	}
	for key, display := range s.SelectedWorkspaces {
		normalized, err := sliceprotocol.NormalizeWorkspaceName(display)
		if err != nil || normalized != key {
			return fmt.Errorf("invalid selected workspace %q", key)
		}
	}
	for id, source := range s.Sources {
		if id == "" || id != source.SourceID || source.SourceEpoch == "" || source.SessionID == "" || source.SessionName == "" {
			return errors.New("invalid tracked source")
		}
		switch source.Lifecycle {
		case SourceEligible, SourceGoneGrace, SourceConflict, SourceClosed, SourceReplaced:
		default:
			return errors.New("invalid source lifecycle")
		}
		if source.Connection != "" {
			switch source.Connection {
			case ConnectionLaunching, ConnectionConnected, ConnectionReconnecting, ConnectionDisconnected:
			default:
				return errors.New("invalid connection state")
			}
		}
		if source.Recovery != nil && (source.Recovery.Generation == 0 || source.Recovery.ExpiresAt.IsZero() || source.Recovery.StartedAt.IsZero() || !source.Recovery.ExpiresAt.After(source.Recovery.StartedAt)) {
			return errors.New("invalid recovery")
		}
	}
	appIDs := map[string]string{}
	for id, projection := range s.Projections {
		if id != projection.SourceID || projection.AppID == "" || projection.AttachToken == "" || projection.ExpectedSessionName == "" || projection.ProcessSourceID == "" || projection.CreatedAt.IsZero() {
			return errors.New("invalid projection mapping")
		}
		hasProcessAuthority := projection.ExpectedKittyExecutable != "" || len(projection.ExpectedKittyArgv) > 0
		if projection.ExpectedPID > 0 && !hasProcessAuthority {
			return errors.New("launched projection lacks exact executable/argv authority")
		}
		if hasProcessAuthority {
			if projection.ExpectedKittyExecutable == "" || !utf8.ValidString(projection.ExpectedKittyExecutable) || len(projection.ExpectedKittyExecutable) > MaxProjectionArgvEntryBytes || strings.ContainsAny(projection.ExpectedKittyExecutable, "\x00\r\n") {
				return errors.New("projection executable authority exceeds bounds")
			}
			if err := ValidateProjectionArgv(projection.ExpectedKittyArgv); err != nil {
				return err
			}
		}
		if _, ok := s.Sources[id]; !ok {
			return errors.New("projection mapping references unknown source")
		}
		if prior, ok := appIDs[projection.AppID]; ok && prior != id {
			return errors.New("duplicate projection app id")
		}
		appIDs[projection.AppID] = id
		switch projection.Status {
		case ProjectionLaunching, ProjectionOwned, ProjectionClosing:
		default:
			return errors.New("invalid projection status")
		}
	}
	for id := range s.Pickups {
		if _, ok := s.Sources[id]; !ok {
			return errors.New("pickup references unknown source")
		}
	}
	for id, drop := range s.ClosedByUser {
		if id != drop.SessionID || !sliceprotocol.ValidSessionID(drop.SessionID) || !sliceprotocol.ValidSessionName(drop.SessionName) || drop.CreatedAt.IsZero() || drop.AbsenceCount < 0 {
			return errors.New("invalid session drop")
		}
		if drop.AbsenceCount == 0 {
			if !drop.AbsenceSince.IsZero() || !drop.AbsenceDeadline.IsZero() {
				return errors.New("session drop has absence times without evidence")
			}
		} else if drop.AbsenceSince.IsZero() || drop.AbsenceDeadline.IsZero() || !drop.AbsenceDeadline.After(drop.AbsenceSince) {
			return errors.New("session drop absence evidence is incomplete")
		}
	}
	for id, gate := range s.SuccessorGates {
		if gate.OldSourceID != id || gate.SessionID == "" || gate.CreatedAt.IsZero() {
			return errors.New("invalid successor gate")
		}
	}
	if len(s.RetiredHandoffTokens) > MaxRetiredHandoffTombstones {
		return errors.New("retired handoff tombstone capacity exhausted; explicit maintenance/re-enrollment required")
	}
	retiredTokens := map[string]bool{}
	for _, token := range s.RetiredHandoffTokens {
		if !safeTokenPattern.MatchString(token) || retiredTokens[token] {
			return errors.New("invalid retired handoff tombstones")
		}
		retiredTokens[token] = true
	}
	for token, handoff := range s.LaunchHandoffs {
		if token != handoff.Token || retiredTokens[token] || !safeTokenPattern.MatchString(token) || (handoff.HostTerminalID != "" && !safeTokenPattern.MatchString(handoff.HostTerminalID)) || (handoff.SourceID != "" && !safeTokenPattern.MatchString(handoff.SourceID)) || (handoff.SourceEpoch != "" && !safeTokenPattern.MatchString(handoff.SourceEpoch)) || (handoff.SessionName != "" && !safeTokenPattern.MatchString(handoff.SessionName)) || !utf8.ValidString(handoff.WorkspaceName) || len(handoff.WorkspaceName) > 255 || strings.ContainsAny(handoff.WorkspaceName, "\x00\r\n") || handoff.UpdatedAt.IsZero() {
			return errors.New("invalid launch handoff")
		}
		if (handoff.SessionName == "") != (handoff.WorkspaceName == "") {
			return errors.New("incomplete routed launch metadata")
		}
		if handoff.SessionName != "" {
			if handoff.SessionName != slicerpc.StableSessionName(handoff.Token) {
				return errors.New("non-deterministic routed session")
			}
			if _, err := sliceprotocol.NormalizeWorkspaceName(handoff.WorkspaceName); err != nil {
				return errors.New("invalid routed workspace")
			}
		}
		if handoff.Status != "launch_pending" && handoff.Status != "launched" && handoff.Status != "failed" && handoff.Status != "not_created" {
			return errors.New("invalid launch handoff status")
		}
		present := 0
		if handoff.SourceID != "" {
			present++
		}
		if handoff.SourceEpoch != "" {
			present++
		}
		if handoff.RuntimeWindowID != 0 {
			present++
		}
		if present != 0 && present != 3 {
			return errors.New("incomplete launch handoff source tuple")
		}
		if present == 3 {
			derived, err := sourceinventory.SourceID(handoff.SourceEpoch, handoff.RuntimeWindowID)
			if err != nil || derived != handoff.SourceID {
				return errors.New("invalid launch handoff source tuple")
			}
		}
	}
	if len(s.SelectedWorkspaces) > MaxSelectedWorkspaces || len(s.ClosedByUser) > MaxTerminalSources || len(s.Sources) > MaxTerminalSources || len(s.SuccessorGates) > MaxSuccessorGates || len(s.PendingCleanups) > MaxSuccessorGates || len(s.Lineage) > MaxLineageRecords || len(s.LaunchHandoffs) > MaxLaunchHandoffs || len(s.Spatial) > MaxSpatialRecords {
		return errors.New("controller current authority exceeds bounds")
	}
	for id, record := range s.Lineage {
		if id != record.OldSourceID || record.SessionID == "" || record.UpdatedAt.IsZero() || (record.Status != "unresolved" && record.Status != "rebound" && record.Status != "conflict") {
			return errors.New("invalid lineage record")
		}
	}
	for id, gate := range s.PendingCleanups {
		if id != gate.SourceID || gate.Epoch == "" || gate.UpdatedAt.IsZero() {
			return errors.New("invalid cleanup gate")
		}
	}
	for _, record := range s.Spatial {
		if record.Recovery != nil && (record.Recovery.Attempt < 1 || record.Recovery.StartedAt.IsZero() || !record.Recovery.ExpiresAt.After(record.Recovery.StartedAt)) {
			return errors.New("invalid spatial recovery")
		}
	}
	if len(s.Undo) > MaxUndoEntries || len(s.Audit) > MaxAuditEntries {
		return errors.New("history exceeds bounds")
	}
	return nil
}

func (s *State) Compact() error {
	sliceprotocol.CompactAcceptanceState(&s.Acceptance)
	if len(s.SelectedWorkspaces) > MaxSelectedWorkspaces {
		return errors.New("selected workspace authority exceeds bound")
	}
	active := map[string]bool{}
	if s.Inventory != nil {
		for _, source := range s.Inventory.Sources {
			active[source.SourceID] = true
		}
	}
	for id := range s.Pickups {
		active[id] = true
	}
	for id := range s.Projections {
		active[id] = true
	}
	for id := range s.SuccessorGates {
		active[id] = true
	}
	for id := range s.PendingCleanups {
		active[id] = true
	}
	for _, record := range s.Lineage {
		if record.Status != "rebound" {
			active[record.OldSourceID] = true
		}
		if record.SuccessorSourceID != "" {
			active[record.SuccessorSourceID] = true
		}
	}
	if len(active) > MaxTerminalSources {
		return errors.New("non-prunable source authority exceeds bound")
	}
	var terminal []string
	for id, source := range s.Sources {
		if !active[id] && (source.Lifecycle == SourceClosed || source.Lifecycle == SourceReplaced) {
			terminal = append(terminal, id)
		}
	}
	sort.Strings(terminal)
	for len(s.Sources) > MaxTerminalSources && len(terminal) > 0 {
		id := terminal[0]
		terminal = terminal[1:]
		delete(s.Sources, id)
		delete(s.Spatial, id)
	}
	if len(s.Sources) > MaxTerminalSources {
		return errors.New("non-prunable source authority exceeds bound")
	}
	if len(s.SuccessorGates) > MaxSuccessorGates || len(s.PendingCleanups) > MaxSuccessorGates {
		return errors.New("non-prunable gate authority exceeds bound")
	}
	var resolvedLineage []string
	for id, record := range s.Lineage {
		if record.Status == "rebound" {
			resolvedLineage = append(resolvedLineage, id)
		}
	}
	sort.Strings(resolvedLineage)
	for len(s.Lineage) > MaxLineageRecords && len(resolvedLineage) > 0 {
		id := resolvedLineage[0]
		resolvedLineage = resolvedLineage[1:]
		delete(s.Lineage, id)
	}
	if len(s.Lineage) > MaxLineageRecords {
		return errors.New("non-prunable lineage authority exceeds bound")
	}
	type handoffKey struct {
		id string
		at time.Time
	}
	var resolved []handoffKey
	pending := 0
	for id, h := range s.LaunchHandoffs {
		if h.Status == "launch_pending" {
			pending++
		} else {
			resolved = append(resolved, handoffKey{id, h.UpdatedAt})
		}
	}
	if pending > MaxLaunchHandoffs {
		return errors.New("non-prunable launch authority exceeds bound")
	}
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].at.Equal(resolved[j].at) {
			return resolved[i].id < resolved[j].id
		}
		return resolved[i].at.Before(resolved[j].at)
	})
	retireCount := len(s.LaunchHandoffs) - MaxLaunchHandoffs
	if retireCount > len(resolved) {
		return errors.New("non-prunable launch authority exceeds bound")
	}
	if retireCount > 0 && len(s.RetiredHandoffTokens)+retireCount > MaxRetiredHandoffTombstones {
		return errors.New("retired handoff tombstone capacity exhausted; explicit maintenance/re-enrollment required")
	}
	for retireCount > 0 {
		id := resolved[0].id
		resolved = resolved[1:]
		s.RetiredHandoffTokens = append(s.RetiredHandoffTokens, id)
		delete(s.LaunchHandoffs, id)
		retireCount--
	}
	sort.Strings(s.RetiredHandoffTokens)
	var orphanSpatial []string
	for id := range s.Spatial {
		if !active[id] {
			orphanSpatial = append(orphanSpatial, id)
		}
	}
	sort.Strings(orphanSpatial)
	for len(s.Spatial) > MaxSpatialRecords && len(orphanSpatial) > 0 {
		id := orphanSpatial[0]
		orphanSpatial = orphanSpatial[1:]
		delete(s.Spatial, id)
	}
	if len(s.Spatial) > MaxSpatialRecords {
		return errors.New("non-prunable spatial authority exceeds bound")
	}
	return nil
}

func retiredHandoffContains(state State, token string) bool {
	for _, retired := range state.RetiredHandoffTokens {
		if retired == token {
			return true
		}
	}
	return false
}

// ValidateProjectionArgv is the shared persisted/build-time argv boundary.
func ValidateProjectionArgv(argv []string) error {
	if len(argv) < 2 || len(argv) > MaxProjectionArgvEntries {
		return errors.New("projection argv entry count exceeds bound")
	}
	total := 0
	for _, entry := range argv {
		if entry == "" || !utf8.ValidString(entry) || len(entry) > MaxProjectionArgvEntryBytes || strings.ContainsAny(entry, "\x00\r\n") {
			return errors.New("projection argv entry exceeds bound")
		}
		total += len(entry)
	}
	if total > MaxProjectionArgvTotalBytes {
		return errors.New("projection argv aggregate exceeds bound")
	}
	return nil
}

// ValidateProjectionTransportOptions reserves half of the complete argv budget
// for fixed flags, bounded paths, projection identity, and every generated
// --transport-option flag. BuildProjectionCommand validates the exact result.
func ValidateProjectionTransportOptions(options []string) error {
	if len(options) > MaxProjectionTransportOptions {
		return errors.New("transport option count exceeds projection argv bound")
	}
	total := 0
	for _, option := range options {
		if option == "" || !utf8.ValidString(option) || len(option) > MaxProjectionArgvEntryBytes || strings.ContainsAny(option, "\x00\r\n") {
			return errors.New("transport option entry exceeds projection argv bound")
		}
		total += len(option)
	}
	if total > MaxProjectionTransportOptionBytes {
		return errors.New("transport option aggregate exceeds projection argv bound")
	}
	return nil
}

func safeName(value string) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n/")
}

func (s State) Wanted(sourceID string) bool {
	source, ok := s.Sources[sourceID]
	if !ok || (source.Lifecycle != SourceEligible && source.Lifecycle != SourceGoneGrace) {
		return false
	}
	_, dropped := s.ClosedByUser[source.SessionID]
	return (s.Pickups[sourceID] || s.SelectedWorkspaces[source.WorkspaceKey] != "") && !dropped
}

func (s State) SortedSourceIDs() []string {
	ids := make([]string, 0, len(s.Sources))
	for id := range s.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
