---
title: Reconcile selected host workspaces without duplicate windows
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-6
---

## Goal

Add an opt-in bounded leech controller that projects selected host workspaces and explicit per-window overrides onto positively owned local Niri/Kitty windows while host sessions remain authoritative.

## Acceptance Criteria

- Depend on the resolved domain/sharing/spatial contracts, stable protocol, hardened RPC, and both passing executable spikes.
- Provide a foreground singleton controller with a mode-0600 local Unix control socket, store lock, bounded timeouts, and optional packaged service wiring disabled by default.
- Serialize status, workspace selection, pickup, close/drop, reopen, undo, explicit reconnect, and routed-launch handoff through the controller.
- Persist a state machine distinguishing `launching`, `connected`, bounded `reconnecting`, stable `disconnected`, `closed_by_user`, source-gone grace, and conflict/degraded states.
- Stop retrying after a configurable bounded exponential retry window; recovery within that window may re-adopt uniquely matching projections, while explicit reconnect restarts attempts afterward.
- Create exactly one local projection per eligible source identity using a unique Kitty app ID plus atomically persisted mapping and Niri PID/process-command evidence; titles are never ownership evidence.
- Automatically project eligible current/new host terminals in selected workspaces except exact `closed_by_user` overrides; keep closed sources discoverable and reopen only explicitly.
- Use the proven live-only attachment wrapper concurrently with the host; never attach-or-create, resurrect, prefix-match another session, or inherit a user setting that quits the host session on projection close.
- Consume only complete authoritative revisions for destructive reconciliation; degraded inventory, disconnects, and isolated sub-query failures cannot close projections.
- Require consecutive complete absence or a bounded source-gone grace before closing an owned projection for source disappearance.
- Reconcile workspace selection, explicit overrides, source close, host workspace/floating/size changes, repeated revisions, controller restart, source epoch replacement, and duplicate responses idempotently.
- Close only positively owned local projection windows and never emit remote session termination.
- Treat manual local disappearance as `closed_by_user` when the host/source remains healthy; provide a packaged focused-close helper with safe direct-Niri fallback for later consumer binding, without editing mono-nix.
- Apply host-location mode without writeback and leech-location mode for ownership-checked workspace/floating/size updates with feedback-loop prevention; report order drift but do not correct it in MVP.
- Preserve existing mirror snapshot/list/open/status/close interactive-attach behavior during rollout; return a clear unsupported result for pinned-Zellij watch rather than constructing a nonexistent command.

## Design Decisions

- Controller is opt-in and disabled by default.
- Use bounded polling of revisioned host snapshots; local Niri event consumption may be continuous inside the controller.
- Host owns source existence and session lifecycle; leech owns only marked projections and local selection/connection state.
- Manual projection close persists until explicit reopen but does not hide or terminate the host source.
- Exact live order synchronization is stretch-only.

## Implementation Notes

Separate pure desired-state planning from side effects. Ownership, state transition, reconnect, and disappearance gates must be hermetically testable before live service wiring.


## Completion Summary

- Added an opt-in crash-safe singleton slice controller with private control socket/store lock, strict serialized control protocol, disabled-by-default service wiring, and bounded persisted authority.
- Implemented complete-only revision reconciliation, selected-workspace/pickup/close/reopen/drop/undo semantics, source-gone grace, durable bounded reconnect/exhaustion, explicit reconnect, epoch lineage/successor gates, and cleanup-before-launch barriers.
- Implemented exactly-one projection mapping before side effects, configured-executable/full-argv/PID/app/Niri ownership proof, crash re-adoption, exact token-correlated live attachment confirmation, and close-only-owned behavior.
- Implemented healthy-host manual-close classification, hardened post-lock focused-close fallback, monotonic routed-launch handoff, startup re-observation, and explicit unsupported pinned-Zellij watch compatibility.
- Integrated host/leech spatial planning with fresh per-action ownership proof, origin prevention, bounded failure recovery/rollback, and report-only order drift.
- Bound all authority/history/argv/state dimensions deterministically with fail-closed exact tombstone capacity, passed full Go/race/vet/Nix/package checks, and obtained final independent correctness and adversarial review passes.

### Files Changed

- internal/slicecontroller/
- internal/sliceprotocol/accept.go
- internal/sliceprotocol/protocol_test.go
- internal/sliceattach/attach.go
- internal/sliceattach/attach_test.go
- cmd/redeem/main.go
- cmd/redeem/main_test.go
- internal/config/config.go
- internal/config/slice_test.go
- internal/niriipc/types.go
- internal/slicerpc/
- internal/slicetransport/
- internal/mirror/windows.go
- internal/mirror/orchestration_test.go
- modules/home-manager/terminal-redeemer.nix
- flake.nix
- docs/PROTOCOL.md
- docs/OPERATIONS.md
- docs/CONFIG.md
- README.md
