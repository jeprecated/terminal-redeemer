---
title: Publish slice readiness, migration, rollback, and consumer contract
priority: high
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-8
---

## Goal

Document product/operator behavior, schema migration and rollback, a bounded smoke matrix, and an immutable consumer contract suitable for a later separately approved mono-nix pin update.

## Acceptance Criteria

- Document terminology, selected-workspace behavior, per-window pickup/drop overrides, Leech mode, routed Super+Enter launch, host-location/leech-location authority, controller behavior, statuses, timing, security, troubleshooting, and initial non-goals without overstating support.
- Document additive schema/state migration, legacy coexistence, upgrade order, downgrade, backup, rollback, and controller disablement without deleting host sessions.
- Provide an operator smoke matrix for selected workspaces, automatic new host windows, routed terminal creation with fallback, one-window/one-session enforcement, pickup/drop/undo, both layout-authority modes, workspace creation, order/size correspondence, concurrent interaction, local close, reconnect, duplicate suppression, and ownership-safe close.
- Require hermetic tests plus an explicitly operator-run non-secret smoke check; automated work does not activate hosts or inspect live sessions.
- Expose a documented package/module/config/schema contract and immutable release or commit reference for later consumer pinning.
- Identify legacy one-shot behavior, opt-in replacement, clipboard-off rollout, limitations, and proof required before retiring legacy behavior.
- Ensure rollback leaves host sessions and unrelated local windows untouched.
- Do not edit mono-nix or activate Lattice/Overton.

## Design Decisions

- This repo publishes a consumer-ready immutable contract but does not update consumer pins.
- Legacy one-shot behavior remains until replacement readiness and rollback are proven.
- First rollout is opt-in, bounded, and clipboard-disabled.

## Implementation Notes

Depends on all preceding tasks. Update public docs, ADR/protocol references, schema fixtures, module examples, release metadata, and flake outputs. End with an immutable ref and consumer settings contract for a later mono-nix proposal.
