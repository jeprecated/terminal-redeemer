---
title: Define single-monitor Niri spatial mapping policy
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-4
---

## Goal

Codify deterministic spatial projection for the initial one-monitor host and one-monitor leech topology, using static Niri workspace names and explicit host-location versus leech-location authority modes.

## Acceptance Criteria

- Use static Niri workspace names as the primary and required cross-machine workspace identity.
- Limit the first release to one active monitor/output on the host and one on the leech; multi-monitor and docked topology mapping remain explicit future work.
- Create a missing selected workspace on the leech when the host workspace exists, and create the corresponding host workspace when an authorized leech-location operation requires it.
- Project workspace membership, terminal order, tiled/floating state, and terminal size for every selected workspace.
- Define size correspondence in Niri logical/normalized layout terms so differing single-monitor resolutions do not cause unsafe raw-pixel replay.
- Define host-location mode: host placement remains authoritative for shared state, while leech-only rearrangement is local and is never written back to the host.
- Define leech-location mode: authorized leech workspace/order/size changes are persisted onto the matching host window with loop prevention, conflict reporting, and ownership checks.
- Define deterministic mode switching, initial synchronization direction, concurrent move conflict handling, failure/degraded outcomes, and rollback to host-location mode.
- Ensure placement failure never changes Zellij ownership, closes host work, or moves unrelated Niri windows.
- Produce semantics precise enough for hermetic equal-resolution and differing-resolution single-monitor fixtures.

## Design Decisions

- Both initial machines use one monitor; output-to-output mapping is therefore implicit and one-to-one.
- Workspace names are static and authoritative across machines.
- Workspace, order, and size must correspond for projected terminals.
- Missing named workspaces may be created.
- Host-location mode never propagates leech placement changes to the host.
- Leech-location mode propagates authorized leech placement changes to the host.
- Raw Niri runtime IDs and raw physical pixels do not cross machines.

## Implementation Notes

Depends on the domain ADR. Multi-monitor output mapping, dock/undock transitions, and many-to-one output projection are deferred. Separate pure layout mapping from Niri mutations and attach mode/authority metadata to every proposed move so feedback loops are testable and preventable.
