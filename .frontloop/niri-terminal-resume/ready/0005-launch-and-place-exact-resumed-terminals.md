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
