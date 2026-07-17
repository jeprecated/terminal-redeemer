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
