---
title: Decide share and slice selection and persistence semantics
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-2
---

## Goal

Resolve how the host exposes windows, how a leech picks them up into named slices, how state persists, and how newly opened eligible windows become included.

## Acceptance Criteria

- Choose the selection unit and cover multiple open host windows attached to one Zellij session.
- Assign authoritative storage and mutation ownership for host share state and leech slice state.
- Define deterministic pickup, drop, undo, local close, source disappearance, and reappearance semantics.
- Define how newly opened eligible host windows become shared or enter an already selected slice without silently broadening exposure.
- Specify naming, deletion, empty-slice behavior, persistence, and initial single-host/single-leech scope.
- Record the decision in or alongside the domain ADR and preserve future rule-based selection as an additive extension.

## Design Decisions

- Selection concerns open shared host terminal windows; headless sessions are not an initial source.
- Leech-local operations cannot mutate or terminate host sessions.
- Exposure and pickup must remain explicit, inspectable, and reversible.

## Implementation Notes

Depends on the domain ADR. Recommendation: named slices persisted on the leech, explicit membership by stable shared-window identity, host-owned share allowlist, and optional matching rules only in a later schema.

## Questions

### Q1: Should initial membership select each open Kitty window, all windows for a selected Zellij session, or support both? Recommendation: individual stable windows, grouped by session for presentation.

### Q2: Should the host own share declarations while each leech owns named slice membership? Recommendation: split authority this way.

### Q3: Should named membership survive restart/reboot, and should undo history persist? Recommendation: persist membership atomically; keep bounded undo in memory initially.

### Q4: When a source window disappears and a similar one returns, should membership automatically rebind? Recommendation: tombstone and require explicit pickup unless a future opt-in rule matches.

### Q5: Should newly opened windows require explicit inclusion or support automatic rules? Recommendation: explicit inclusion first; optional allowlisted rules later.

### Q6: Should the first release support one host and one leech per controller configuration? Recommendation: yes, while namespacing identities for future expansion.
