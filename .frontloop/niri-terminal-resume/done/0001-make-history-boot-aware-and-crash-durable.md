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


## Completion Summary

- Added additive Linux boot IDs to newly written events and snapshots while preserving legacy decoding.
- Made event appends and snapshot/prune rewrites power-loss durable with fsync, atomic replacement, and malformed trailing-record recovery.
- Replaced stale O_EXCL markers with shared crash-recoverable advisory flock coordination for capture and prune.
- Added compatibility, corruption, process-death locking, prune-exclusion, durability-ordering, race, and repeated-run coverage.
- Passed independent Opus judgment and parent `go test ./...` validation (165 tests across 18 packages).

### Files Changed

- docs/OPERATIONS.md
- go.mod
- go.sum
- internal/bootid/bootid.go
- internal/storelock/lock.go
- internal/events/store.go
- internal/events/store_test.go
- internal/snapshots/store.go
- internal/snapshots/store_test.go
- internal/replay/engine.go
- internal/replay/engine_test.go
- internal/replay/history.go
- internal/replay/history_test.go
- internal/prune/prune.go
- internal/prune/prune_test.go
- internal/capture/runner_test.go
