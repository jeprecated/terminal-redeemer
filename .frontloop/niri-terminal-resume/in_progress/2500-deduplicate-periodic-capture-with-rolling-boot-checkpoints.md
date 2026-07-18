---
title: Deduplicate periodic capture with rolling boot checkpoints
priority: high
---

## Goal

Keep the complete 60-second reconciliation cadence while appending history only when normalized captured state changes. Preserve prior-boot freshness, empty-candidate behavior, crash durability, and forensic history through one atomically refreshed checkpoint per boot.

## Acceptance Criteria

- Every timer activation still performs a complete Niri windows/workspaces and terminal-metadata reconciliation.
- The first successful capture for each boot appends a boot-aware `state_full` event even when state equals the previous boot; subsequent unchanged captures append no event.
- A semantic state change appends one durable `state_full` event and updates the rolling checkpoint.
- Each successful capture atomically and durably refreshes one checkpoint for its boot with boot ID, host/profile, observed time, full state, state hash, and event position/reference.
- `redeem resume` selects the newest prior-boot rolling checkpoint by latest successful observation time, preserving authoritative empty candidates and age policy; durable event fallback covers missing/stale checkpoint updates after a crash.
- Legacy bootless events and existing snapshots remain compatible with explicit historical restore.
- Concurrent captures remain single-writer and cannot race duplicate unchanged events or checkpoint updates.
- Pruning removes expired rolling boot checkpoints consistently with retention.
- Tests cover unchanged capture suppression across separate processes, first capture per boot, state changes, empty candidates, checkpoint/event crash boundaries, concurrent capture, age freshness, prune, and legacy compatibility.
- `go test ./...` and `nix flake check 'path:.'` pass.

## Design Decisions

- Use change-only append history plus one rolling checkpoint per boot.
- Full reconciliation cadence remains configurable and defaults to 60 seconds.
- Checkpoint freshness represents latest successful observation, not latest state change.
- Event log remains the forensic change timeline and recovery fallback.

## Implementation Notes

ADR follow-up to docs/adr/0001-resume-zellij-terminals-in-niri.md. Likely areas: internal/capture, a durable boot-checkpoint store, internal/replay/resume selection, prune, doctor, docs/CONFIG.md, docs/OPERATIONS.md. Use advisory store locking and fsync/atomic rename discipline. Parent owns Frontloop lifecycle; writer must not edit task files.
