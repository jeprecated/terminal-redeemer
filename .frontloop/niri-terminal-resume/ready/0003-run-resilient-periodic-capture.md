---
title: Run resilient periodic capture
priority: high
---

## Goal

Use a configurable systemd timer to capture complete Niri and terminal state at a simple, dependable cadence, with a one-minute distributed default.

## Acceptance Criteria

- Capture performs a complete Niri windows/workspaces and terminal-metadata reconciliation on every timer activation.
- The distributed default interval is 60 seconds and remains configurable.
- The timer waits for the graphical session/Niri IPC and failures are visible in the journal; a failed run does not corrupt history and the next interval can recover.
- `capture once` remains available and uses the same full-capture path for an immediate checkpoint.
- No long-running Niri event-stream subscriber, debounce loop, or reconnect state machine is introduced.
- Home Manager configures the oneshot capture service and periodic timer with appropriate lifecycle and persistence behavior.
- Timer cadence, failed-run recovery, full reconciliation, and module-evaluation tests pass.
- `go test ./...` and `nix flake check 'path:.'` pass.

## Design Decisions

- The recovery point objective is one configured capture interval plus normal scheduling delay.
- The distributed default capture interval is 60 seconds.
- Only Terminal Redeemer writes its capture history; concurrent writers fail visibly.
- Capture and restore remain separate responsibilities.

## Implementation Notes

Primary areas: internal/capture, internal/niri, cmd/redeem, internal/config, modules/home-manager, modules/nixos, and docs/CONFIG.md. Preserve fixture-driven tests without requiring a live compositor. The existing Home Manager oneshot service/timer is the preferred base; harden it rather than replacing it with an event-stream daemon. ADR: docs/adr/0001-resume-zellij-terminals-in-niri.md.
