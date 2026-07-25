---
title: Decide cross-machine Niri spatial mapping policy
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-4
---

## Goal

Choose deterministic mapping and degradation rules for host Niri workspaces and placement when the leech has different workspace names, outputs, display geometry, scale, or orientation.

## Acceptance Criteria

- Define precedence and fallback among workspace names, indices, and output-qualified indices.
- Define explicit output mapping for renamed/missing outputs, laptop-only or docked layouts, and many-to-one mappings.
- Classify tiled/floating state, relative order, dimensions, focus, and raw coordinates as required, best-effort, or unsupported.
- Make differing scale, resolution, orientation, and display counts deterministic and observable rather than replaying unsafe raw coordinates.
- State whether host placement is authoritative and how leech-local moves and conflicts behave.
- Define observable reversible fallback for missing workspaces or outputs.
- Produce semantics precise enough for hermetic planner/reconciler fixtures.

## Design Decisions

- Spatial correspondence is semantic mapping into leech-local Niri, not raw coordinate cloning.
- Niri runtime IDs cannot cross machines.
- Placement failures are degraded results and never affect host session ownership.

## Implementation Notes

Depends on the domain ADR. Recommendation: host placement authoritative on host changes; exact workspace name mapping first, then explicit output+index mapping, then a configured fallback; preserve tiled/floating and relative order where possible, scale dimensions best-effort, never replay raw pixels.

## Questions

### Q1: Should workspace name or index be the primary key? Recommendation: configured/name mapping, then output+index, then explicit index fallback.

### Q2: How should host outputs map to leech outputs across docked/undocked topologies? Recommendation: explicit per-topology mapping with a declared fallback.

### Q3: Which placement properties are required? Recommendation: workspace and tiled/floating when supported; order and scaled dimensions best-effort; focus/raw pixels initially unsynchronized.

### Q4: How should differing displays degrade? Recommendation: preserve semantic workspace/order and fall back visibly rather than clone coordinates.

### Q5: Is host placement authoritative over leech-local moves? Recommendation: apply host move events, but do not continually fight a leech-only move until host placement changes.

### Q6: May the controller create missing leech workspaces? Recommendation: map only to existing/configured targets initially.
