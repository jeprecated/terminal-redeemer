# Persistent mirror reconnect: current-main acceptance

This is the `mirror` implementation, not the retired slice/controller feature.
The contract is [the approved plan](../plans/persistent-mirror-reconnect.md).
Local checkpoint/resume ownership and ADR 0003 are unchanged.

## Public launch and creation boundary

`open`, `apply`, `follow` and `new` all use `PlanLaunch`/`PlanNew` to run one
local `mirror session-supervisor` inside Kitty. The argv binds the configured
transport/helper prefix, literal name, exact socket-birth incarnation and unique
projection token. Apply/follow retain their existing selection, attempt caps,
correlation and one-time placement. Ready and offline helpers count as present.
Old direct-SSH windows remain recognizable; they are not silently restarted.

`new` first invokes one unattended, bounded `session-create` source operation.
It refuses an existing generated name, including resurrection-cache collisions,
creates headlessly once, captures its socket identity immediately after success,
and checks that identity against a complete inventory before returning a receipt.
Only that receipt authorizes the local view. Failed creation, missing/truncated
receipt, cancellation, changed identity or failed post-creation observation never
triggers another creation or a later lookup to adopt the generated name. The error
reports the generated name for explicit operator inspection; a session may exist.
Source-local Kitty attachment remains best effort after local launch and cannot
roll back the session/view. A local launcher failure likewise does not delete the
created session. Dry-run's launch template contains an invalid receipt placeholder.

Public image-paste mappings capture helper/attempt/input generation before
clipboard acquisition. Image-path injection and Ctrl-V fallback both revalidate
inside the supervisor event loop, immediately before the current PTY write.
There is no unqualified late Kitty `send-text` in the supervised mapping.

## Automated evidence

Completed component gates: full Go suites, mirror/procmeta/Zellij/CLI races,
vet, repeated real PTY and pinned-Zellij tests, and local Nix builds. Final
post-cutover comprehensive gates are pending at this document's initial commit;
results will be recorded below after completion.

| Invariant | Regression/evidence |
| --- | --- |
| Shared planner, literal name/ID, token-bound clipboard, no create in viewer | `TestPlanLaunchAttachMetadata`, `TestPlanNewRequiresReceiptAndNeverCreates`; CLI open/new dry-run tests |
| No name-only fallback in apply/follow, including apply dry-run | `TestApplyAndFollowRefuseNameOnlyLaunches` |
| Offline save and no duplicate apply/follow | `TestSupervisedPresenceSavesAndDeduplicatesRegardlessOfReadiness` |
| Complete ownership, wrapped executables, no double-counted transport | `TestSupervisorProjectionOfflineOwnershipAndTransportDescendant`, `TestTwoSupervisorsUnderOneWindowRemainAmbiguous`, complete-tree/PID-reuse regressions |
| One creation; lost receipt never adopted/replayed | `TestSessionCreationRunsOnceAndRequiresReceipt`, `TestRemoteCreationLostOrWrongReceiptNeverRetries` |
| CLI receipt precedes view; source-view failure preserves it | `TestMirrorNewCreationReceiptPrecedesViewAndNeverReplays` |
| Actual headless creation returns a usable exact identity | `TestRealPinnedZellijCreationReceiptAttachesExactly` |
| Original incarnation and actual rendered-client readiness | [Exact attachment evidence](exact-mirror-attachment.md) and `TestRealPinnedZellij*` |
| Discard offline/connecting input, partial paste, stale grants and delayed clipboard | [Terminal evidence](persistent-session-terminal.md); `TestSessionSupervisor*`, `TestSessionInputPasteKeepsOriginAcrossEverySplit`, `TestSessionGateDoesNotFlushUnreadPasteStart`, `TestSessionLocalOriginRejectsOfflineConnectingAndDelayedPaste`, `TestPasteBridgeUploadAndFallbackKeepOriginalAttachment` |
| Thirty processes, one probe, two pending slots, retry coalescing and safe daemon restart | [Shared recovery evidence](shared-mirror-recovery.md); `TestSessionRecoveryThirtyProcessesShareBlockedProbe`, `TestSessionRecoveryRestartPreservesProbeAndPendingBounds` and companion cross-process tests |

Real-source fixtures isolate HOME, XDG, sockets and cache under `/tmp/ra-real-*`.
They do not access `/tmp/zellij-1000` or operator sessions. Shell/transport/Niri
fixtures prove command sequencing and boundaries, not physical GUI continuity.

## Runtime requirements and source-first upgrade

- Source Redeem must provide exact snapshot/catalog IDs, attachment and creation
  helpers; Zellij is pinned to **0.44.3**. Socket birth time from Linux `statx` is
  mandatory. Unsupported version/filesystem observations are errors, not absence.
- Viewer runtime uses owner-only local sockets/files and same-UID kernel pidfd
  authentication (`SO_PEERPIDFD`, Linux **6.5+**). Aliases are not merged; the full
  configured transport/helper identity keys shared recovery.
- Review/build the immutable package first. Upgrade the **source first**, verify
  exact IDs and the configured wrapper prefix, then upgrade the viewer. Newly
  opened windows receive persistence; existing direct-SSH windows do not.
- Waiting/offline, checking and connecting retain the window with input closed.
  Enter requests a coalesced check; backoff is 1, 2, 4, 8, 16, then 30 seconds
  indefinitely. Real rendered-client readiness opens input. Validated detach or
  fresh complete absence of the original incarnation permits exit. Failed,
  stale, incomplete or ambiguous observations cannot close a retained window.
- Explicit close stops that window's helper. A still-running foreground follow
  may open another eligible window under its unchanged manual-close policy.
  Stopping follow stops additions, not the already launched supervisors.

No deployment, activation, networking changes, credentials, hibernate or operator
session termination is part of repository validation.

## Operator-only acceptance (not claimed by hermetic tests)

After explicit user activation on both hosts, use disposable sessions:

1. Record Kitty PIDs, Niri window IDs, workspaces and placement for views opened
   through all four workflows. Interrupt and restore transport; verify those
   identities and placements stay unchanged.
2. With thirty views, verify one host countdown/check, coalesced Enter and no more
   than two pending attachments. Repeat after suspend/hibernate/resume.
3. Type, paste and press retry Enter offline/connecting; verify none reaches the
   later shell. Delay an image upload across reconnect and verify it is rejected.
4. Verify a same-name replacement never receives input from an old window. End a
   disposable original session; only fresh authoritative absence may close it.
5. Save with a verified offline view while the host is observable; verify it is
   retained. Fail source acquisition and verify the old pin bytes are unchanged.
6. Close one view explicitly, with and without foreground follow running, and
   verify the documented distinction. Source-view failure must not kill either
   the session or the successfully launched local view.

These physical-host results remain **unperformed** until the operator records them.
