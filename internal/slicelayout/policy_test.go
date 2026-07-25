package slicelayout

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

type resolutionFixture struct {
	SourceOutput    sliceprotocol.Output `json:"source_output"`
	TargetOutput    sliceprotocol.Output `json:"target_output"`
	SourceWindow    []int                `json:"source_window"`
	TargetWindow    []int                `json:"target_window"`
	ExpectedPercent []float64            `json:"expected_percent"`
}

func loadFixture(t *testing.T, name string) resolutionFixture {
	t.Helper()
	payload, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var fixture resolutionFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.SourceWindow) != 2 || len(fixture.TargetWindow) != 2 || len(fixture.ExpectedPercent) != 2 {
		t.Fatalf("fixture vectors must each contain width and height: %#v", fixture)
	}
	return fixture
}

func workspace(id uint64, name string) Workspace {
	key, err := sliceprotocol.NormalizeWorkspaceName(name)
	if err != nil {
		panic(err)
	}
	return Workspace{RuntimeID: id, Name: name, Key: key}
}

func observation(side Side, output sliceprotocol.Output, window []int) Observation {
	id := uint64(10)
	epoch := "11111111-1111-4111-8111-111111111111"
	if side == Leech {
		id = 20
		epoch = "22222222-2222-4222-8222-222222222222"
	}
	return Observation{
		Quality: Complete, SourceID: "source-1", SourceEpoch: epoch, RuntimeWindowID: id,
		Output: output, Workspace: workspace(1, "Dev"), Mode: Tiled,
		WindowWidth: window[0], WindowHeight: window[1], Order: &sliceprotocol.Position{Column: 1, Tile: 1},
	}
}

func inputFromFixture(t *testing.T, name string) Input {
	t.Helper()
	fixture := loadFixture(t, name)
	host := observation(Host, fixture.SourceOutput, fixture.SourceWindow)
	leech := observation(Leech, fixture.TargetOutput, fixture.TargetWindow)
	return Input{
		Mode: HostLocation, PreviousMode: HostLocation, ControllerID: "controller-1", Generation: 1,
		Host: host, Leech: &leech,
		HostWorkspaces: []Workspace{workspace(1, "Dev")}, LeechWorkspaces: []Workspace{workspace(1, "Dev")},
		Ownership: Ownership{SourceID: "source-1", HostCompositorEpoch: host.SourceEpoch, LeechCompositorEpoch: leech.SourceEpoch, HostRuntimeWindowID: 10, LeechRuntimeWindowID: 20, ProjectionPositivelyOwned: true},
	}
}

func change(result Result, kind ChangeKind) (Change, bool) {
	for _, proposal := range result.Proposals {
		for _, item := range proposal.Changes {
			if item.Kind == kind {
				return item, true
			}
		}
	}
	return Change{}, false
}

func conflict(result Result, code ConflictCode, property string) bool {
	for _, item := range result.Conflicts {
		if item.Code == code && (property == "" || item.Property == property) {
			return true
		}
	}
	return false
}

func TestEqualResolutionProjectionUsesPercentagesAndNonDisruptiveProposal(t *testing.T) {
	fixture := loadFixture(t, "equal-resolution.json")
	input := inputFromFixture(t, "equal-resolution.json")
	result := Plan(input)
	if result.Status != PlanComplete || len(result.Proposals) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	width, ok := change(result, ChangeWidth)
	if !ok || width.Percent != fixture.ExpectedPercent[0] {
		t.Fatalf("width = %#v", width)
	}
	height, ok := change(result, ChangeHeight)
	if !ok || height.Percent != fixture.ExpectedPercent[1] {
		t.Fatalf("height = %#v", height)
	}
	proposal := result.Proposals[0]
	if proposal.Target != Leech || proposal.TargetCompositorEpoch != input.Leech.SourceEpoch || proposal.RuntimeWindowID != 20 || proposal.Focus || !proposal.VerifyAfterWrite {
		t.Fatalf("unsafe proposal: %#v", proposal)
	}
	if err := proposal.ValidateNonDisruptive(); err != nil {
		t.Fatal(err)
	}
	if !result.Fidelity.Approximate || !slices.Contains(result.Fidelity.Reasons, "niri_working_area_unobservable") || slices.Contains(result.Fidelity.Reasons, "logical_dimensions_differ") {
		t.Fatalf("fidelity = %#v", result.Fidelity)
	}
}

func TestDifferingResolutionPreservesProportionAndReportsApproximation(t *testing.T) {
	fixture := loadFixture(t, "differing-resolution.json")
	input := inputFromFixture(t, "differing-resolution.json")
	result := Plan(input)
	width, _ := change(result, ChangeWidth)
	height, _ := change(result, ChangeHeight)
	if width.Percent != fixture.ExpectedPercent[0] || height.Percent != fixture.ExpectedPercent[1] {
		t.Fatalf("mapped percentages = %v/%v", width.Percent, height.Percent)
	}
	for _, reason := range []string{"logical_dimensions_differ", "output_scale_differs", "niri_working_area_unobservable", "terminal_cell_grid_may_differ"} {
		if !slices.Contains(result.Fidelity.Reasons, reason) {
			t.Fatalf("missing fidelity reason %q: %#v", reason, result.Fidelity)
		}
	}
	if result.Fidelity.Source.LogicalWidth != 1920 || result.Fidelity.Target.LogicalWidth != 1440 || result.Fidelity.Target.Scale != 1.5 {
		t.Fatalf("logical metadata lost: %#v", result.Fidelity)
	}
}

func TestResolutionFixtureVectorsAreWidthHeightPairs(t *testing.T) {
	for _, name := range []string{"equal-resolution.json", "differing-resolution.json"} {
		fixture := loadFixture(t, name)
		if len(fixture.SourceWindow) != 2 || len(fixture.TargetWindow) != 2 || len(fixture.ExpectedPercent) != 2 {
			t.Fatalf("%s has malformed vectors: %#v", name, fixture)
		}
	}
}

func TestAnyExactScaleDifferenceIsReported(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Leech.Output.Scale = 1.005
	result := Plan(input)
	if !slices.Contains(result.Fidelity.Reasons, "output_scale_differs") {
		t.Fatalf("small exact scale difference was omitted: %#v", result.Fidelity)
	}
}

func TestWorkspaceEnsurePrecedesExactWindowMutation(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Host.Workspace = workspace(2, "Ops")
	input.HostWorkspaces = append(input.HostWorkspaces, workspace(2, "Ops"))
	result := Plan(input)
	if len(result.Proposals) != 1 || len(result.Proposals[0].Changes) != 1 {
		t.Fatalf("expected ensure-only proposal: %#v", result)
	}
	ensure := result.Proposals[0].Changes[0]
	if ensure.Kind != ChangeEnsureWorkspace || ensure.WorkspaceName != "Ops" || ensure.WorkspaceKey != "ops" {
		t.Fatalf("ensure = %#v", ensure)
	}
	if result.Proposals[0].Focus || !result.Proposals[0].VerifyAfterWrite {
		t.Fatal("workspace ensure must preserve focus and verify")
	}

	input.LeechWorkspaces = append(input.LeechWorkspaces, workspace(9, "Ops"))
	result = Plan(input)
	move, ok := change(result, ChangeWorkspace)
	if !ok || move.WorkspaceRuntimeID != 9 {
		t.Fatalf("exact target workspace ID not proposed: %#v", result)
	}
}

func TestWorkspaceDuplicatesAndCaseCollisionsAreConflicts(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.HostWorkspaces = append(input.HostWorkspaces, workspace(2, "Dev"))
	result := Plan(input)
	if result.Status != PlanConflict || !conflict(result, ConflictWorkspaceDuplicate, "workspace") || len(result.Proposals) != 0 {
		t.Fatalf("duplicate not rejected: %#v", result)
	}

	input = inputFromFixture(t, "equal-resolution.json")
	input.LeechWorkspaces = append(input.LeechWorkspaces, workspace(2, "ＤＥＶ"))
	result = Plan(input)
	if !conflict(result, ConflictWorkspaceCollision, "workspace") || len(result.Proposals) != 0 {
		t.Fatalf("normalization collision not rejected: %#v", result)
	}
}

func TestHostLocationConvergesAllSupportedLeechDivergence(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	baseline := Spatial{WorkspaceName: "Dev", WorkspaceKey: "dev", Mode: Tiled, WidthPercent: 50, HeightPercent: 50}
	input.Baseline = &baseline
	input.Leech.Workspace = workspace(2, "Other")
	input.LeechWorkspaces = append(input.LeechWorkspaces, workspace(2, "Other"))
	input.Leech.Mode, input.Leech.Order = Floating, nil
	input.Leech.WindowWidth = 1152
	input.Leech.WindowHeight = 756
	result := Plan(input)
	if result.Status != PlanComplete || len(result.Proposals) != 1 {
		t.Fatalf("supported divergence was not converged: %#v", result)
	}
	proposal := result.Proposals[0]
	if proposal.Target != Leech || proposal.Focus || !proposal.VerifyAfterWrite || len(proposal.Changes) != 4 {
		t.Fatalf("unsafe or incomplete convergence proposal: %#v", proposal)
	}
	for _, kind := range []ChangeKind{ChangeWorkspace, ChangeLayoutMode, ChangeWidth, ChangeHeight} {
		if _, ok := change(result, kind); !ok {
			t.Fatalf("missing %s correction: %#v", kind, result)
		}
	}
}

func TestModeSwitchAlwaysSeedsFromHostBeforeLeechAuthority(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Mode, input.PreviousMode = LeechLocation, HostLocation
	input.Leech.Mode = Floating
	input.Leech.Order = nil
	result := Plan(input)
	mode, ok := change(result, ChangeLayoutMode)
	if !ok || mode.Mode != Tiled || !result.ModeSwitchPending || result.Proposals[0].Target != Leech || result.Proposals[0].Origin.Cause != "mode_switch_seed_from_host" {
		t.Fatalf("non-deterministic switch: %#v", result)
	}
	if result.SuggestedBaseline == nil || result.SuggestedBaseline.Mode != Tiled {
		t.Fatalf("host baseline missing: %#v", result.SuggestedBaseline)
	}
}

func TestRollbackToHostLocationConvergesLeechFromHost(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Mode, input.PreviousMode = HostLocation, LeechLocation
	input.Leech.Mode = Floating
	input.Leech.Order = nil
	input.Baseline = &Spatial{WorkspaceName: "Dev", WorkspaceKey: "dev", Mode: Floating, WidthPercent: 40, HeightPercent: 40}
	result := Plan(input)
	mode, ok := change(result, ChangeLayoutMode)
	if !ok || mode.Mode != Tiled || result.Proposals[0].Target != Leech || !result.ModeSwitchPending {
		t.Fatalf("rollback did not restore host authority: %#v", result)
	}
}

func TestAuthorizedLeechLocationWritesExactHostWindowWithOrigin(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Mode, input.PreviousMode = LeechLocation, LeechLocation
	input.LeechWriteAuthorized = true
	baseline := Spatial{WorkspaceName: "Dev", WorkspaceKey: "dev", Mode: Tiled, WidthPercent: 50, HeightPercent: 50, Order: &sliceprotocol.Position{Column: 1, Tile: 1}}
	input.Baseline = &baseline
	input.Leech.Mode = Floating
	input.Leech.Order = nil
	input.Leech.WindowWidth = 1152 // 60%
	input.Leech.WindowHeight = 756 // 70%
	input.Leech.Output.LogicalWidth = 1920
	input.Leech.Output.LogicalHeight = 1080
	input.LeechWorkspaces = append(input.LeechWorkspaces, workspace(8, "Ops"))
	input.Leech.Workspace = workspace(8, "Ops")
	input.HostWorkspaces = append(input.HostWorkspaces, workspace(7, "Ops"))

	result := Plan(input)
	if result.Status != PlanComplete || len(result.Proposals) != 1 {
		t.Fatalf("writeback failed: %#v", result)
	}
	proposal := result.Proposals[0]
	if proposal.Target != Host || proposal.TargetCompositorEpoch != input.Host.SourceEpoch || proposal.RuntimeWindowID != 10 || proposal.Origin.From != Leech || proposal.Origin.Mode != LeechLocation || proposal.Focus || !proposal.VerifyAfterWrite {
		t.Fatalf("writeback boundary = %#v", proposal)
	}
	if move, ok := change(result, ChangeWorkspace); !ok || move.WorkspaceRuntimeID != 7 {
		t.Fatalf("host workspace exact ID absent: %#v", result)
	}
	if mode, ok := change(result, ChangeLayoutMode); !ok || mode.Mode != Floating {
		t.Fatalf("floating write absent: %#v", result)
	}
	if width, ok := change(result, ChangeWidth); !ok || width.Percent != 60 {
		t.Fatalf("width write absent: %#v", result)
	}
	if height, ok := change(result, ChangeHeight); !ok || height.Percent != 70 {
		t.Fatalf("height write absent: %#v", result)
	}
}

func TestLeechWritebackRequiresAuthorizationAndExactOwnership(t *testing.T) {
	makeInput := func() Input {
		input := inputFromFixture(t, "equal-resolution.json")
		input.Mode, input.PreviousMode = LeechLocation, LeechLocation
		baseline := Spatial{WorkspaceName: "Dev", WorkspaceKey: "dev", Mode: Tiled, WidthPercent: 50, HeightPercent: 50}
		input.Baseline = &baseline
		input.Leech.Mode, input.Leech.Order = Floating, nil
		return input
	}
	input := makeInput()
	result := Plan(input)
	if !conflict(result, ConflictWriteNotAuthorized, "mode") || len(result.Proposals) != 0 {
		t.Fatalf("unauthorized write accepted: %#v", result)
	}
	input = makeInput()
	input.LeechWriteAuthorized = true
	input.Ownership.SourceID = "unrelated-source"
	result = Plan(input)
	if !conflict(result, ConflictOwnership, "source_id") || len(result.Proposals) != 0 {
		t.Fatalf("unrelated ownership accepted: %#v", result)
	}
	input = makeInput()
	input.LeechWriteAuthorized = true
	input.Ownership.ProjectionPositivelyOwned = false
	result = Plan(input)
	if !conflict(result, ConflictOwnership, "source_id") || len(result.Proposals) != 0 {
		t.Fatalf("title-like ownership accepted: %#v", result)
	}
}

func TestConcurrentPropertyConflictIsReportedWithoutOverwritingThatProperty(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Mode, input.PreviousMode, input.LeechWriteAuthorized = LeechLocation, LeechLocation, true
	baseline := Spatial{WorkspaceName: "Dev", WorkspaceKey: "dev", Mode: Tiled, WidthPercent: 50, HeightPercent: 50}
	input.Baseline = &baseline
	input.Host.WindowWidth = 1152  // 60%
	input.Leech.WindowWidth = 1344 // 70%
	input.Leech.WindowHeight = 756 // 70%, leech-only change remains writable
	result := Plan(input)
	if result.Status != PlanConflict || !conflict(result, ConflictConcurrentProperty, "width_percent") {
		t.Fatalf("concurrent conflict absent: %#v", result)
	}
	if _, ok := change(result, ChangeWidth); ok {
		t.Fatal("conflicted width was overwritten")
	}
	if height, ok := change(result, ChangeHeight); !ok || height.Percent != 70 {
		t.Fatalf("independent property did not converge: %#v", result)
	}
}

func hostTargetInput(t *testing.T) Input {
	t.Helper()
	input := inputFromFixture(t, "equal-resolution.json")
	input.Mode, input.PreviousMode, input.LeechWriteAuthorized = LeechLocation, LeechLocation, true
	input.Baseline = &Spatial{WorkspaceName: "Dev", WorkspaceKey: "dev", Mode: Tiled, WidthPercent: 50, HeightPercent: 50}
	input.Leech.Mode, input.Leech.Order = Floating, nil
	return input
}

func TestOriginSuppressionUsesOnlyControllerGenerationAndTarget(t *testing.T) {
	tests := []struct {
		name   string
		input  func(*testing.T) Input
		target Side
	}{
		{"leech target", func(t *testing.T) Input { return inputFromFixture(t, "equal-resolution.json") }, Leech},
		{"host target", hostTargetInput, Host},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := test.input(t)
			// Diagnostic fields deliberately disagree with the new proposal.
			// The exact controller/generation/target triple still suppresses it.
			input.LastApplied = &AppliedWrite{Target: test.target, Origin: Origin{ControllerID: input.ControllerID, Generation: input.Generation, From: test.target, Mode: "diagnostic-mismatch", Cause: "diagnostic-mismatch"}}
			result := Plan(input)
			if result.Status != PlanConflict || !conflict(result, ConflictWriteAwaitingVerify, "origin") || len(result.Proposals) != 0 {
				t.Fatalf("same triple bypassed suppression: %#v", result)
			}
		})
	}
}

func TestStaleOrMismatchedOriginsFailClosedForBothTargets(t *testing.T) {
	targets := []struct {
		name   string
		input  func(*testing.T) Input
		target Side
	}{
		{"leech target", func(t *testing.T) Input { return inputFromFixture(t, "equal-resolution.json") }, Leech},
		{"host target", hostTargetInput, Host},
	}
	for _, target := range targets {
		t.Run(target.name+" controller", func(t *testing.T) {
			input := target.input(t)
			input.LastApplied = &AppliedWrite{Target: target.target, Origin: Origin{ControllerID: "stale-controller", Generation: input.Generation}}
			result := Plan(input)
			if result.Status != PlanConflict || !conflict(result, ConflictOriginControllerMismatch, "origin") || len(result.Proposals) != 0 {
				t.Fatalf("mismatched controller did not fail closed: %#v", result)
			}
		})
		for _, generation := range []uint64{1, 3} {
			t.Run(target.name+" generation", func(t *testing.T) {
				input := target.input(t)
				input.Generation = 2
				input.LastApplied = &AppliedWrite{Target: target.target, Origin: Origin{ControllerID: input.ControllerID, Generation: generation}}
				result := Plan(input)
				if result.Status != PlanConflict || !conflict(result, ConflictOriginGenerationMismatch, "origin") || len(result.Proposals) != 0 {
					t.Fatalf("generation %d did not fail closed: %#v", generation, result)
				}
			})
		}
	}
}

func TestMismatchedPendingOriginFailsClosedEvenWhenSpatiallyConverged(t *testing.T) {
	leechTarget := inputFromFixture(t, "equal-resolution.json")
	leechTarget.Leech.WindowWidth, leechTarget.Leech.WindowHeight = leechTarget.Host.WindowWidth, leechTarget.Host.WindowHeight
	leechTarget.LastApplied = &AppliedWrite{Target: Leech, Origin: Origin{ControllerID: "stale", Generation: leechTarget.Generation}}
	if result := Plan(leechTarget); !conflict(result, ConflictOriginControllerMismatch, "origin") || len(result.Proposals) != 0 {
		t.Fatalf("converged leech target ignored mismatched origin: %#v", result)
	}

	hostTarget := hostTargetInput(t)
	hostTarget.Leech.Mode, hostTarget.Leech.Order = Tiled, &sliceprotocol.Position{Column: 1, Tile: 1}
	hostTarget.Leech.WindowWidth, hostTarget.Leech.WindowHeight = hostTarget.Host.WindowWidth, hostTarget.Host.WindowHeight
	hostTarget.LastApplied = &AppliedWrite{Target: Host, Origin: Origin{ControllerID: hostTarget.ControllerID, Generation: hostTarget.Generation + 1}}
	if result := Plan(hostTarget); !conflict(result, ConflictOriginGenerationMismatch, "origin") || len(result.Proposals) != 0 {
		t.Fatalf("converged host target ignored mismatched origin: %#v", result)
	}
}

func TestOwnershipAndExactWindowProposalsAreBoundToCompositorEpoch(t *testing.T) {
	t.Run("leech numeric ID reuse", func(t *testing.T) {
		input := inputFromFixture(t, "equal-resolution.json")
		input.Leech.SourceEpoch = "33333333-3333-4333-8333-333333333333"
		result := Plan(input)
		if !conflict(result, ConflictOwnership, "source_id") || len(result.Proposals) != 0 {
			t.Fatalf("stale leech ownership authorized reused ID: %#v", result)
		}
		input.Ownership.LeechCompositorEpoch = input.Leech.SourceEpoch
		result = Plan(input)
		if len(result.Proposals) != 1 || result.Proposals[0].TargetCompositorEpoch != input.Leech.SourceEpoch {
			t.Fatalf("renewed leech ownership did not bind proposal epoch: %#v", result)
		}
	})
	t.Run("host numeric ID reuse", func(t *testing.T) {
		input := hostTargetInput(t)
		input.Host.SourceEpoch = "44444444-4444-4444-8444-444444444444"
		result := Plan(input)
		if !conflict(result, ConflictOwnership, "source_id") || len(result.Proposals) != 0 {
			t.Fatalf("stale host ownership authorized reused ID: %#v", result)
		}
		input.Ownership.HostCompositorEpoch = input.Host.SourceEpoch
		result = Plan(input)
		if len(result.Proposals) != 1 || result.Proposals[0].TargetCompositorEpoch != input.Host.SourceEpoch {
			t.Fatalf("renewed host ownership did not bind proposal epoch: %#v", result)
		}
	})
}

func TestDegradedObservationsNeverProposeMutation(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Host.Quality = Degraded
	result := Plan(input)
	if result.Status != PlanDegraded || !conflict(result, ConflictIncompleteObservation, "") || len(result.Proposals) != 0 {
		t.Fatalf("degraded host authorized mutation: %#v", result)
	}
	input = inputFromFixture(t, "equal-resolution.json")
	input.Leech.Quality = Degraded
	result = Plan(input)
	if result.Status != PlanDegraded || len(result.Proposals) != 0 {
		t.Fatalf("degraded leech authorized mutation: %#v", result)
	}
}

func TestInitialProjectionCarriesWorkspaceModeAndProportionsOnly(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Leech = nil
	input.Ownership = Ownership{}
	result := Plan(input)
	if len(result.Proposals) != 1 || len(result.Proposals[0].Changes) != 1 {
		t.Fatalf("initial projection = %#v", result)
	}
	initial := result.Proposals[0].Changes[0]
	if initial.Kind != ChangeInitialProjection || initial.WorkspaceKey != "dev" || initial.Mode != Tiled || initial.WidthPercent != 50 || initial.HeightPercent != 50 {
		t.Fatalf("initial spatial intent = %#v", initial)
	}
	if err := result.Proposals[0].ValidateNonDisruptive(); err != nil {
		t.Fatal(err)
	}
}

func TestFloatingProjectionHasNoTiledOrder(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Host.Mode, input.Host.Order = Floating, nil
	result := Plan(input)
	mode, ok := change(result, ChangeLayoutMode)
	if !ok || mode.Mode != Floating || len(result.OrderDrift) == 0 {
		t.Fatalf("floating projection/result = %#v", result)
	}
}

func TestOrderIsPreservedInitiallyAndDriftIsReportOnly(t *testing.T) {
	items := []OrderItem{
		{SourceID: "c", Position: nil},
		{SourceID: "b", Position: &sliceprotocol.Position{Column: 1, Tile: 2}},
		{SourceID: "a", Position: &sliceprotocol.Position{Column: 1, Tile: 1}},
	}
	ordered := InitialLaunchOrder(items)
	if got := []string{ordered[0].SourceID, ordered[1].SourceID, ordered[2].SourceID}; !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("initial order = %v", got)
	}
	drift := CompareOrder(ordered[:2], []OrderItem{{SourceID: "a", Position: &sliceprotocol.Position{Column: 2, Tile: 1}}, {SourceID: "b", Position: &sliceprotocol.Position{Column: 1, Tile: 2}}})
	if len(drift) != 1 || drift[0].SourceID != "a" {
		t.Fatalf("drift = %#v", drift)
	}
	input := inputFromFixture(t, "equal-resolution.json")
	input.Leech.Order = &sliceprotocol.Position{Column: 2, Tile: 1}
	result := Plan(input)
	if len(result.OrderDrift) != 1 {
		t.Fatalf("planner did not report order drift: %#v", result)
	}
	for _, proposal := range result.Proposals {
		for _, item := range proposal.Changes {
			if item.Kind != ChangeWidth && item.Kind != ChangeHeight {
				t.Fatalf("order drift generated correction: %#v", proposal)
			}
		}
	}
}

func TestInvalidTopologyAndRuntimeIdentityFailClosed(t *testing.T) {
	input := inputFromFixture(t, "equal-resolution.json")
	input.Host.Output.LogicalWidth = 0
	result := Plan(input)
	if result.Status != PlanConflict || len(result.Proposals) != 0 {
		t.Fatalf("invalid output accepted: %#v", result)
	}
	input = inputFromFixture(t, "equal-resolution.json")
	input.Host.RuntimeWindowID = 0
	result = Plan(input)
	if result.Status != PlanConflict || len(result.Proposals) != 0 {
		t.Fatalf("cross-epoch/missing runtime ID accepted: %#v", result)
	}
}

func TestProposalValidationRejectsFocusUnscopedAndEpochlessMutations(t *testing.T) {
	proposal := Proposal{Target: Leech, SourceID: "source", Origin: Origin{ControllerID: "controller", Generation: 1}, VerifyAfterWrite: true, Focus: true, RuntimeWindowID: 2, Changes: []Change{{Kind: ChangeWorkspace, WorkspaceRuntimeID: 3}}}
	if proposal.ValidateNonDisruptive() == nil {
		t.Fatal("focus-changing proposal accepted")
	}
	proposal.Focus = false
	if proposal.ValidateNonDisruptive() == nil {
		t.Fatal("exact-window proposal without target epoch accepted")
	}
	proposal.TargetCompositorEpoch = "22222222-2222-4222-8222-222222222222"
	if err := proposal.ValidateNonDisruptive(); err != nil {
		t.Fatalf("epoch-bound exact-window proposal rejected: %v", err)
	}
	proposal.RuntimeWindowID = 0
	if proposal.ValidateNonDisruptive() == nil {
		t.Fatal("unscoped window mutation accepted")
	}
}
