---
title: Make history boot-aware and crash-durable
priority: critical
---

## Goal

Establish the storage guarantees required for power-loss recovery: boot-epoch metadata, durable writes, tolerant replay, and crash-recoverable writer exclusion coordinated with pruning.

## Acceptance Criteria

- New events and snapshots record the Linux boot ID while existing records remain readable.
- Event append acknowledgement occurs only after durable synchronization; snapshots use flushed temporary files, atomic rename, and directory synchronization.
- Replay ignores only a malformed trailing event and preserves all earlier complete events.
- Writer exclusion recovers safely after process death or reboot and prune cannot race an active writer.
- Focused corruption, stale-lock, compatibility, and durability tests pass.
- `go test ./...` passes.

## Design Decisions

- Extend persisted schemas additively rather than invalidating existing history.
- History remains subject to existing retention policy.
- Use advisory locking or boot-aware stale-owner detection; a persistent O_EXCL file must not block capture forever.

## Implementation Notes

Primary areas: internal/events, internal/snapshots, internal/replay, internal/prune, internal/capture, and model/schema tests. Preserve current state directory compatibility. ADR: docs/adr/0001-resume-zellij-terminals-in-niri.md.
