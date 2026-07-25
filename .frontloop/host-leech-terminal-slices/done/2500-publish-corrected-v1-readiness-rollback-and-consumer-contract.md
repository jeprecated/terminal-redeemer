---
title: Publish corrected v1 readiness, rollback, and consumer contract
priority: high
frontloop_approval_task: b8392b2aa50f804dc7f7fe8e05230b9ff1a8e118ae81ce3c82c52657d40afa61-4
---

## Goal

Publish the final host-location-only v1 contract after all corrective implementation and validation evidence exists, replacing the blocked stale publication task.

## Acceptance Criteria

- Operator documentation covers selected workspaces, pickup/drop/reopen/undo, routed launch, controller states, timing, security, exact attachment, ownership, and complete/degraded observation semantics using the final contracts.
- Documentation clearly says v1 is host-location only, supported local layout drift is reverted, order remains observation-only, and leech-location is deferred/non-configurable.
- Documentation explains session-keyed drop lifetime, inspectability, bounded confirmed-absence expiry, and the explicit pre-release backup/re-enrolment procedure.
- Minimum-client-grid Zellij reflow, approximate proportional placement, pinned-version coupling, transport ambiguity, and launch-correlation races are stated as residual limitations.
- Migration, downgrade, disablement, and rollback leave host Zellij sessions, pending routed launches, unrelated local windows, and legacy one-shot attachment untouched.
- The smoke matrix exercises the corrected v1 behavior and records hermetic validation plus explicit non-secret operator checks without activation or credential inspection.
- Package, module, configuration, schema, controller, keybinding, and generated Niri integration contracts are documented at an immutable reference suitable for a later separately approved consumer pin.
- mono-nix, Lattice, and Overton are not edited or activated.

## Design Decisions

- The repo publishes a consumer-ready immutable contract but does not update consumer pins.
- Legacy one-shot mirror/attach behavior remains until separately proven replacement readiness and rollback.
- Clipboard remains disabled for first rollout.

## Implementation Notes

Replace rather than follow the blocked stale publication assumptions. Execute only after the prior three corrective tasks complete.


## Completion Summary

- Published the corrected host-location-only v1 candidate contract with controller schema 2 and immutable content reference sha256:28ace3a2f6bd5d869e61802dff2300a8808df0817f21af2bebcaed3fd7d78a69.
- Regenerated and strictly validated contract schemas, release metadata, flake library/package exports, generated Niri bindings, and package artifact membership.
- Completed operator readiness, migration/re-enrolment, rollback, smoke, security, exact-attachment, residual-limitation, and legacy-coexistence documentation without activation.
- Kept repository commit/release null, immutable source pin unpublished, candidate inactive, clipboard off, and consumer approval separate.
- Aligned ADR/protocol implementation status and the 107-byte pathname contract; removed the obsolete blocked publication task.
- Full Go/race/vet/module verification, hermetic matrix, locked Niri/Zellij spikes, package/module/consumer builds, offline flake checks, links/citations, and final independent publication review passed with no blockers.

### Files Changed

- contracts/host-leech-slices/v1/consumer-contract.json
- contracts/host-leech-slices/v1/consumer-contract.schema.json
- contracts/host-leech-slices/v1/release-metadata.json
- contracts/host-leech-slices/v1/release-metadata.schema.json
- contracts/host-leech-slices/v1/niri-bindings.kdl.in
- scripts/tests/host-leech-consumer-contract.py
- internal/consumercontract/contract_test.go
- flake.nix
- README.md
- docs/CONFIG.md
- docs/HOST_LEECH_READINESS.md
- docs/PROTOCOL.md
- docs/OPERATIONS.md
- docs/adr/0002-host-leech-terminal-slice-domain-and-lifecycle.md
- docs/adr/0003-terminal-slice-workspace-sharing-and-persistence.md
- docs/spikes/0001-zellij-live-only-attachment.md
- scripts/spikes/zellij-live-only-attachment.sh
