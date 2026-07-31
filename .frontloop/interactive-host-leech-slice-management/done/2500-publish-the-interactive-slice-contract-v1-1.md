---
title: Publish the interactive slice contract v1.1
priority: high
frontloop_approval_task: 825d83f26e1a3b5c4d26b920878c36f468ed5ae1b24877774b5dc032ea3e978b-1
---

## Goal

Record the additive all-eligible selection and live-management semantics before implementation, and align the currently conflicting or misleading operator documentation.

## Acceptance Criteria

- A new decision record defines `(all eligible OR selected workspace OR exact pickup) AND NOT closed_by_user`, with all-eligible covering current/future eligible sources including unnamed host workspaces.
- The decision states that all-eligible affects projection desire only; routed Super+Enter remains controlled by explicit named-workspace selection and separate Leech mode.
- Unnamed sources are defined as attachable but not cross-machine spatially placeable, appear in an explicit `(unnamed)` management group, and must not create spatial conflict churn.
- The existing controller schema remains 2 with optional `all_eligible`; the in-place consumer contract version becomes 1.1.0 and documents upgrade/downgrade behavior.
- Downgrade instructions require disabling all-eligible while the current controller runs, stopping the service, then downgrading; global toggles leave no incompatible undo residue, and skipping the step is documented as fail-closed unknown-field/service failure.
- Contract JSON, strict schema, flake assertions, consumer-contract tests, ADRs, protocol/config/operations/readiness docs, and README agree on commands, module exports, disabled defaults, and unchanged inventory/RPC/exact-attachment semantics.
- ADR 0004's status is reconciled with its shipped use, and README clearly distinguishes legacy one-shot mirror, continuous slice management, and unsupported watch mode.

## Design Decisions

- Amend the existing v1 contract in place from 1.0.0 to 1.1.0; do not create a parallel contract directory.
- Keep inventory schema 1, RPC schema 1, and controller schema 2.
- Use an optional `all_eligible` boolean rather than a synthetic workspace key.
- Do not add named slices, watch, multi-monitor, clipboard sync, leech writeback, or automatic binding installation.

## Implementation Notes

Relevant evidence: docs/adr/0002-0004, docs/PROTOCOL.md, docs/OPERATIONS.md, docs/HOST_LEECH_READINESS.md, contracts/host-leech-slices/v1, .frontloop/host-leech-terminal-slices. Fable review: /tmp/terminal-redeemer-fable-review.md.


## Completion Summary

- Published ADR 0005 and amended ADRs 0002/0003 with additive all-eligible/live-management semantics.
- Marked ADR 0004 accepted for its shipped disabled-by-default spatial policy.
- Bumped the in-place strict consumer contract and package metadata to 1.1.0 while retaining inventory/RPC/controller schemas 1/1/2.
- Documented unnamed-source no-spatial behavior, routed-launch separation, safe downgrade order, fail-closed prior-reader behavior, disabled defaults, and unchanged non-goals.
- Added strict contract mutations/runtime assertions and a frozen v1.0-reader compatibility test.
- Expanded the mandatory physical-smoke table with every v1.1 observation; focused/full Go and strict contract/hermetic Nix checks pass.
- Independent review found the final contract/readiness changes clean.

### Files Changed

- README.md
- contracts/host-leech-slices/v1/consumer-contract.json
- contracts/host-leech-slices/v1/consumer-contract.schema.json
- contracts/host-leech-slices/v1/niri-bindings.kdl.in
- docs/CONFIG.md
- docs/HOST_LEECH_READINESS.md
- docs/OPERATIONS.md
- docs/PROTOCOL.md
- docs/adr/0002-host-leech-terminal-slice-domain-and-lifecycle.md
- docs/adr/0003-terminal-slice-workspace-sharing-and-persistence.md
- docs/adr/0004-single-monitor-niri-spatial-mapping-policy.md
- docs/adr/0005-global-slice-selection-and-live-management.md
- flake.nix
- internal/consumercontract/contract_test.go
- internal/slicecontroller/legacy_reader_compat_test.go
- modules/home-manager/terminal-redeemer.nix
