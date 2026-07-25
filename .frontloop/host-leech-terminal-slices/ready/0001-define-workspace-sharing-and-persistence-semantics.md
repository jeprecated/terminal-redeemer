---
title: Define workspace sharing and persistence semantics
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-2
---

## Goal

Codify the operator-approved model in which the leech can browse any eligible open host terminal, automatically project selected named workspaces, and reversibly pick up or drop individual projected terminals.

## Acceptance Criteria

- Define and enforce the invariant that one eligible host Kitty window maps to exactly one Zellij session; duplicate windows for one session or one window without a unique session are explicit conflicts rather than silently merged.
- Make every eligible open host Kitty/Zellij window discoverable to the leech without a host-side publish or allowlist step.
- Persist the leech's selected set of static Niri workspace names; adding a workspace begins automatic projection and removing it drops only the leech projections.
- Automatically include every eligible host terminal currently in a selected workspace and every eligible terminal later opened there.
- Permit explicit pickup of an individual eligible terminal outside the selected workspace set.
- Permit explicit drop and undo for an individual projection without closing Kitty or terminating Zellij on the host; represent drops as leech-local exclusions from automatic workspace projection.
- Define deterministic behavior for host window close, workspace move, temporary disappearance, controller restart, and later new windows with new stable identities.
- Persist selected workspaces and explicit per-window pickup/drop overrides atomically; keep history bounded and inspectable.
- Scope the first controller configuration to one host and one leech while namespacing identities for future expansion.
- Record these decisions in or alongside the domain ADR and remove obsolete named-slice/host-publish assumptions.

## Design Decisions

- One Kitty window maps to one Zellij session.
- The host need not publish terminals; all eligible open host terminals are available for leech discovery.
- The primary automatic selection unit is a static Niri workspace name.
- The live slice is the projection of selected workspaces plus explicit per-window pickup/drop overrides; named slices are not required initially.
- New eligible host windows in selected workspaces appear automatically.
- Leech-local drop, close, undo, or controller exit never mutates or terminates host work.
- Workspace and override state persist until the operator changes it.

## Implementation Notes

Depends on the domain ADR. Keep host session lifecycle authoritative and leech selection state local. A source window that closes leaves the projection; a newly created window receives a new stable identity and is projected if its workspace is selected. This task defines semantics only and does not edit mono-nix or activate machines.
