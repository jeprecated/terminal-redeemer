---
title: Prove Niri direct-IPC inventory and safe mutations
priority: critical
---

## Goal

Prove the exact pinned-Niri mechanisms for complete host inventory, source-instance detection, workspace creation, targeted placement, proportional sizing, and non-disruptive MVP mutation boundaries before protocol or controller implementation.

## Acceptance Criteria

- Use a bounded Niri 25.11 headless/live harness to consume the event-stream initial replay through the final ConfigLoaded sentinel.
- Query Outputs separately, join it to workspace/window state, and define validation for transient or dangling cross-references.
- Prove source-instance detection from boot identity plus private NIRI_SOCKET filesystem identity without serializing socket values.
- Prove missing named-workspace creation by naming the trailing empty workspace through an ID reference, including duplicate and case-insensitive-name behavior and verify-after-write.
- Prove exact-ID workspace move with focus disabled, tiled/floating mutation, and proportional width/height mutation with bounded observation after every action.
- Confirm that exact existing-column reorder has no non-focus target in Niri 25.11 and record it as a stretch-only focus-dance operation.
- Record exact request/response shapes, timeout behavior, and a recommended Go direct-socket adapter contract without retaining synthetic Niri state fixtures.
- Block downstream inventory/spatial implementation if complete-snapshot or mutation safety cannot be proven.

## Design Decisions

- The host consumes Niri's local event stream but publishes revisioned full snapshots to the leech; leech-facing event streaming is deferred.
- Workspace names are cross-machine identity; runtime workspace IDs are used only for same-epoch mutations.
- MVP supports workspace, tiled/floating, and proportional size projection; exact live order synchronization is stretch-only.
- Every mutation is verified after application because some Niri failures are silent.

## Implementation Notes

This is an implementation gate for inventory, spatial policy, reconciliation, and routed launch. Pinned source review found the initial replay is emitted under one state borrow, ConfigLoaded is the final replay sentinel, Outputs requires a separate request, and naming the trailing empty workspace creates a replacement empty workspace.

## Outcome

The synthetic Niri state fixtures and obsolete spike test wrapper were deleted.
Production client tests and the locked executable/version path remain, while the
live script is reproducible through the locked `niri-spike` development shell.
