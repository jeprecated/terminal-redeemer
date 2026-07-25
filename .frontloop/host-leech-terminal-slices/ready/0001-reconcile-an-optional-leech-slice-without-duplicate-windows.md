---
title: Reconcile an optional leech slice without duplicate windows
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-6
---

## Goal

Add an opt-in bounded leech controller that continuously converges one selected slice onto locally owned Niri/Kitty windows while host sessions remain authoritative and operations remain idempotent and reversible.

## Acceptance Criteria

- Provide a foreground controller with bounded timeouts and optional packaged service wiring disabled by default.
- Support inspect, pickup, drop, and undo according to the chosen persistence semantics.
- Create exactly one owned leech window per picked-up stable source-window identity, including multiple windows sharing one session and repeated revisions.
- Reconcile newly included, removed, dropped, moved, or changed source windows within a configured bound.
- Use interactive attach concurrently from host and leech, scrub nested-Zellij variables, and never attach-or-create.
- Handle disconnects, timeouts, partial inventory, restart, source epoch replacement, and local failures with bounded backoff and full resync.
- Make repeated pickup/drop/undo/reconcile/reconnect/close idempotent without duplicate windows.
- Close only positively owned local projection windows and never emit remote session termination.
- Handle manual local close according to the chosen selection decision without touching unrelated Kitty windows.
- Preserve existing mirror snapshot/list/open/status/close behavior during compatibility rollout.

## Design Decisions

- Controller is opt-in and disabled by default.
- Use bounded polling of revisioned snapshots initially.
- Host owns source existence, session lifecycle, and placement; leech owns only marked projections and slice state.
- Never attach-or-create or terminate host sessions.
- Stable source-window identity is the duplicate-suppression key.

## Implementation Notes

Depends on both clarify decisions plus protocol and transport tasks. Separate pure desired-state planning from side effects so failure and idempotence are hermetically testable.
