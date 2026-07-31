package slicecontroller

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

// legacyV10StateFixture freezes the strict contract-1.0 controller-state
// reader shape. It deliberately duplicates the old top-level schema rather
// than embedding State: adding a current field must not silently teach this
// compatibility reader about it.
type legacyV10StateFixture struct {
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

func readLegacyV10StateFixture(payload []byte) (legacyV10StateFixture, error) {
	var state legacyV10StateFixture
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return state, err
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, state); err != nil {
		return state, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return state, err
	}
	return state, nil
}

func TestLegacyV10ReaderRejectsActiveAllEligibleAndAcceptsDisabledState(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	engine, _ := newEngine(t, &now)

	state, _, err := engine.SelectWorkspace("Work", true)
	if err != nil {
		t.Fatal(err)
	}
	undoBefore := append([]UndoAction(nil), state.Undo...)
	auditBefore := append([]AuditEntry(nil), state.Audit...)

	state, _, err = engine.SelectAll(true)
	if err != nil {
		t.Fatal(err)
	}
	activePayload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readLegacyV10StateFixture(activePayload); err == nil || !strings.Contains(err.Error(), `unknown object key "all_eligible"`) {
		t.Fatalf("legacy reader accepted active all_eligible or returned the wrong error: %v", err)
	}
	if !reflect.DeepEqual(state.Undo, undoBefore) {
		t.Fatalf("enable changed undo records: got=%+v want=%+v", state.Undo, undoBefore)
	}

	state, _, err = engine.SelectAll(false)
	if err != nil {
		t.Fatal(err)
	}
	disabledPayload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disabledPayload, []byte(`"all_eligible"`)) {
		t.Fatalf("disabled state retained all_eligible: %s", disabledPayload)
	}
	legacyState, err := readLegacyV10StateFixture(disabledPayload)
	if err != nil {
		t.Fatalf("legacy reader rejected disabled state: %v", err)
	}
	if !reflect.DeepEqual(legacyState.Undo, undoBefore) {
		t.Fatalf("downgrade changed existing undo records: got=%+v want=%+v", legacyState.Undo, undoBefore)
	}
	if len(legacyState.Audit) != len(auditBefore)+2 || !reflect.DeepEqual(legacyState.Audit[:len(auditBefore)], auditBefore) {
		t.Fatalf("downgrade did not preserve existing audit records: got=%+v want-prefix=%+v", legacyState.Audit, auditBefore)
	}
	if legacyState.Audit[len(legacyState.Audit)-2].Kind != "all_eligible_selection" ||
		legacyState.Audit[len(legacyState.Audit)-2].Detail != "enabled=true" ||
		legacyState.Audit[len(legacyState.Audit)-1].Kind != "all_eligible_selection" ||
		legacyState.Audit[len(legacyState.Audit)-1].Detail != "enabled=false" {
		t.Fatalf("global toggle audit records missing after downgrade: %+v", legacyState.Audit)
	}
}
