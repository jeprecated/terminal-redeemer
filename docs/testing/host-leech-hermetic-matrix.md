# Host/leech hermetic acceptance matrix

This matrix is the executable acceptance index for the host/leech terminal-slice
MVP. It runs without a desktop, network, credentials, or pre-existing private
state. Compositor, transport, process, clock, and filesystem evidence comes from
fixtures, fakes, deterministic clocks, subprocess helpers, or temporary
owner-only directories.

Run the Go matrix locally with an explicit current-user cache (or from a tree
with a populated `vendor/` directory):

```console
HOST_LEECH_GOMODCACHE="$(go env GOMODCACHE)" \
HOST_LEECH_GOCACHE="$(go env GOCACHE)" \
  scripts/tests/host-leech-hermetic-matrix.sh
```

The runner rejects non-absolute, symlinked, non-directory, or non-owned cache
paths, creates and trap-cleans a private temporary HOME, and unsets all XDG and
graphical runtime inputs. The distinct packaged-process acceptance check is:

```console
nix build .#checks.x86_64-linux.host-leech-subprocess-acceptance -L
```

For a local run, both dependencies must be supplied explicitly; the test skips
rather than silently substituting `go run` or an ambient executable:

```console
REDEEM_BIN=/absolute/path/to/packaged/redeem \
ZELLIJ_BIN=/absolute/path/to/zellij-0.43.1 \
  go test -count=1 -v ./internal/subprocessacceptance
```

`nix flake check` additionally runs the same matrix in the Nix sandbox with a
2,000-event deterministic soak, risk-family coverage report, the flake-locked
Zellij 0.43.1 executable spike, and the pinned Niri 25.11 `--contract` spike.
The Niri contract mode validates the checked-in IPC replay/state/action fixtures
and exact action shapes without starting a compositor; the script's default
mode remains the live nested-compositor operator gate. The matrix must not read
a live `NIRI_SOCKET`, SSH agent, Zellij environment, or user cache.

The bounded soak can be run alone. It writes exactly one strict, secret-free
JSON summary to stdout:

```console
scripts/tests/host-leech-soak.sh --iterations 2000
```

The explicit longer validation run is:

```console
scripts/tests/host-leech-soak.sh --iterations 10000 > host-leech-soak-summary.json
```

The summary contains only counters, configured limits, resource deltas, and the
fixed public seed. It contains no paths, tokens, source/session identities,
environment, credentials, or private state. The test drives complete,
degraded, duplicate, stale, conflicting, and retired-epoch replay observations;
session/source churn; selection, pickup, drop/reopen, reconnect, intents,
controller restart, prepared socket/cache churn, and bounded helper children.
It asserts persisted state/tombstone/audit/undo caps, stable recovery budgets,
no simultaneous duplicate projection, no host-target effect, and zero remaining
owned temporary resources.

Coverage is recorded by risk family and named critical functions rather than by
a superficial repository-wide percentage:

```console
python3 scripts/tests/host-leech-coverage.py \
  --output docs/testing/host-leech-coverage-baseline.json
```

The checked-in baseline covers protocol acceptance, inventory authority,
controller lifecycle, host/leech routed journals, exact attachment, transport
typing, and spatial policy. Baseline
`host-leech-v1-2026-07-26.2` is byte-for-byte enforced by the hermetic/Nix
matrix: any value, function, package, policy, or serialization drift fails until
an explicitly versioned baseline is regenerated and reviewed. It remains a
risk-local attestation, not a superficial global threshold or sole safety gate.

The version-1 review compared the original pre-model/fuzz baseline with the
final hardened tree. Protocol statement coverage decreased from 69.0% to 65.7%
because bounded encoding and recursive exact-field rejection added defensive
production branches; the named `Decode` path improved from 81.0% to 85.7% and
duplicate/hash paths did not regress. Inventory `Write` moved from 76.7% to
75.0% because no-follow bounded safefile and exact-field failures expanded that
path, while the inventory family increased to 80.1% and `Build` to 93.5%.
Routed-host `Update` moved from 61.8% to 61.1% after journal validation/IO
hardening, while that family increased to 68.7%. Controller, routed-leech, and
transport families increased to 70.2%, 70.5%, and 68.9%; exact attachment and
spatial policy remained stable. Baseline `.2` raises routed-Leech aggregate
coverage from 70.0% to 70.5% after adding explicit already-expired and
sleep-crosses-deadline regressions; no risk family decreased from `.1`. These reviewed decreases are expanded
fail-closed denominator branches, not removed tests, and are locked exactly by
the new baseline rather than hidden by a tolerance.

Evidence layers remain distinct:

1. ordinary unit/integration tests prove focused behavior;
2. deterministic stateful model tests compare long generated sequences with an
   independent reference model;
3. native Go fuzz targets exercise hostile wire and persisted-state boundaries;
4. packaged subprocess and crash tests cross real process/state/socket seams;
5. locked Niri/Zellij checks prove pinned component contracts;
6. deterministic soak tests detect bounded-growth, restart, retry, and leak
   failures; and
7. the credentialed physical two-machine smoke remains mandatory before
   activation.

`scripts/tests/host-leech-layer-smoke.sh --require` discovers and runs every
native `TestModel...` suite and every `Fuzz...` target. Each fuzz invocation uses
a clean temporary Go cache and a bounded one-second, single-worker campaign, so
Go consumes the complete checked-in seed corpus and then executes generated
mutations rather than stopping partway through baseline gathering. The cache is
trap-removed. The hermetic matrix and Nix check use `--require`, so removing
either evidence layer fails acceptance instead of silently reporting it missing.

For a longer fuzz run, keep the Go cache temporary and run each
native target separately (Go permits only one matching fuzz target per run):

```console
cache="$(mktemp -d)"; trap 'rm -rf "$cache"' EXIT
export GOCACHE="$cache" GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
while read -r file target; do
  go test -run '^$' -fuzz "^${target}$" -fuzztime=30s "./$(dirname "$file")"
done < <(grep -RH --include='*_test.go' -E '^func Fuzz' internal \
  | sed -E 's#^([^:]+):func (Fuzz[A-Za-z0-9_]+)\(.*#\1 \2#' | sort)
```

Focused race-enabled fuzzing is valid for these hermetic targets, for example:

```console
GOCACHE="$(mktemp -d)" go test -race -run '^$' \
  -fuzz '^FuzzInventoryEnvelope$' -fuzztime=10s ./internal/sliceprotocol
```

Ordinary `go test` executes every checked-in seed corpus deterministically.
Generated fuzz cache/corpus output is not repository evidence and must not be
committed. Promote any reproducer to a small named `f.Add` regression seed and
focused unit test, then remove generated `testdata/fuzz` output.

## Traceability

| Contract | Hermetic evidence |
| --- | --- |
| Zero, one, and many eligible terminals; headless exclusion; one-window/one-session and duplicate conflicts | `sourceinventory.TestInventoryCardinalityMatrix`; `sourceinventory.TestBuilderOneWindowTwoSessionCandidatesIsAmbiguousConflict`; `sourceinventory.TestBuilderEligibleConflictDuplicateAndHeadlessScope`; `zellijlive.TestCommandCatalogClassifiesActiveDeadPrefixAndNeverAttaches`; `zellijlive.TestCommandCatalogRejectsDuplicateListing`; `zellijlive.TestConflictingEnvironmentAndArgvRemainAmbiguous` |
| Missing, dead/resurrectable, ambiguous-prefix, and exact active catalog taxonomy | `zellijlive.TestCommandCatalogClassifiesActiveDeadPrefixAndNeverAttaches`; `zellijlive.TestCommandCatalogFailureSingletonAndScannerTaxonomy`; `zellijlive.TestCommandCatalogPropagatesDeadSessionCatalogReadFailures` |
| Exact live-only attachment: hard link, empty cache, scrub, detach, path bounds, typed exits, stale GC, routed-host exact-disappearance isolation, no prefix/resurrection, and host-session survival | `sliceattach.TestExactAttachHardLinksScrubsAndDetaches`; `sliceattach.TestAttachRejectsPathBudgetAndNonemptyCache`; `sliceattach.TestAttachMapsProvablyStaleSocketToUnavailable`; `sliceattach.TestAttachGarbageCollectsOnlyOldOwnedPrefixDirectories`; `sliceattach.TestAttachRejectsSymlinkedOrUnmarkedPrivateRootWithoutGC`; `slicerpc.TestDirectHostTransactionExactSocketDisappearanceRetainsIsolatedNamespaceWithoutPrefixFallback`; `slicerpc.TestDirectHostTransactionSourceProofBeforeDelayedHelperConnectRetainsNamespaceUntilExit`; `slicerpc.TestPreparedNamespaceIdentitySurvivesCrashReplayUntilCommitProof`; `sliceattach.TestPreparedWrapperOwnsNamespaceUntilPinnedClientExit`; `sliceattach.TestPreparedWrapperRefusesJournalIdentityMismatchWithoutStarting`; `checks.zellij-live-only-attachment-spike` and `checks.host-leech-hermetic-matrix` |
| Pinned Niri 25.11 IPC contract, initial replay through explicit `ConfigLoaded.failed == false`, separate Outputs join, transient inconsistency, and adversarially checked exact workspace/window references, non-focus move, tiled/floating actions, and bounded width/height actions without a compositor | `checks.niri-direct-ipc-contract-spike`; `checks.host-leech-hermetic-matrix`; `niriipc.TestClientInitialReplayAndSeparateOutputs`; `niriipc.TestClientTransientDanglingReplayConvergesBeforeConfigLoaded`; `niriipc.TestInitialReplayFailureCodes`; `niriipc.TestValidateRejectsDanglingGeometryAndTopology`; `niri.TestNiriIPCInitialReplayFixtureEndsAtConfigLoaded`; `niri.TestNiriIPCCompleteAndDanglingStateFixtures`; `niri.TestNiriIPCMVPActionFixtureUsesExactBoundedShapes` |
| Source epoch rotation, per-complete-poll revisions, degraded retention, stale/out-of-order/duplicate replay and full resync | `sourceinventory.TestPublisherRevisionDegradedRetentionEpochRotationAndRuntimeReuse`; `sliceprotocol.TestAcceptorOrderingEpochAndReceiveTimeFreshness`; `sliceprotocol.TestDegradedPreservesAuthority`; `sliceprotocol.TestRetiredEpochTombstonesAreExactBoundedAndFailClosedAtCapacity`; `slicecontroller.TestEpochLineageAndExhaustedSuccessorGate` |
| Workspace selection, pickup/drop, close/reopen/undo, dynamic add/move/remove, repeated revision and restart | `slicecontroller.TestDesiredSelectionDuplicateRevisionCloseReopenUndo`; `slicecontroller.TestDynamicSelectionPickupMoveDropAndRemovalMatrix`; `slicecontroller.TestRetryBudgetPersistsExhaustsAndExplicitReconnect`; `slicecontroller.TestStartupCreatesAndExhaustsBoundedRecoveryForOrdinaryConnectedState` |
| Healthy manual close versus disconnect; bounded recovery, successful in-window reconnect, exhaustion, explicit reconnect | `slicecontroller.TestManualLocalDisappearanceBecomesClosedByUser`; `slicecontroller.TestDegradedHostCannotTurnLocalLossIntoManualClose`; `slicecontroller.TestAttachmentLossCommitsRecoveryAndCloseBeforeRetry`; `slicecontroller.TestRecoverySucceedsInsideOriginalPersistedWindowWithoutBudgetReset`; `slicecontroller.TestRetryBudgetPersistsExhaustsAndExplicitReconnect`; `slicecontroller.TestReadinessRelayRejectsAuthStallAndWrongMarker` |
| Degraded/partial evidence is non-destructive; session-keyed drops survive source/epoch replacement but cannot suppress a different verified session; expiry requires consecutive complete live-session absence plus grace | `slicecontroller.TestPublisherAndControllerExpireStableAbsentSessionOnlyAfterConfirmationsAndGrace`; `slicecontroller.TestSessionDropExpiryRequiresConsecutiveCompleteAbsenceAndGrace`; `slicecontroller.TestSessionDropPresenceResetsEvidenceAndRejectedObservationsDoNotAdvance`; `slicecontroller.TestSessionDropRetiredEpochReplayAndSameRevisionConflictPreserveAllEvidence`; `slicecontroller.TestSessionDropSurvivesSourceAndEpochReplacementAndHeadlessPresence`; `slicecontroller.TestDifferentVerifiedSessionIsWantedDespiteExistingDrop`; `slicecontroller.TestIncompleteLocalProcessEvidenceCannotCloseProjection` |
| Hostile JSON, UTF-8, argv, title, CWD, session, identity, revision, app ID, journals, and state | `sliceprotocol.FuzzInventoryEnvelope`; `sliceprotocol.FuzzCanonicalHashing`; `sliceprotocol.FuzzWorkspaceNormalization`; `sliceprotocol.FuzzDuplicateAndTruncatedJSON`; `slicerpc.FuzzRPCRequest`; `slicerpc.FuzzRPCPayload`; `slicetransport.FuzzRPCResponse`; `slicecontroller.FuzzControlRequestAndResponse`; `slicecontroller.FuzzControllerStateStore`; `slicecontroller.FuzzProjectionArgv`; `sourceinventory.FuzzSourceInventoryStore`; `slicerpc.FuzzHostTokenJournal`; `slicerpc.FuzzTokenRecordValidation`; `slicelaunch.FuzzRoutedIntentJournal`; `slicelaunch.FuzzRoutedIntentValidation`; `zellijlive.FuzzProcessArgvEnvironmentMetadata`; `sliceenv.FuzzProcessEnvironmentMetadata`; plus the focused hostile-boundary unit tests |
| Close/drop/status act only on positively owned local windows and never terminate remote work | `redeem.TestExecuteSliceControllerCloseDropEffectsRequireExactOwnershipAndStayLocal`; `slicecontroller.TestPositiveOwnershipIgnoresTitlesAndRejectsPIDOrArgvMismatch`; `slicecontroller.TestFocusedFallbackReprovesAfterLockAndRejectsReusedID`; `slicecontroller.TestLeechSpatialReproofRejectsReusedIDWithoutAction`; `mirror.TestOwnedWindowFilteringAndCloseDryRun` |
| One-monitor equal/different resolution, workspace creation/collision, membership, tiled/floating, proportional size, launch order and drift-only reporting | `slicelayout.TestEqualResolutionProjectionUsesPercentagesAndNonDisruptiveProposal`; `slicelayout.TestDifferingResolutionPreservesProportionAndReportsApproximation`; `slicelayout.TestWorkspaceEnsurePrecedesExactWindowMutation`; `slicelayout.TestWorkspaceDuplicatesAndCaseCollisionsAreConflicts`; `slicelayout.TestInitialProjectionCarriesWorkspaceModeAndProportionsOnly`; `slicelayout.TestFloatingProjectionHasNoTiledOrder`; `slicelayout.TestOrderIsPreservedInitiallyAndDriftIsReportOnly`; `slicerpc.TestWorkspaceEnsureRevalidatesCollisionAfterWrite` |
| V1 host-location workspace/floating/width/height convergence and rollback, schema-2/config/HM rejection of leech-location or writeback, exact host RPC reproof, and order-only drift | `slicelayout.TestHostLocationConvergesAllSupportedLeechDivergence`; `slicelayout.TestRollbackToHostLocationConvergesLeechFromHost`; `slicelayout.TestOrderIsPreservedInitiallyAndDriftIsReportOnly`; `config.TestSliceControllerValidation`; `slicecontroller.TestStoreRejectsOldExperimentalControllerAuthorityWithoutMigration`; `slicecontroller.TestV1RejectsLeechAuthorityAndHostTargetSpatialProposal`; `slicecontroller.TestSpatialFailureClearsOriginRetriesBoundedlyAndRollsBack`; `redeem.TestSliceRPCProductionRejectsSpatialApplyBeforeNiriAccess`; `redeem.TestHostSpatialRPCRechecksExactSourceAndVerifies`; `checks.hm-module-rejects-leech-location`; `checks.hm-module-rejects-leech-write-authorized` |
| Routed launch mode off/unselected, one token/host terminal, clean replay, typed metadata/conflict rejection through production transport, lost success/partial creation/readiness delay, exact handoff adoption, exhaustion/reconnect/cancel/definitive absence, crash boundaries, no fallback | `slicelaunch.TestModeOffAndUnselectedUseOnlyUnchangedLocalBoundary`; `slicelaunch.TestFirstSuccessPersistsStableIntentAndHandoff`; `slicelaunch.TestLostResponseReplaysSameTokenWithoutLocalFallback`; `slicelaunch.TestDeterministicRoutedLaunchRejectionsAreTerminalWithoutFallback`; `slicelaunch.TestExhaustionThenExplicitReconnectUsesSameIntent`; `slicelaunch.TestCancellationPersistsPendingAndNeverFallsBack`; `slicelaunch.TestDefiniteHostAbsenceBeforeCreationFailsWithoutFallback`; `slicelaunch.TestDefiniteHostNonCreationFailsAndNeverFallsBack`; `slicetransport.TestServerDeterministicLaunchRejectionsTraverseClientWithoutAmbiguity`; `slicerpc.TestHostTransactionCommitsExactIdentityAndReplaysWithoutEffects`; `slicerpc.TestHostTransactionPendingResumesSameSessionAfterAmbiguity`; `slicerpc.TestDirectHostTransactionNeverRepeatsUncertainCreateOrKittyStart`; `slicerpc.TestDirectHostTransactionSourceProofBeforeDelayedHelperConnectRetainsNamespaceUntilExit`; `slicerpc.TestPreparedNamespaceIdentitySurvivesCrashReplayUntilCommitProof`; `slicerpc.TestPendingCreationCrashBoundariesNeverExposeTornFinalRecord`; `slicecontroller.TestCrashWindowReAdoptsOnlyUniqueExactProjection`; `slicecontroller.TestLaunchedHandoffExplicitReplayRestartsDisconnectedExactSource`; `slicecontroller.TestLaunchedHandoffMismatchedAuthorityRemainsPendingWithoutReconnect`; `slicecontroller.TestLaunchHandoffTransitionsAreMonotonic`; `slicecontroller.TestStartupCreatesAndExhaustsBoundedRecoveryForOrdinaryConnectedState`; `sliceattach.TestReadinessMarkerRequiresBoundedInteractiveSurvival` |
| Packaged two-node subprocess boundaries: isolated roots, direct CLI enrolment/refusal, complete real-Zellij inventory, Niri JSON-line replay/Outputs, shell-inert SSH to packaged host RPC, controller run/status/selection/restart, mode-off/unselected local launches, exact-session routed commit with delayed process publication and automatic projection `-tt` attach start, response loss armed before the selected packaged `slice launch` and recovered by its same-token router replay with monotonic handoff/effect count, routed cancellation/no-local-fallback with exact owned-process reap assertions, disablement persistence, socket-restart epoch rotation, and sentinel non-mutation | `subprocessacceptance.TestHermeticTwoNodePackagedSubprocessLifecycle`; `zellijlive.TestProcObserverFindsChildCreatedByNonLeaderThread`; `checks.host-leech-subprocess-acceptance` |
| Routed-launch durable crash partition and restart: exact pending/session-starting/session-created/socket-planned/kitty-prepared/kitty-starting/placed/proof-committed/committed fsync gates, pidfd-only responsible-process kill, shell-inert packaged `redeem --config <host> slice rpc` replay, complete authority/exact source tuple/final compositor-rotation proof, total crash-plus-replay token/session/socket/Kitty/placement/source/handoff/projection/fallback/sentinel cardinalities, effect-free committed replay, and seven distinct cancellation/delayed-child/ambiguous-post-start/delayed-inventory/definite-failure/response-loss/cleanup-crash outcomes | `subprocessacceptance.TestRoutedLaunchPackagedProcessCrashMatrix/{pending,session_starting,session_created,socket_planned,kitty_prepared,kitty_starting,placed,proof_committed,committed,cancellation,delayed_child_connect,ambiguous_post_start,delayed_inventory,definite_pre_start_failure,response_loss,cleanup_crash}`; `slicerpc.TestProofCommittedRequiresMatchingFinalProofBeforeCommit`; `slicerpc.TestTokenTransactionLockIsCloseOnExec`; `slicerpc.TestHostTransactionCommitsExactIdentityAndReplaysWithoutEffects`; `checks.host-leech-subprocess-acceptance` |
| Deterministic bounded soak: thousands of observation/churn events; controller and production Router/host-RPC retry across reconstructed intent/token stores; exact token/session/deadline/attempt restart persistence; one host session/Kitty/placement/source and one local projection effect across lost-response replay; every authority/state/argv cap including pending cleanup; exact tombstones; prepared namespace/cache lifecycle; helper child/fd/goroutine bounds; and no duplicate or host-target effects | `hostleechsoak.TestBoundedHostLeechSoak`; `scripts/tests/host-leech-soak.sh`; `scripts/tests/host-leech-layer-smoke.sh`; `checks.host-leech-hermetic-matrix`; `docs/testing/host-leech-coverage-baseline.json` |
| Legacy one-shot attach compatibility and pinned watch unsupported | `mirror.TestPlanLaunchAttachMetadataAndWatchUnsupported`; `mirror.TestOwnedWindowFilteringAndCloseDryRun`; `mirror.TestLegacySnapshotFixtureRemainsUnversionedAndSeparate`; packaged CLI tests in `cmd/redeem` |

Hostile working-directory strings remain legacy one-shot metadata and are tested
as discrete argv rather than shell text; they are not a slice RPC field or part
of routed launch authority. Slice tests assert clean allowlisted environments and
exact complete argv. Window titles likewise remain observation data only and are
explicitly excluded from ownership and persisted controller state.

## Non-hermetic operator smoke gate

These checks are deliberately separate from repository acceptance and require a
disposable host/leech setup:

1. Run the pinned Niri direct-IPC smoke in its default live mode from
   [ADR 0004](../adr/0004-single-monitor-niri-spatial-mapping-policy.md) and
   verify visible workspace, floating/tiled, percentage-size, focus, and
   unrelated-window behavior on equal and differing outputs.
2. With operator-provisioned SSH host keys and authentication, route one launch,
   lose one response, and verify the same token/session/window is recovered.
3. Attach host and leech Zellij clients concurrently, close only the leech
   projection, reopen it, exhaust/restart recovery, and verify host work remains
   alive throughout.
4. Exercise manual close, source disappearance grace, controller restart, Niri
   restart, host epoch replacement, host-location convergence, and v1 host-writeback
   rejection while a sentinel unrelated window stays untouched.

The smoke must not weaken host-key checking, authorize credentials, activate a
consumer binding, or turn a failure into local fallback. Exact live column
reordering, multi-monitor mapping, and pinned-Zellij watch remain outside MVP.
The consumer-facing checklist and migration/rollback sequence are documented in
[HOST_LEECH_READINESS.md](../HOST_LEECH_READINESS.md). Run
`scripts/tests/host-leech-consumer-contract.py` before deployment; it verifies
the technical contract, schema, defaults, templates, links, and package bytes
without live access.
