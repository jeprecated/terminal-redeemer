package sourceinventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jmo/terminal-redeemer/internal/niriipc"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

// inventoryModel is an independent cardinality oracle. It knows only the v1
// evidence rules (verified Kitty, exactly one exact active session, one binding
// per session); it does not call Builder helpers or canonicalization.
type inventoryModel struct {
	epoch    string
	windows  map[int]inventoryWindow
	headless map[string]zellijlive.Status
}

type inventoryWindow struct {
	id         uint64
	pid        int
	appID      string
	kitty      bool
	candidates []string
	session    string
	status     zellijlive.Status
	column     int
	workspace  string
}

type inventoryModelOp struct {
	Kind string `json:"kind"`
	Slot int    `json:"slot,omitempty"`
	Peer int    `json:"peer,omitempty"`
}

type inventoryProcesses struct {
	values map[int]zellijlive.ProcessEvidence
	lost   map[int]bool
}

func (p inventoryProcesses) Observe(_ context.Context, pid int) (zellijlive.ProcessEvidence, error) {
	if p.lost[pid] {
		return zellijlive.ProcessEvidence{}, errors.New("process observation incomplete")
	}
	return p.values[pid], nil
}

func newInventoryModel() *inventoryModel {
	return &inventoryModel{epoch: "11111111-1111-4111-8111-111111111111", windows: map[int]inventoryWindow{}, headless: map[string]zellijlive.Status{}}
}

func inventoryName(slot int) string { return fmt.Sprintf("session-%d", slot) }
func inventorySessionID(slot int) string {
	letter := string(rune('A' + slot%26))
	return "ses_" + strings.Repeat(letter, 43)
}

func (m *inventoryModel) reset(slot int) {
	m.windows[slot] = inventoryWindow{id: uint64(100 + slot), pid: 1000 + slot, appID: "kitty", kitty: true, candidates: []string{inventoryName(slot)}, session: inventoryName(slot), status: zellijlive.StatusActive, column: slot + 1, workspace: "Dev"}
}

func (m *inventoryModel) apply(op inventoryModelOp) (processLoss bool) {
	switch op.Kind {
	case "present":
		m.reset(op.Slot)
	case "absent":
		delete(m.windows, op.Slot)
	case "headless":
		m.headless[inventoryName(op.Slot+10)] = zellijlive.StatusActive
	case "headless_absent":
		delete(m.headless, inventoryName(op.Slot+10))
	case "dead":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.status = zellijlive.StatusDeadResurrectable
		m.windows[op.Slot] = w
	case "prefix":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.status = zellijlive.StatusPrefixOnly
		m.windows[op.Slot] = w
	case "missing":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.status = ""
		m.windows[op.Slot] = w
	case "ambiguous":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.candidates = []string{inventoryName(op.Slot), inventoryName(op.Peer)}
		m.windows[op.Slot] = w
	case "candidate_absent":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.candidates = nil
		m.windows[op.Slot] = w
	case "unverified":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.kitty = false
		m.windows[op.Slot] = w
	case "nonkitty":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.kitty, w.appID = false, "firefox"
		m.windows[op.Slot] = w
	case "duplicate":
		m.reset(op.Slot)
		m.reset(op.Peer)
		w := m.windows[op.Slot]
		w.candidates = []string{inventoryName(op.Peer)}
		w.session = inventoryName(op.Peer)
		m.windows[op.Slot] = w
	case "move":
		m.reset(op.Slot)
		w := m.windows[op.Slot]
		w.workspace = []string{"Dev", "Ops"}[(op.Slot+op.Peer)%2]
		w.column = 1 + (op.Peer % 4)
		m.windows[op.Slot] = w
	case "process_loss":
		m.reset(op.Slot)
		processLoss = true
	}
	return processLoss
}

type inventoryRelation struct {
	windowID               uint64
	sessionID, sessionName string
	workspaceKey           string
	column                 int
}

type expectedInventory struct {
	relations map[uint64]inventoryRelation
	conflicts map[string]int
}

func (m *inventoryModel) catalogStatus(name string, fallback zellijlive.Status) zellijlive.Status {
	if status, ok := m.headless[name]; ok {
		return status
	}
	if owner, ok := m.windows[slotForName(name)]; ok && len(owner.candidates) == 1 && owner.candidates[0] == name {
		return owner.status
	}
	return fallback
}

func inventoryConflictKey(code sliceprotocol.ConflictCode, sessionID string) string {
	return string(code) + "|" + sessionID
}

func (m *inventoryModel) expected() expectedInventory {
	// First compute only the mathematical window↔exact-active-session relation.
	// Identity derivation, protocol Source construction, workspace/output shaping,
	// canonicalization, and Builder branch order are deliberately absent.
	active := map[uint64]inventoryRelation{}
	for _, window := range m.windows {
		if !window.kitty || len(window.candidates) != 1 {
			continue
		}
		name := window.candidates[0]
		if m.catalogStatus(name, window.status) != zellijlive.StatusActive {
			continue
		}
		active[window.id] = inventoryRelation{windowID: window.id, sessionID: inventorySessionID(slotForName(name)), sessionName: name, workspaceKey: strings.ToLower(window.workspace), column: window.column}
	}

	bySession := map[string][]uint64{}
	for id, relation := range active {
		bySession[relation.sessionID] = append(bySession[relation.sessionID], id)
	}
	relations := map[uint64]inventoryRelation{}
	for id, relation := range active {
		if len(bySession[relation.sessionID]) == 1 {
			relations[id] = relation
		}
	}

	// Conflict cardinalities are a separate predicate/multiset oracle. They are
	// intentionally not attached to derived SourceIDs.
	conflicts := map[string]int{}
	add := func(code sliceprotocol.ConflictCode, sessionID string) {
		conflicts[inventoryConflictKey(code, sessionID)]++
	}
	for _, window := range m.windows {
		if !window.kitty {
			if strings.Contains(strings.ToLower(window.appID), "kitty") {
				add(sliceprotocol.ConflictKittyProcessUnverified, "")
			}
			continue
		}
		switch len(window.candidates) {
		case 0:
			add(sliceprotocol.ConflictSessionCandidateMissing, "")
			continue
		case 1:
		default:
			add(sliceprotocol.ConflictSessionCandidateAmbiguous, "")
			continue
		}
		name := window.candidates[0]
		sessionID := inventorySessionID(slotForName(name))
		switch m.catalogStatus(name, window.status) {
		case zellijlive.StatusActive:
			if len(bySession[sessionID]) > 1 {
				add(sliceprotocol.ConflictSessionDuplicateBinding, sessionID)
			}
		case zellijlive.StatusDeadResurrectable:
			add(sliceprotocol.ConflictSessionDeadResurrectable, sessionID)
		case zellijlive.StatusPrefixOnly:
			add(sliceprotocol.ConflictSessionPrefixOnly, sessionID)
		default:
			add(sliceprotocol.ConflictSessionMissing, sessionID)
		}
	}
	return expectedInventory{relations: relations, conflicts: conflicts}
}

func slotForName(name string) int {
	var slot int
	_, _ = fmt.Sscanf(name, "session-%d", &slot)
	return slot
}

func (m *inventoryModel) production(lostSlot int, rng *rand.Rand) ([]sliceprotocol.Source, []sliceprotocol.Conflict, error) {
	output := "DP-1"
	workspaceNames := map[string]*string{}
	workspaceIDs := map[string]uint64{"Dev": 1, "Ops": 2}
	state := niriipc.State{Outputs: map[string]niriipc.Output{output: {Name: output, Logical: niriipc.Logical{Width: 1920, Height: 1080, Scale: 1, Transform: "Normal"}}}}
	for _, name := range []string{"Dev", "Ops"} {
		copy := name
		workspaceNames[name] = &copy
		state.Workspaces = append(state.Workspaces, niriipc.Workspace{ID: workspaceIDs[name], Index: int(workspaceIDs[name]), Name: workspaceNames[name], Output: &output})
	}
	catalog := zellijlive.Catalog{Sessions: map[string]zellijlive.Session{}, Names: []string{}}
	processes := inventoryProcesses{values: map[int]zellijlive.ProcessEvidence{}, lost: map[int]bool{}}
	candidateStatuses := map[string]zellijlive.Status{}
	var slots []int
	for slot := range m.windows {
		slots = append(slots, slot)
	}
	// Deliberately shuffle raw compositor order; canonical output must remain
	// identical to the independent sorted expectation.
	rng.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
	for _, slot := range slots {
		window := m.windows[slot]
		workspace := workspaceIDs[window.workspace]
		state.Windows = append(state.Windows, niriipc.Window{ID: window.id, AppID: window.appID, PID: window.pid, WorkspaceID: &workspace, Layout: niriipc.Layout{Position: []int{window.column, 1}, TileSize: []float64{900, 700}, WindowSize: []int{900, 700}}})
		processes.values[window.pid] = zellijlive.ProcessEvidence{KittyVerified: window.kitty, Candidates: append([]string(nil), window.candidates...)}
		if slot == lostSlot {
			processes.lost[window.pid] = true
		}
		for _, name := range window.candidates {
			candidateStatuses[name] = m.catalogStatus(name, window.status)
		}
	}
	for name, status := range m.headless {
		candidateStatuses[name] = status
	}
	for name, status := range candidateStatuses {
		slot := slotForName(name)
		catalog.Sessions[name] = zellijlive.Session{Name: name, ID: inventorySessionID(slot), Status: status}
	}
	for name := range catalog.Sessions {
		catalog.Names = append(catalog.Names, name)
	}
	return (Builder{Processes: processes}).Build(context.Background(), m.epoch, state, catalog)
}

func inventoryPrefix() []inventoryModelOp {
	return []inventoryModelOp{{Kind: "present", Slot: 0}, {Kind: "present", Slot: 1}, {Kind: "headless", Slot: 5}, {Kind: "dead", Slot: 0}, {Kind: "prefix", Slot: 0}, {Kind: "missing", Slot: 0}, {Kind: "ambiguous", Slot: 0, Peer: 1}, {Kind: "candidate_absent", Slot: 0}, {Kind: "unverified", Slot: 0}, {Kind: "nonkitty", Slot: 0}, {Kind: "present", Slot: 0}, {Kind: "duplicate", Slot: 0, Peer: 1}, {Kind: "process_loss", Slot: 0}, {Kind: "absent", Slot: 1}, {Kind: "headless_absent", Slot: 5}, {Kind: "present", Slot: 0}}
}

func generatedInventoryOps(seed int64, count int) []inventoryModelOp {
	rng := rand.New(rand.NewSource(seed))
	kinds := []string{"present", "present", "absent", "headless", "headless_absent", "dead", "prefix", "missing", "ambiguous", "candidate_absent", "unverified", "nonkitty", "duplicate", "move", "process_loss"}
	ops := append([]inventoryModelOp(nil), inventoryPrefix()...)
	for len(ops) < count {
		slot := rng.Intn(5)
		peer := rng.Intn(5)
		if peer == slot {
			peer = (peer + 1) % 5
		}
		ops = append(ops, inventoryModelOp{Kind: kinds[rng.Intn(len(kinds))], Slot: slot, Peer: peer})
	}
	return ops
}

func TestModelInventoryGeneratedSequences(t *testing.T) {
	seeds := []int64{2, 5, 17, 31, 0x1badb002, 0x51515151}
	if raw := os.Getenv("TERMINAL_REDEEMER_INVENTORY_MODEL_SEED"); raw != "" {
		seed, err := strconv.ParseInt(raw, 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		seeds = []int64{seed}
	}
	for _, seed := range seeds {
		seed := seed
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			model := newInventoryModel()
			rng := rand.New(rand.NewSource(seed ^ 0x4567))
			ops := generatedInventoryOps(seed, 220)
			identities := map[uint64]string{}
			metamorphicWitnesses := map[string]int{}
			for step, op := range ops {
				processLoss := model.apply(op)
				lostSlot := -1
				if processLoss {
					lostSlot = op.Slot
				}
				sources, conflicts, err := model.production(lostSlot, rng)
				if processLoss {
					if err == nil || !strings.Contains(err.Error(), string(sliceprotocol.ReasonProcessObservationIncomplete)) {
						failInventoryModel(t, seed, step, ops[:step+1], fmt.Errorf("process-evidence removal outcome=%v", err))
					}
					metamorphicWitnesses["process_evidence_removal"]++
					continue
				}
				if err != nil {
					failInventoryModel(t, seed, step, ops[:step+1], err)
				}
				if err := assertInventoryOracle(model, sources, conflicts, identities); err != nil {
					failInventoryModel(t, seed, step, ops[:step+1], err)
				}

				// Same semantic input in another raw compositor order must retain
				// identities and canonical ordering exactly.
				shuffledSources, shuffledConflicts, err := model.production(-1, rng)
				if err != nil || inventoryJSON(sources, conflicts) != inventoryJSON(shuffledSources, shuffledConflicts) {
					failInventoryModel(t, seed, step, ops[:step+1], fmt.Errorf("input-shuffle metamorphism changed output: err=%v", err))
				}
				metamorphicWitnesses["input_shuffle"]++

				if step%17 == 0 {
					checkInventoryMetamorphisms(t, seed, step, ops[:step+1], model, sources, conflicts, rng, metamorphicWitnesses)
				}
			}
			for _, name := range []string{"input_shuffle", "unrelated_headless", "unrelated_dead", "unrelated_prefix", "exact_active_duplication", "process_evidence_removal"} {
				if metamorphicWitnesses[name] == 0 {
					t.Fatalf("seed=%d mandatory inventory metamorphism %s was not witnessed: %+v", seed, name, metamorphicWitnesses)
				}
			}
			t.Logf("seed=%d oracle=window↔exact-active-session relation/conflict multiset witnesses=%+v", seed, metamorphicWitnesses)
		})
	}
}

func assertInventoryOracle(model *inventoryModel, sources []sliceprotocol.Source, conflicts []sliceprotocol.Conflict, identities map[uint64]string) error {
	expected := model.expected()
	if len(sources) != len(expected.relations) {
		return fmt.Errorf("eligible cardinality=%d oracle=%d relations=%+v", len(sources), len(expected.relations), expected.relations)
	}
	seenSessions, seenSources, seenWindows := map[string]bool{}, map[string]bool{}, map[uint64]bool{}
	for index, source := range sources {
		relation, ok := expected.relations[source.RuntimeWindowID]
		if !ok {
			return fmt.Errorf("published window %d is outside oracle relation", source.RuntimeWindowID)
		}
		if seenSessions[source.Session.ID] || seenSources[source.SourceID] || seenWindows[source.RuntimeWindowID] {
			return errors.New("duplicate eligible projection identity became reachable")
		}
		seenSessions[source.Session.ID], seenSources[source.SourceID], seenWindows[source.RuntimeWindowID] = true, true, true
		if source.Session.ID != relation.sessionID || source.Session.Name != relation.sessionName || source.Session.Status != "active" || source.Workspace.Key != relation.workspaceKey || source.Layout.Position == nil || source.Layout.Position.Column != relation.column {
			return fmt.Errorf("relation semantics window=%d source=%+v oracle=%+v", source.RuntimeWindowID, source, relation)
		}
		if prior := identities[source.RuntimeWindowID]; prior != "" && prior != source.SourceID {
			return fmt.Errorf("source identity changed for stable epoch/window %d: %s -> %s", source.RuntimeWindowID, prior, source.SourceID)
		}
		identities[source.RuntimeWindowID] = source.SourceID
		if index > 0 && !sourceCanonicalLess(sources[index-1], source) {
			return fmt.Errorf("sources not in strict canonical order: %+v then %+v", sources[index-1], source)
		}
	}
	actualConflicts := map[string]int{}
	seenConflictIDs := map[string]bool{}
	for index, conflict := range conflicts {
		actualConflicts[inventoryConflictKey(conflict.Code, conflict.SessionID)]++
		if seenConflictIDs[conflict.SourceID] {
			return fmt.Errorf("multiple conflict outcomes for source identity %s", conflict.SourceID)
		}
		seenConflictIDs[conflict.SourceID] = true
		if index > 0 && !conflictCanonicalLess(conflicts[index-1], conflict) {
			return fmt.Errorf("conflicts not in strict canonical order: %+v then %+v", conflicts[index-1], conflict)
		}
	}
	if inventoryMapJSON(actualConflicts) != inventoryMapJSON(expected.conflicts) {
		return fmt.Errorf("conflict multiset=%v oracle=%v", actualConflicts, expected.conflicts)
	}
	return nil
}

func sourceCanonicalLess(a, b sliceprotocol.Source) bool {
	if a.Workspace.Key != b.Workspace.Key {
		return a.Workspace.Key < b.Workspace.Key
	}
	aColumn, bColumn := 0, 0
	if a.Layout.Position != nil {
		aColumn = a.Layout.Position.Column
	}
	if b.Layout.Position != nil {
		bColumn = b.Layout.Position.Column
	}
	if aColumn != bColumn {
		return aColumn < bColumn
	}
	return a.SourceID < b.SourceID
}

func conflictCanonicalLess(a, b sliceprotocol.Conflict) bool {
	if a.Code != b.Code {
		return a.Code < b.Code
	}
	if a.SourceID != b.SourceID {
		return a.SourceID < b.SourceID
	}
	return a.SessionID < b.SessionID
}

func inventoryJSON(sources []sliceprotocol.Source, conflicts []sliceprotocol.Conflict) string {
	payload, _ := json.Marshal(struct {
		Sources   []sliceprotocol.Source   `json:"sources"`
		Conflicts []sliceprotocol.Conflict `json:"conflicts"`
	}{sources, conflicts})
	return string(payload)
}

func inventoryMapJSON(value map[string]int) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}

func cloneInventoryModel(model *inventoryModel) *inventoryModel {
	clone := newInventoryModel()
	clone.epoch = model.epoch
	for slot, window := range model.windows {
		window.candidates = append([]string(nil), window.candidates...)
		clone.windows[slot] = window
	}
	for name, status := range model.headless {
		clone.headless[name] = status
	}
	return clone
}

func checkInventoryMetamorphisms(t *testing.T, seed int64, step int, ops []inventoryModelOp, model *inventoryModel, baselineSources []sliceprotocol.Source, baselineConflicts []sliceprotocol.Conflict, rng *rand.Rand, witnesses map[string]int) {
	t.Helper()
	baseline := inventoryJSON(baselineSources, baselineConflicts)
	headless := cloneInventoryModel(model)
	headless.headless[inventoryName(70)] = zellijlive.StatusActive
	sources, conflicts, err := headless.production(-1, rng)
	if err != nil || inventoryJSON(sources, conflicts) != baseline {
		failInventoryModel(t, seed, step, ops, fmt.Errorf("unrelated headless addition changed authority: %v", err))
	}
	witnesses["unrelated_headless"]++

	for _, mutation := range []struct {
		name   string
		slot   int
		status zellijlive.Status
	}{{"unrelated_dead", 71, zellijlive.StatusDeadResurrectable}, {"unrelated_prefix", 72, zellijlive.StatusPrefixOnly}} {
		variant := cloneInventoryModel(model)
		variant.reset(mutation.slot)
		window := variant.windows[mutation.slot]
		window.status = mutation.status
		variant.windows[mutation.slot] = window
		sources, conflicts, err = variant.production(-1, rng)
		if err != nil || assertInventoryOracle(variant, sources, conflicts, map[uint64]string{}) != nil {
			failInventoryModel(t, seed, step, ops, fmt.Errorf("%s addition violated relation oracle: %v", mutation.name, err))
		}
		if len(sources) != len(baselineSources) {
			failInventoryModel(t, seed, step, ops, fmt.Errorf("%s changed eligible relation cardinality", mutation.name))
		}
		witnesses[mutation.name]++
	}

	expected := model.expected()
	for _, relation := range expected.relations {
		variant := cloneInventoryModel(model)
		variant.windows[80] = inventoryWindow{id: 180, pid: 1080, appID: "kitty", kitty: true, candidates: []string{relation.sessionName}, session: relation.sessionName, status: zellijlive.StatusActive, column: 4, workspace: "Dev"}
		sources, conflicts, err = variant.production(-1, rng)
		if err != nil {
			failInventoryModel(t, seed, step, ops, err)
		}
		if oracleErr := assertInventoryOracle(variant, sources, conflicts, map[uint64]string{}); oracleErr != nil {
			failInventoryModel(t, seed, step, ops, fmt.Errorf("exact-active duplication: %w", oracleErr))
		}
		if len(sources) != len(baselineSources)-1 {
			failInventoryModel(t, seed, step, ops, errors.New("exact-active duplication did not suppress both bindings"))
		}
		witnesses["exact_active_duplication"]++
		break
	}
}

func failInventoryModel(t *testing.T, seed int64, step int, ops []inventoryModelOp, err error) {
	t.Helper()
	payload, _ := json.Marshal(ops)
	t.Fatalf("seed=%d step=%d error=%v\nreplay: TERMINAL_REDEEMER_INVENTORY_MODEL_SEED=%d go test ./internal/sourceinventory -run '^TestModelInventoryGeneratedSequences$'\noperations=%s", seed, step, err, seed, payload)
}
