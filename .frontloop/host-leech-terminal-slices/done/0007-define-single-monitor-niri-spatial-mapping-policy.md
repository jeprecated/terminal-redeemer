---
title: Define single-monitor Niri spatial mapping policy
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-4
---

## Goal

Codify deterministic, non-disruptive MVP spatial projection for one host monitor and one leech monitor using static workspace names and explicit host-location versus leech-location authority modes.

## Acceptance Criteria

- Depend on the passing Niri direct-IPC spike and use its exact request, mutation, and verify-after-write contracts.
- Use case-normalized static Niri workspace names as the required cross-machine workspace identity; runtime workspace IDs are same-epoch mutation targets only.
- Limit MVP to one active output on the host and one on the leech; multi-monitor and docked topology mapping remain future work.
- Create a missing named workspace by naming the exact trailing empty workspace by runtime ID, then verify the result and replacement empty workspace; reject duplicate/case-colliding names explicitly.
- Project workspace membership, tiled/floating state, and proportional terminal width/height using exact window IDs and Niri working-area percentage actions where available.
- Carry output logical dimensions/scale and report that cross-machine size remains approximate when exclusive zones or font cell grids differ.
- Observe exact source `(column,tile)` order, preserve initial launch order where practical, and report later order drift without automatic correction.
- Define host-location mode: host placement seeds and updates authoritative shared properties, while leech-only rearrangement remains local and is never written back automatically.
- Define leech-location mode: authorized leech workspace/floating/size changes may be written to the matching host window with ownership checks, origin metadata, loop prevention, and conflict reporting; column-order writeback is excluded from MVP.
- Define deterministic mode switching, initial synchronization direction, concurrent property conflict handling, failure/degraded outcomes, and rollback to host-location mode.
- Ensure placement failure never changes Zellij ownership, closes host work, focuses unrelated windows, or moves unrelated Niri windows.
- Produce hermetic equal-resolution and differing-resolution fixtures plus live operator smoke criteria.

## Design Decisions

- Output mapping is implicit and one-to-one for MVP.
- Raw Niri IDs and raw physical pixels do not cross epochs or machines as durable identity.
- Exact live column-order synchronization, including any focus-dance implementation, is a stretch goal.
- Concurrent host/leech Zellij client sizing differences are accepted and do not block spatial projection.
- Every mutation is verified after application because Niri may silently ignore some requests.

## Implementation Notes

Depends on the domain ADR and passing Niri spike. Separate pure layout mapping from Niri mutations and attach authority/origin metadata to every proposed write so feedback loops are testable.

## Outcome

Define single-monitor Niri spatial mapping policy was implemented and validated within the recorded acceptance boundaries.
