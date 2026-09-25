# Minimal persistent mirror reconnect

Status: implementation in progress. The smaller direction and the two highlighted requirements below are approved. Public window launch behaviour has not changed.

### Progress

- Milestone 1, catalog foundation: implemented additive boot-bound session IDs, strict complete inventory validation, a headless `mirror session-catalog` source helper, and bounded non-interactive remote acquisition using the configured helper prefix. Empty inventory is distinct from unsupported or failed observation. Literal session names are preserved rather than tokenized.
- Fixed the pinned Zellij empty-catalog outcome: only its exact exit-1/stderr result is accepted as empty; other failures remain unknown. Verified against isolated Zellij 0.44.3 directories, never the user's sessions.
- Validation for this checkpoint: `go test ./...`, `go test -race ./internal/mirror ./internal/zellijlive ./cmd/redeem`, `go vet ./...`, and `nix build .#terminal-redeemer --no-link` passed.
- Milestone 1, exact attachment: implemented private `mirror session-attach`, descriptor-anchored and post-link-verified socket pinning, attempt-local cache isolation, bounded protocol relay, and attempt-specific readiness rendered by the real Zellij client. Status probes and startup stdout cannot mark ready. Session switching becomes detach; reconnect has no creation authority.
- A real same-name replacement test exposed immediate socket-inode reuse. Reconnect IDs now include `statx` socket birth time, with no unsupported-filesystem fallback. Existing checkpoint IDs are unchanged.
- Milestone 1 is complete. Real-PTY tests cover literal names, verified fresh input, replacement before and during attachment, detach, cancellation and source-session survival. Full Go tests, targeted race tests, three real-Zellij/PTY race repetitions, vet and a local Nix package build passed. The vendor hash was recomputed for `creack/pty`. See [exact attachment evidence](../testing/exact-mirror-attachment.md).
- Milestone 2 is complete as an internal terminal core: persistent physical-terminal ownership, replaceable SSH PTYs, current-attempt readiness/outcome decoding, generation-bound input, paste-boundary preservation, bounded IO/cleanup, resize and terminal restoration. The shared-control seam has a hermetic adapter in tests; production coordination/private CLI wiring belongs to milestone 3.
- Full Go tests, targeted races, three combined PTY/source-Zellij race repetitions (no skips), vet and a local Nix build passed for milestone 2. See [persistent terminal evidence](../testing/persistent-session-terminal.md), including the unread-paste-start regression that rules out blind readiness flushing.
- Milestone 3 is complete: private supervisor/coordinator CLI, process-bound runtime membership, shared catalog/backoff, fair two-slot admission, restart-safe helper-held slot locks and probe cleanup, bounded output and read-only outage gating for discovery/follow. No desired-window registry or service deployment was added. See [shared recovery evidence](../testing/shared-mirror-recovery.md).
- Milestone 3 Go gates passed: full tests, mirror/Zellij/CLI races, three combined supervisor/real-Zellij/cross-process repetitions (no skips), vet, and ten additional thirty-process race repetitions. Final pidfd/replay corrections also passed all 17 recovery race tests and a refreshed local Nix build. The comprehensive combined suite also passed on that final source state.
- Milestone 4 foundation: the private helper exposes bounded, process-authenticated local state/input IPC. Clipboard operations can bind an origin before acquisition and revalidate completion directly against the current child PTY, including Ctrl-V fallback. Full tests, mirror/Zellij/CLI races, three repetitions of clipboard/PTY boundaries, vet and a local Nix build passed. Public launch planning now uses this boundary.
- Milestone 4 ownership integration now recognizes verified offline helpers, suppresses their SSH descendants, and retains legacy direct-SSH evidence. Process-tree observation is all-or-nothing and rechecks ancestry after helper IPC. Save checks a supervised view against the fresh snapshot's exact incarnation; presence, not readiness, drives apply/follow deduplication. Full Go tests, mirror/procmeta/Zellij/CLI races, vet and a local Nix build passed before launch cutover.
- Milestones 4–5 remain incomplete. Workflow/ownership and asynchronous clipboard integration remain mandatory before public cutover. Public launches remain unchanged; no independent per-window host retry loop has been enabled.

## In one minute

1. Keep the existing `mirror` commands and shared launch planner.
2. Put a small persistent helper between Kitty and SSH. Replace SSH, not the window.
3. Give the helpers one small, on-demand local coordinator for shared host checks and bounded attachment attempts.
4. Verify the exact remote session before every attachment; never create during reconnect.
5. Ship and verify reconnect before extending name-based automatic opening.

**Non-negotiable: 30 disconnected windows share one host check.**

**Non-negotiable: offline/connecting input and paste are discarded, including the Enter used to request retry. They must never arrive in the remote shell after reconnect.**

No replacement product architecture, generic rule engine, public-command rename, durable projection database, or resurrection of the retired slice controller.

## Starting point in Jujutsu

Use current main, not the conflicted feature as an implementation base:

- Base: `9582706967f66919538518478ba10aab83e08c43` (`main` when this plan was prepared).
- Planning change: `zszquwwpormzsmttxoylyokyrnnomvnu`, created with `jj new main` in the existing workspace.
- Original implementation preserved at bookmark `reconnect-reference-original`, commit `f5eb20ea4c3d07e6b9746039281e8868d48f8848`.
- Conflicted rebased implementation preserved at bookmark `reconnect-reference-rebased`, commit `b31ca125fd2f6eb80e436f7623998eb59c08560d`.

The reference bookmarks intentionally retain two versions of the same old change. Use the bookmark or full commit ID, not its now-ambiguous change ID. Nothing was abandoned, pushed, or merged into main. The current workspace is no longer based on the conflicted feature.

After the planning commit, implement in new changes on this clean line. Keep the existing commands working between changes; do not switch public launch paths until the safety prerequisites pass. Prefer one reviewable conventional commit per milestone, with tests alongside implementation. Recheck main before coding if time has passed; do not silently rebase or combine the reference branches.

Frontloop's status tool rejected this repository's legacy queue layout (missing the v2 default layout). Do not manually edit its queues or make migration part of reconnect. This document is the plan until queue migration is separately authorized.

Planning verification: `go test ./...` passed on this clean-main working tree while preparing the document. Only this plan changed. This is baseline evidence, not validation of the unimplemented reconnect milestones below.

## Behaviour contract

| Event | Required result |
|---|---|
| SSH/network loss or suspend/resume | Same Kitty PID, Niri window ID and placement; replace only transport. |
| Waiting or connecting | Show host/session, reason, shared countdown, and Enter-to-retry. Acknowledge Enter locally without waiting for SSH. |
| Typing or pasting before verified readiness | Discard it; no buffered input crosses into a later attachment. |
| Thirty helpers lose one configured host | One in-flight host probe and one retry deadline, not thirty independent backoff loops. |
| Host returns | Admit bounded exact attachments, initially at most two not-yet-ready attempts. Ready connections do not occupy these slots. |
| One session fails while others work | Back off that attachment; do not tear down healthy connections or declare the whole host down. |
| Remote Kitty disappears or Niri is unavailable | Not proof that Zellij ended. Existing projections may reconnect to a still-active headless session. |
| Authoritative evidence confirms the bound session ended | Close the local projection. Failed, stale or incomplete observations are not end evidence. |
| Same name now identifies a different incarnation | Never attach to it. Report replacement; close only on authoritative confirmation that the old incarnation ended, not an ambiguous observation. |
| User closes local Kitty or deliberately detaches | Stop that helper and its transport; reconnect itself never opens another window. |
| `mirror new` loses transport | Creation authority is used once; later attempts only attach to the established incarnation. Uncertain creation is reconciled, not blindly repeated. |

Scope qualification: today's foreground `mirror follow` independently reopens manually closed eligible windows. **Do not change or claim to solve that policy in the reconnect milestone.** Outside follow, a closed projection stays closed. The previously cancelled close-policy question remains unanswered for future following changes.

## Minimal implementation shape

Keep workflow policy and launch planning in `internal/mirror`. New implementation files there, plus thin private CLI entry points in `cmd/redeem`, are sufficient unless code proves a small lower-level extraction necessary. No new public framework is required.

The persistent helper owns terminal input and a separate child PTY. Its immutable identity includes configured source/transport, exact session incarnation and projection token. One attachment is disposable; the helper and Kitty are not.

The shared coordinator is a transient, same-user process reached over a private runtime Unix socket. It exists because independent Kitty processes cannot share a Go mutex. Its only work is host-observation scheduling and attachment admission; it has no authority to select, launch, close or reposition windows. Runtime membership is not a durable desired-state registry. No systemd service, timer, history store, or restored slice protocol.

Key coordination by the configured transport identity: exact SSH destination, executable/options and relevant source-helper/profile configuration. Do not claim that different SSH aliases necessarily mean the same machine or accidentally merge different user/port configurations.

## Milestone 1 — prove exact remote attachment before changing launch behaviour

Likely files: `internal/mirror/snapshot.go`, `remote.go`, `dual_new.go`, new remote-attachment code/tests, `internal/zellijlive`, and private CLI dispatch in `cmd/redeem`.

- Add an additive session-incarnation field to snapshots while preserving the current session-name inventory. Obtain names and IDs from the same catalog observation; validate consistency and completeness.
- Supply the actual boot ID explicitly. Current mirror catalog callers leave `CommandCataloger.BootID` empty; copying their construction would undermine reboot identity.
- Add a small catalog-only remote operation for outage recovery. Session checks must work without Niri/Wayland; malformed compositor metadata must not masquerade as session termination.
- Derive remote helper argv from the configured Redeem snapshot-command prefix, preserving wrappers/absolute paths as `PlanSourceAttach` already does. Unsupported custom command shapes fail with an actionable error; never guess an unrelated `redeem` executable.
- Before attaching, compare the expected incarnation against the actual socket. Close the check-to-attach race using a verified, attempt-unique pinned socket namespace, adapting only the necessary exact-socket logic from the old implementation. Never follow the original name to a replacement socket after verification.
- Scrub inherited Zellij environment and prevent resurrection-cache fallback. Reconnect commands never receive creation authority.
- Establish attachment-specific positive readiness before accepting input. Prove this with the pinned Zellij version: SSH startup or an arbitrary elapsed delay alone is not sufficient. If a trustworthy readiness condition cannot be demonstrated, stop with evidence instead of weakening the contract.
- Keep missing/replaced/ambiguous session outcomes distinct from transport failure. A missing helper, invalid response, permission error or unsupported version is not session-end evidence.
- Preserve mirror's case-sensitive names and leading-dash handling. Do not import the old token-only session validator. Keep socket-path traversal/symlink checks at the filesystem seam and test valid supported names end-to-end.

Version policy: upgrade the remote helper before activating the new local launch path. New local code must fail closed, with an upgrade diagnostic, when exact-identity/attach capability is missing. Do not silently fall back to name-only reconnect. Old readers may ignore the additive snapshot field; avoid a generic schema migration or protocol-negotiation framework.

Acceptance: same-name replacement before and during attachment never receives input; reboot changes identity; headless sessions work; incomplete catalogs cannot close a view; unique attempt cleanup cannot remove another attempt's socket namespace; old remote capability fails safely. Existing launch behaviour and tests remain working while these helpers are introduced.

## Milestone 2 — implement and test the persistent window helper

Likely files: new supervisor/PTY code under `internal/mirror` or `cmd/redeem`, existing process utilities where applicable, and PTY tests.

- Own the physical terminal for the helper lifetime; use a separate PTY for each SSH child. Adapt the old raw-mode, generation/discard, resize and bounded-write mechanics without importing slice state or control types.
- Model offline, checking, connecting and verified-ready states explicitly. Input is admitted only for the current ready attachment; drain/discard kernel and user-space backlogs on transitions. Retry Enter is local control, never shell input.
- Keep input handling responsive under a blocked host call, saturated paste, stalled child input or descendants holding output pipes open. Bound writes, process cancellation and output drain; reap children and restore terminal/file flags on exit.
- Force unattended SSH behaviour, timeouts and keepalives. OpenSSH option precedence matters: user-supplied options must not undo BatchMode or retry bounds. Preserve host-key checking; never bypass it to avoid prompts.
- Forward resize and test repeated reconnect without changing the Kitty process. Account for PTY controlling-session requirements: `procrun.CommandContext`'s `Setpgid` cannot simply be combined with PTY `Setsid` setup.
- Distinguish intentional detach from transport loss using validated remote outcome evidence, not an unqualified SSH exit status alone.

Acceptance: isolated PTY tests exercise text/paste before and across readiness, retry Enter, fresh input after readiness, repeated disconnect, resize, stalled writes, signal/close cleanup and terminal restoration. No public cutover to an unsafe independent retry loop.

## Milestone 3 — add the smallest shared host scheduler

Likely files: a focused coordinator implementation/tests in `internal/mirror`, private CLI startup, and the supervisor's coordinator adapter.

- Use a same-user runtime socket and startup lock to elect one coordinator for one configured transport. Concurrent startup, stale socket recovery and last-client exit are covered by process-level tests.
- Keep registration/status/retry and attachment admission small and internal. Reject unrelated or stale client/attempt identities; use live process/instance evidence, not titles or PID alone.
- At most one bounded host catalog probe runs at a time. Run it outside the status/input path so all windows remain responsive while it blocks.
- Share capped exponential backoff (1, 2, 4, 8, 16, then 30 seconds) indefinitely. Enter coalesces with other requests, shows immediate feedback, and cannot create overlapping probes or a retry storm; retain a minimum one-second start spacing.
- Limit not-yet-ready attachments to two, with attempt deadlines and per-session backoff. A permanently bad session cannot monopolize admission or exhaust all other sessions' retries. Do not apply follow's lifetime launch budget to reconnect.
- Replayed client messages must not create extra attempts. Expired/released grants, old readiness and old transport-loss reports cannot affect a replacement attempt. Coordinator restart must invalidate pending attempts safely, preserve local windows and not introduce a burst of unbounded attachments; established healthy transports need not be killed.
- While the coordinator reports an outage, active follow/discovery polling must join or respect its shared recovery state rather than run a second independent outage retry loop. Healthy workspace discovery remains distinct from catalog-only health checks; do not turn Niri metadata into a reconnect prerequisite.

Acceptance: thirty separate clients/helpers demonstrate one in-flight probe and one shared deadline; simultaneous Enter requests coalesce; a deliberately blocked probe cannot freeze local feedback; reconnect admission is bounded and fair; close, coordinator restart and stale messages do not duplicate windows or attach attempts. In-memory unit tests alone do not prove cross-process sharing.

## Milestone 4 — switch the existing launch paths and ownership evidence

Likely files: `internal/mirror/windows.go`, `projection.go`, `pin_workflow.go`, `follow.go`, `new.go`, `dual_new.go`, `paste.go`, their tests, and `cmd/redeem/main.go`.

- Replace Kitty's direct SSH child with the persistent helper in the shared launch planner. Thread self-command, remote helper configuration, incarnation and correlation through **all callers**, including apply/follow, which currently do not pass all those fields. No separate per-workflow reconnect implementations. Already-running direct-SSH windows remain recognizable but cannot be retroactively given a supervisor without restarting them; do not silently close/relaunch them. The new persistence guarantee begins with helper-launched windows.
- Treat one verified supervisor under the Kitty PID as stable projection evidence. Its SSH descendant is not a second projection. Mere SSH-process existence is not readiness; expose verified attachment state separately from presence.
- Offline owned projections count as present for deduplication, correlation and one-time placement. Titles remain presentation, and ambiguous process evidence still fails closed. Preserve Nix-wrapped Kitty recognition and test the configured/wrapped helper executable too.
- Preserve `save`'s fresh-snapshot contract. If source acquisition fails, return an error and leave the prior pin byte-for-byte intact. If the host is observable and a particular projection is offline, include its verified supervisor in the pin. Do not add offline pin reconstruction or cached-source persistence.
- Preserve `mirror new`'s one creation authority and best-effort source-local attachment. Establish and retain the new session's incarnation before enabling input/reconnect, including in its live ownership evidence. Reconcile an interrupted initial creation only when the original incarnation can be proven; a generated name alone is not a replacement for lost creation evidence. Otherwise report uncertain creation and require explicit user action; never replay `--create` as an ordinary retry. Source-view failure still cannot destroy the session or local view.
- Preserve clipboard support, with explicit tests for its asynchronous path: `paste-image` can finish an upload after readiness changes, then inject via Kitty. Reject/cancel stale paste by the originating attachment generation rather than allowing a late unqualified `send-text`. Offline/connecting text fallback and image-path injection must both obey the input contract.
- Preserve current follow selection, cap and manual-close semantics in this milestone. No lock-lifetime/rule-engine rewrite is needed merely to retain existing windows. A later longevity change will revisit the long-held host/profile lock separately.

Acceptance: open/apply/follow/new all retain the same local window during transport loss; apply and follow never duplicate an offline supervisor; save failure preserves the pin; connected and offline save cases have regressions; no creation replay; clipboard completion cannot inject stale input; existing placement and source-local attach tests still pass.

Milestone 4 implementation is complete: all four public launch paths now run the same immutable-incarnation supervisor; old direct-SSH forms exist only as recognition and test fixtures. `mirror new` creates once via the headless source helper, anchors the resulting socket identity, validates it against a complete catalog, then opens the view. Lost/invalid receipts and identity changes fail without replay or name-only adoption. Dry-run prints an explicitly invalid receipt placeholder rather than a guessed identity. Targeted launch/workflow, source creation, real pinned-Zellij creation/attachment, and lost-receipt/source-view failure regressions pass. Final comprehensive gates are recorded in the milestone 5 evidence.

## Milestone 5 — validation, documentation and deployment evidence

Update ADR 0002 narrowly: persistent per-window helper, incarnation verification, and an on-demand shared recovery process are now deliberate exceptions to its old process-evidence/no-runtime wording. Do not carry forward the old slice ADR unchanged. Keep local checkpoint/resume ownership under ADR 0003 untouched.

Document waiting/checking/connecting/ended/error states, source-helper requirements, intentional-close versus foreground-follow behaviour, unsupported-version diagnostics and the source-first activation sequence. Keep the simple text reconnect screen; no TUI redesign.

Required automated verification on the final clean-main port:

```sh
go test ./...
go test -race ./internal/mirror ./internal/zellijlive ./cmd/redeem
go vet ./...
nix build .#terminal-redeemer --no-link
```

Also repeat the actual PTY reconnect/discard/backpressure tests at least three times under the race detector and record which tests ran. Use fake clocks and deterministic fake transports for scheduling; use real PTYs and a hermetic pinned-Zellij fixture where process/terminal behaviour is the assertion. Recompute the Nix vendor hash for the new dependency set; the old feature's hash is not evidence for this port. Snapshot new files with jj before Nix evaluation as needed.

Operator-only smoke gate, after reviewing the immutable build: actual host disconnect/reconnect, hibernate/resume, thirty windows, retry Enter and pasted input, preservation of PID/window ID/placement, deliberate close and a disposable session ending. Never interrupt real networking, hibernate, alter credentials, deploy to both hosts, or terminate real sessions automatically.

Review the complete diff; verify no retired slice packages/services or unrelated checkpoint changes returned. Commit conventionally and verify `jj st`. Automated tests and builds do not substitute for physical-host smoke evidence.

## Next increment, not a blocker for reconnect

Once reconnect passes, extend existing `mirror follow`, not a generic rule engine:

- Add one explicit session-name selector. Name-only matching must consider the complete ACTIVE inventory, including headless sessions, not just visible Kitty windows in a selected workspace.
- Revisit the lifetime admission cap separately from reconnect. Bound uncertain/repeated opening and serialize duplicate-prone effects; inspect the existing per-session reservation in `local_attach.go` before adding another lock scheme.
- Narrow the host/profile lock to the effects that need it if follow becomes long-lived; do not let an indefinitely running follower exclude manual save/apply forever.

Before implementing this increment, decide: foreground versus persisted rule lifetime; whether explicit close suppresses reopening; session-name matching syntax; and destination placement for headless matches. None was decided by the cancelled earlier question. Recommend starting with one foreground selector and no persisted rule store; get agreement before changing established follow semantics.

## Reference and stop conditions

Reusable reference material from `reconnect-reference-original`: `cmd/redeem/projection.go` and PTY tests; pure retry/admission tests; exact socket attachment tests. Read via `jj file show -r reconnect-reference-original PATH`. Translate tests to the current session/window distinction instead of importing controller models or old source-window retirement semantics.

Stop with evidence if exact attach/readiness cannot be demonstrated, the shared scheduler needs product policy beyond this scope, or a port would require weakening either highlighted guarantee. Report the failed assertion, attempted mechanism and specific decision needed. Do not substitute independent per-window retries, shared-terminal SSH input, name-only attachment, or obsolete slice restoration to make tests pass.
