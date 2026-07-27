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

## Outcome

Publish corrected v1 readiness, rollback, and consumer contract was implemented and validated within the recorded acceptance boundaries.
