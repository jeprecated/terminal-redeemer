---
title: Prove host-leech slices with hermetic adversarial tests
priority: high
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-7
---

## Goal

Build a deterministic test matrix for inventory, selection, transport, attachment, reconciliation, placement, reconnect, and ownership safety without live desktops, networks, credentials, or private state.

## Acceptance Criteria

- Cover zero, one, and many eligible terminals plus multiple windows attached to one Zellij session.
- Cover pickup/drop/undo, dynamic add/remove/change/move, repeated revisions, duplicate entries/windows, manual local close, and source epoch replacement.
- Cover timeout, disconnect, bounded backoff, stale/out-of-order revisions, full resync, restart, and duplicate-free recovery.
- Cover malformed inventory, failed attach/launch/move/close, unavailable source, partial failures, and cancellation without corrupting independent items or state.
- Cover hostile metadata across JSON, remote argv, titles, CWD, session names, identities, revisions, and state files.
- Prove nested-Zellij environment scrubbing.
- Use the flake-locked Zellij package to prove exact concurrent interactive attach and no-create-on-missing semantics.
- Prove status/drop/close affect only owned local slice windows and never emit remote termination.
- Cover selected spatial mapping and degraded display topologies.
- Keep legacy one-shot tests green; defer watch unless separately proven.
- Require go test and flake checks without live Wayland or network access.

## Design Decisions

- Use fake argv runners, synthetic Niri JSON, temporary state, deterministic clocks/backoff, and flake-locked packages.
- Real-Zellij checks prove the exact supported production contract.
- Watch mode is not required for first rollout.

## Implementation Notes

Depends on both decisions and protocol, transport, and reconciliation implementation. Develop tests alongside owning tasks, then close cross-layer gaps here. Block rollout rather than weakening assertions if pinned Zellij contradicts the plan.
