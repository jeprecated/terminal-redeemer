---
title: Build a live controller-backed slice management TUI
priority: high
frontloop_approval_task: 825d83f26e1a3b5c4d26b920878c36f468ed5ae1b24877774b5dc032ea3e978b-3
---

## Goal

Provide `redeem slice manage`, a live Bubble Tea interface over the existing private controller socket for understanding and changing the current slice without exposing raw opaque JSON workflows.

## Acceptance Criteria

- A separate `internal/slicetui` package derives rows from controller status and never reads/writes controller files or executes effects directly.
- The UI groups named and unnamed workspaces and shows independent discoverable, desired, projection, closed, connection, degraded, and conflict facts without collapsing state axes.
- The UI retains inspectable orphan session-close records when no current source window exists.
- Actions support all enable/disable, workspace add/remove, pickup/pickup-remove, close/reopen, reconnect, undo, refresh, and quit through control verbs only.
- Bounded polling updates the view without restart; cursor identity remains stable across reorder/add/remove, and actions refresh from authoritative responses rather than optimistic local state.
- Controller unavailable/restarting, empty inventory, malformed or oversized `response_too_large` status, transient action errors, terminal resize, q, and Ctrl-C produce clear bounded behavior.
- Model/app tests use a fake control client and Bubble Tea messages; they require no TTY, Niri, SSH, Zellij, or live controller.

## Design Decisions

- Add `redeem slice manage` as the canonical foreground command.
- Reuse the installed Bubble Tea dependency but not the restore-specific `internal/tui` model.
- Allow multiple management windows; do not add focus/reuse policy.
- Use user-facing labels while retaining controller JSON terminology and semantics.

## Implementation Notes

Likely new files: internal/slicetui/{model,app,client}.go and tests; dispatch/help in cmd/redeem/main.go. Respect MaxControlResponseBytes and surface response_too_large rather than falling back to direct store reads.


## Completion Summary

- Added `redeem slice manage` and a separate controller-socket-only Bubble Tea management UI.
- Implemented live polling, authoritative actions, stable cursor identity, named/unnamed grouping, orphan close rows, and independent state/conflict axes.
- Added bounded cell-aware viewport rendering that preserves status facts before truncating names.
- Made management socket-path derivation filesystem-pure and covered unavailable/oversized/error states.
- Added fake-client, CLI, resize, Unicode, long-name, action, polling, and race tests; full Go suite passes.
- Two review rounds identified and verified fixes; final review is clean.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- go.mod
- internal/slicecontroller/store.go
- internal/slicetui/app.go
- internal/slicetui/app_test.go
- internal/slicetui/client.go
- internal/slicetui/model.go
- internal/slicetui/model_test.go
