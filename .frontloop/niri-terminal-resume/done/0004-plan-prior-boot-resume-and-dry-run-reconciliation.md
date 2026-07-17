---
title: Plan prior-boot resume and dry-run reconciliation
priority: high
---

## Goal

Add the boot-aware, terminal-only resume selection and planning layer, including safe defaults, idempotence checks, workspace resolution, and a non-mutating dry run.

## Acceptance Criteria

- `redeem resume --dry-run` selects the latest valid checkpoint from a boot other than the current boot and reports its boot ID and capture time.
- A newer empty prior-boot candidate is reported as empty and does not silently fall back to an older boot.
- Legacy records without boot IDs remain available through explicit historical restore but are not silently selected for resume.
- Current Kitty/Zellij sessions are identified so already-open captured sessions are skipped.
- Unavailable sessions are reported and no dry-run path invokes Zellij attach or create.
- Workspace targets resolve by name, then output plus index, then index, with configurable unresolved-workspace behavior.
- Structured results distinguish already_open, unavailable, degraded, stale, and failed items.
- Selection, age policy, empty candidate, duplicate, unavailable session, and workspace resolution tests pass.
- `go test ./...` passes.

## Design Decisions

- `redeem resume` is terminal-only by default.
- Crash resume must never use `zellij attach --create`.
- Historical `restore apply --at` and `restore tui` remain the explicit forensic interfaces.

## Implementation Notes

Primary areas: cmd/redeem, internal/config, internal/replay, internal/restore, internal/procmeta, and TUI/history compatibility tests. Keep the planner independently testable from Niri and Zellij processes. ADR: docs/adr/0001-resume-zellij-terminals-in-niri.md.


## Completion Summary

- Added boot-aware selection of the newest complete checkpoint from a prior boot, excluding current-boot and legacy bootless records.
- Preserved newer empty/stale candidates without fallback and added explicit forensic restore guidance.
- Added pure terminal reconciliation for already-open, duplicate, unavailable, degraded, stale, failed, and ready items.
- Resolved workspace targets by name, output plus index, then index, with configurable current/skip/fail behavior and a safe degraded default.
- Added a non-mutating `redeem resume --dry-run` path with structured candidate/item/summary output and configurable 24-hour age policy.
- Passed independent Opus ACCEPT/re-ACCEPT, parent `go test ./...` (204 tests), and parent `nix flake check 'path:.'`.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- docs/CONFIG.md
- docs/OPERATIONS.md
- flake.nix
- internal/config/config.go
- internal/config/config_test.go
- internal/procmeta/session_verifier.go
- internal/procmeta/session_verifier_test.go
- internal/replay/checkpoints_test.go
- internal/replay/history.go
- internal/resume/planner.go
- internal/resume/planner_test.go
- modules/home-manager/terminal-redeemer.nix
