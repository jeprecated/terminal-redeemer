---
title: Codify the host-leech terminal-slice domain and lifecycle
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-1
---

## Goal

Add a domain ADR that gives host/workhorse, leech/workstation, share, slice, spatial projection, ownership, lifecycle, connection state, and interactive attachment one precise meaning before protocol or controller work begins.

## Acceptance Criteria

- Define the host as the authoritative machine where Niri, Kitty, Zellij, and agent sessions run, and the leech as the machine rendering and interacting with selected projections.
- Define all eligible open host Kitty windows backed one-to-one by verified live Zellij sessions as discoverable resources without a host publish step.
- Define the live slice as selected static workspace names plus explicit per-window pickup/close/reopen overrides; named slices are not required initially.
- Define Leech mode, host-location mode, leech-location mode, spatial projection, fidelity, degraded outcomes, ownership, pickup, close, reopen, undo, and lifecycle without implying screen streaming.
- Distinguish `connected`, bounded `reconnecting`, stable `disconnected`, and `closed_by_user`; retry exhaustion stops automatic attempts until explicit reconnect.
- State that Super+W or another local projection close detaches only the leech view, never closes the host Kitty window or terminates/creates/resurrects the host Zellij session, and leaves the source discoverable for explicit reopen.
- State that concurrent host/leech attachment and different client sizes are accepted, including Zellij's shared minimum-grid behavior.
- Define routed launch as one idempotent host-terminal creation followed by repeated connection to that same session; uncertain host outcome becomes pending/disconnected and never causes automatic local fallback.
- Scope MVP spatial fidelity to workspace, tiled/floating state, proportional size, initial-order best effort, and order-drift reporting; exact live order synchronization is stretch-only.
- Record that the host may consume Niri's local event stream while the leech-facing protocol remains revisioned full snapshots with bounded polling.
- Record explicit non-goals: screen/video streaming, initial headless-session inventory, arbitrary GUI projection, first-rollout clipboard synchronization, multi-monitor topology, mono-nix edits, and host activation.
- State that pinned Zellij has no supported `watch` subcommand; preserve proven one-shot interactive attach behavior but do not claim watch support.
- Keep prior-boot resume and live host-leech projection as separate domains.

## Design Decisions

- Lattice is the host/workhorse and remains authoritative; Overton is the leech/workstation.
- The primary resource is an open host Kitty window with exactly one verified live Zellij session, not every headless or resurrectable session.
- Static workspace names select automatic projection; individual terminals may be picked up, closed, and reopened as overrides.
- Closing or disconnecting a projection never owns or terminates host work.
- Concurrent attachments and size differences are accepted for MVP.
- Routed launches never auto-fallback locally when host creation may have succeeded.
- Exact live order synchronization is a stretch goal.
- The initial topology is one monitor on each machine.

## Implementation Notes

Depends on no other task. Incorporate the evidence and final decisions from the Niri and Zellij spike tasks as an appendix before the ADR is accepted. Produce a public ADR under docs/adr and minimal navigation updates only.


## Completion Summary

- Added ADR 0002 defining host/leech authority, eligible live sources, share/slice selection, ownership, spatial projection, routed launch, and separate prior-boot/live-projection domains.
- Separated host-source, leech-desire, attachment-connection, and routed-launch-intent axes, including bounded reconnect exhaustion and explicit closed-by-user reopen semantics.
- Recorded MVP spatial fidelity, event-stream/full-snapshot boundaries, unsupported Zellij watch behavior, explicit non-goals, and executable spike evidence with limitations.
- Added minimal README ADR navigation and passed independent subagent review after resolving lifecycle, reopen, and degraded-fidelity findings.

### Files Changed

- docs/adr/0002-host-leech-terminal-slice-domain-and-lifecycle.md
- README.md
