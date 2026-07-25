---
title: Reconcile selected host workspaces without duplicate windows
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-6
---

## Goal

Add an opt-in bounded leech controller that continuously projects selected static host workspaces plus explicit per-window overrides onto locally owned Niri/Kitty windows while host sessions remain authoritative.

## Acceptance Criteria

- Provide a foreground controller with bounded timeouts and optional packaged service wiring disabled by default.
- Support persistent selected workspace names plus inspect, individual pickup, drop, and undo overrides.
- Create exactly one owned leech window per eligible stable source-window identity and report any one-Kitty-window/one-Zellij-session invariant conflict.
- Automatically project every eligible current or newly opened host terminal in a selected workspace within a configured bound.
- Reconcile workspace selection changes, explicit overrides, source close, host workspace moves, order/size changes, and repeated revisions.
- Use interactive attach concurrently from host and leech, scrub nested-Zellij variables, and never attach-or-create.
- Handle disconnects, timeouts, partial inventory, restart, source epoch replacement, and local failures with bounded backoff and full resync.
- Make repeated pickup/drop/undo/reconcile/reconnect/close idempotent without duplicate windows.
- Close only positively owned local projection windows and never emit remote session termination.
- Treat manual local close as a visible leech-local drop override without touching unrelated Kitty windows or host work.
- Apply host-location mode without writing local rearrangements back, and apply leech-location mode with ownership-checked host workspace/order/size updates and feedback-loop prevention.
- Create missing named workspaces under the approved single-monitor mapping policy.
- Preserve existing mirror snapshot/list/open/status/close behavior during compatibility rollout.

## Design Decisions

- Controller is opt-in and disabled by default.
- Use bounded polling of revisioned snapshots initially.
- Host owns source existence, session lifecycle, and placement; leech owns only marked projections and slice state.
- Never attach-or-create or terminate host sessions.
- Stable source-window identity is the duplicate-suppression key.
- Static workspace names are the automatic projection unit; named slices are not required initially.
- Initial spatial scope is one monitor per machine.

## Implementation Notes

Depends on the resolved workspace-sharing and single-monitor spatial policies plus protocol and transport tasks. Separate pure desired-state planning from side effects so failure and idempotence are hermetically testable.
