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
