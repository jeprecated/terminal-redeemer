---
title: Prove host-leech slices with hermetic adversarial tests
priority: high
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-7
---

## Goal

Build a deterministic cross-layer matrix for inventory, identity, selection, exact attachment, RPC replay, reconciliation, placement, routed launch, reconnect, and ownership safety without live desktops, networks, credentials, or private state.

## Acceptance Criteria

- Cover zero, one, and many eligible terminals plus explicit missing/dead/resurrectable, duplicate, ambiguous-prefix, and one-window/one-session conflicts.
- Prove the hard-link private socket directory, empty shim cache, nested-environment scrub, pinned `on_force_close=detach`, path bounds, stale-directory GC, and no-resurrection/no-prefix exact attach contract with flake-locked Zellij.
- Cover Niri event-stream initial replay through `ConfigLoaded`, separate Outputs join, transient cross-reference inconsistency, source epoch rotation, complete/degraded revisions, stale/out-of-order replay, and full resync.
- Cover selected workspaces, pickup, close, explicit reopen, undo, dynamic add/remove/change/move, repeated revisions, duplicate entries/windows, controller restart, and source epoch replacement.
- Distinguish healthy manual close from disconnect; prove bounded reconnect/backoff, successful in-window recovery, retry exhaustion into stable disconnected state, and explicit reconnect afterward.
- Prove degraded or partial inventory cannot authorize destructive closes and consecutive complete absence/grace is required.
- Cover hostile metadata across JSON, argv, titles, CWD, session names, identities, revisions, app IDs, token journals, and state files.
- Prove status/close/drop affect only windows with app-ID, persisted-state, and process evidence and never emit remote termination.
- Cover one-monitor equal/differing-resolution workspace creation, duplicate/case-colliding names, exact membership, tiled/floating state, proportional size, initial-order best effort, and order-drift reporting; do not require exact live reorder in MVP.
- Cover host-location workspace/floating/size authority and rollback, reject stale schema-2 leech-location/writeback configuration, and exclude order writeback.
- Cover routed launch mode off, selected/unselected workspaces, one token/one host terminal, token replay, lost success response, partial host creation, projection confirmation delay, bounded disconnect, explicit reconnect, cancellation, and absence of automatic local fallback under uncertainty.
- Keep legacy one-shot interactive attach tests green and prove pinned-Zellij watch returns a clear unsupported outcome.
- Require `go test ./...` and flake checks without live Wayland or network access, with separately documented operator smoke tests for actual Niri mutation visibility and end-to-end transport.

## Design Decisions

- Use fake argv/socket runners, Niri JSON event fixtures, temporary state, deterministic clocks/backoff, and flake-locked packages.
- Real-Zellij checks prove the exact supported production contract.
- Exact live order and watch mode are not MVP proof requirements.

## Implementation Notes

Depends on all owning implementation tasks and both spike tasks. Develop focused tests alongside each owner, then close cross-layer gaps here. Block rollout rather than weakening safety assertions if pinned tools contradict the contracts.

## Outcome

Prove host-leech slices with hermetic adversarial tests was implemented and validated within the recorded acceptance boundaries.
