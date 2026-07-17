---
title: Launch and place exact resumed terminals
priority: critical
---

## Goal

Implement applying resume so each captured Zellij session is attached in one Kitty window, correlated to the exact Niri window, and moved to its resolved workspace without order-based guessing.

## Acceptance Criteria

- Resume launches Kitty through argv-based execution and retains a correlation identity for the resulting Wayland client.
- Each launch waits with a bounded timeout for the exact Niri window, using client PID when supported; unrelated concurrent Kitty windows cannot steal placement.
- Zellij is invoked with attach-only semantics and a failed/unavailable attachment never creates a replacement session.
- A session is reported restored only after successful attachment evidence and required workspace movement.
- Repeated resume against unchanged state creates no additional windows.
- Failures are item-isolated and structured output distinguishes restored, already_open, unavailable, degraded, and failed items.
- Column ordering, floating state, and sizing are attempted only as best effort and reported separately from required workspace placement.
- Integration tests cover multiple sessions/workspaces, concurrent unrelated windows, timeout, failed attach, failed move, rerun idempotence, and unsupported launcher correlation.
- `go test ./...` passes.

## Design Decisions

- Correlate by identity, never application ID plus creation order.
- Preserve Kitty's normal application ID and existing Niri rules.
- Workspace placement is required for restored status; finer layout fidelity is best effort.

## Implementation Notes

Primary areas: internal/restore, internal/niri, command runners, cmd/redeem, and integration fakes. Avoid shell-string execution for the outer Kitty launch. ADR: docs/adr/0001-resume-zellij-terminals-in-niri.md.


## Completion Summary

- Implemented applying `redeem resume` with direct Kitty argv and attach-only `zellij attach -- <session>` semantics.
- Correlated each launched Kitty client to exactly one Niri window by retained client PID, with bounded polling and no app-ID/order fallback.
- Required two consecutive live attachment observations plus verified workspace movement before reporting `restored`.
- Kept failures item-isolated with conservative process cleanup, move-failure no-duplicate behavior, and rerun/in-execution idempotence.
- Applied floating and sizing metadata only through exact window-ID actions and reported optional layout degradation separately; column order remains explicitly unsupported.
- Added adversarial integration fakes for multiple sessions, unrelated windows, ambiguity, timeout, attach failure/race, failed/unobserved moves, degraded input, reruns, and unsupported correlation.
- Passed independent Opus ACCEPT/re-ACCEPT, parent `go test ./...` (228 tests), resume race tests (37 tests), and `nix flake check 'path:.'`.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- docs/CONFIG.md
- docs/OPERATIONS.md
- internal/config/config.go
- internal/config/config_test.go
- internal/resume/executor.go
- internal/resume/executor_test.go
- internal/resume/planner.go
- internal/resume/runtime.go
- internal/resume/runtime_test.go
- modules/home-manager/terminal-redeemer.nix
