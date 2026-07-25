---
title: Define workspace sharing and persistence semantics
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-2
---

## Goal

Codify the operator-approved model in which the leech can browse any eligible open host terminal, automatically project selected named workspaces, explicitly pick up or close individual projections, and reconnect only under bounded policy.

## Acceptance Criteria

- Enforce that one eligible host Kitty window maps to exactly one verified live Zellij session; missing, dead/resurrectable, duplicate, or ambiguous bindings are explicit conflicts.
- Make every eligible open host Kitty/Zellij window discoverable to the leech without a host-side publish or allowlist step.
- Persist the selected set of case-normalized static Niri workspace names; adding a workspace begins automatic projection and removing it closes only leech projections.
- Automatically include every eligible host terminal currently in a selected workspace and every eligible terminal later opened there.
- Permit explicit pickup of an eligible terminal outside the selected workspace set.
- Treat Super+W or another manual local close as `closed_by_user`: close only the leech projection, retain discovery, and require explicit reopen/undo rather than recreating it on the next poll.
- Distinguish manual close from transport loss. Transport loss enters bounded reconnect attempts, then a stable disconnected state that stops retrying until explicit reconnect.
- Permit automatic re-adoption of a uniquely matching source identity/session only when recovery occurs within the active retry window; after exhaustion, explicit reconnect is required.
- Define deterministic behavior for host window close, host Niri/source-epoch replacement, host workspace move, temporary disappearance, controller restart, and later new windows with new stable identities.
- Persist selected workspaces, closed/reopen overrides, reconnect state, source bindings, and routed-launch tokens atomically; keep history bounded and inspectable.
- Scope the first controller configuration to one host and one leech while namespacing identities for future expansion.
- Remove obsolete named-slice, host-publish, immediate-auto-reopen, and infinite-retry assumptions.

## Design Decisions

- The host need not publish terminals; all eligible open host terminals are available for leech discovery.
- The primary automatic selection unit is a case-normalized static Niri workspace name.
- New eligible host windows in selected workspaces appear automatically unless an exact carried-forward closed override applies during bounded recovery.
- Manual local close is persistent until explicit Terminal Redeemer reopen/undo, but never hides the source from inventory.
- Disconnect retries are bounded; retry exhaustion is stable and operator-resumable.
- Leech-local close, controller exit, or disconnect never mutates or terminates host work.

## Implementation Notes

Depends on the domain ADR and the resolved source-identity/epoch contract. Keep host session lifecycle authoritative and leech selection/connection state local. This task defines semantics only and does not edit mono-nix or activate machines.


## Completion Summary

- Added ADR 0003 defining publication-free discovery, canonical workspace selection, exact pickup/close/reopen semantics, and automatic inclusion of current and future eligible sources.
- Defined monotonic revision/epoch acceptance, deterministic degraded/absence/close/move/restart behavior, and three safe source-epoch replacement paths.
- Defined bounded recovery that survives restart without budget reset, explicit cross-epoch successor lineage, and a non-prunable exhausted-successor gate requiring explicit reconnect.
- Defined one logical atomic current-state boundary, safe first-time namespace initialization, namespaced routed tokens, and bounded inspectable audit/undo history that cannot prune active authority.
- Extended README ADR navigation and passed independent subagent review after resolving snapshot-ordering, successor-formula, epoch-cleanup, and initialization findings.

### Files Changed

- docs/adr/0003-terminal-slice-workspace-sharing-and-persistence.md
- README.md
