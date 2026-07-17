---
title: Capture stable Niri placement metadata
priority: high
---

## Goal

Extend the captured state so each verified Zellij terminal has a cross-boot workspace reference and enough optional Niri layout metadata for best-effort reconstruction.

## Acceptance Criteria

- Workspace capture preserves name, index, and output in addition to runtime ID.
- Terminal windows preserve verified Zellij session identity, CWD, column order, floating state, and available tile dimensions.
- Cross-boot restore data treats runtime Niri IDs as historical evidence only.
- State hashing, normalization, diff/event persistence, snapshots, and replay retain workspace-only and placement-only changes.
- Existing mirror snapshot contracts remain backward compatible.
- Parser, model, diff, replay, and fixture tests cover named and unnamed workspaces plus optional layout fields.
- `go test ./...` passes.

## Design Decisions

- Zellij session name is the canonical terminal identity; process environment/arguments precede verified title fallback.
- Workspace resolution data is name first, then output plus index, then index.
- Correct workspace capture is required; detailed layout capture is optional/best effort.

## Implementation Notes

Primary areas: internal/model, internal/niri, internal/procmeta, internal/diff, internal/events, internal/replay, and mirror compatibility tests. The current default CommandSnapshotter already combines windows and workspaces. ADR: docs/adr/0001-resume-zellij-terminals-in-niri.md.


## Completion Summary

- Captured durable workspace name/index/output references while retaining runtime Niri IDs as evidence.
- Added optional column, floating, tile-size, and window-size placement metadata without breaking legacy payloads.
- Made workspace-only changes persist atomically and placement-only changes replay as sparse patches.
- Preferred durable workspace selectors over runtime IDs in historical planning.
- Added named/unnamed workspace fixtures, normalization/hash, capture/snapshot/replay, legacy compatibility, and enrichment coverage.
- Passed independent Opus judgment and parent `go test ./...` validation (174 tests across 18 packages).

### Files Changed

- internal/model/state.go
- internal/model/state_test.go
- internal/niri/adapter.go
- internal/niri/adapter_test.go
- internal/niri/testdata/placement.json
- internal/diff/engine.go
- internal/diff/engine_test.go
- internal/capture/runner.go
- internal/capture/runner_test.go
- internal/collector/collector_test.go
- internal/replay/engine.go
- internal/replay/engine_test.go
- internal/restore/plan.go
- internal/restore/plan_test.go
