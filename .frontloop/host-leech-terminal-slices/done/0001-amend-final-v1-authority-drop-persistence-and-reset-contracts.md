---
title: Amend final v1 authority, drop-persistence, and reset contracts
priority: critical
frontloop_approval_task: b8392b2aa50f804dc7f7fe8e05230b9ff1a8e118ae81ce3c82c52657d40afa61-1
---

## Goal

Correct the accepted ADRs, protocol, configuration, operations guidance, and queued publication criteria so they reflect the operator-approved v1 semantics before more completion claims are accepted.

## Acceptance Criteria

- ADR 0002 and ADR 0004 state that v1 exposes host-location only, supported leech workspace/floating/width/height drift is reverted, order drift is report-only, and leech-location remains a non-shipping v1.1 specification.
- ADR 0003 and PROTOCOL.md key manual drop overrides by exact verified Zellij session identity, preserve them across source epochs, keep them inspectable, and expire them only after bounded confirmed session absence or explicit reopen/undo.
- The complete inventory contract includes additive verified-live-session evidence sufficient to distinguish a headless live session from true confirmed absence; degraded, stale, duplicate, and retired-epoch observations cannot authorize expiry.
- CONFIG.md and OPERATIONS.md expose no leech-location v1 configuration path and explain that host-location convergence intentionally reverts supported local layout edits.
- Document the approved pre-release transition: back up and explicitly re-enrol slice controller authority; never silently reset state, delete host sessions, or touch unrelated configuration.
- Revise the blocked publication criteria and remaining test matrix to remove both-authority-mode v1 claims and include the final persistence and convergence semantics.

## Design Decisions

- Explicit pre-release controller re-enrolment is acceptable; no one-off in-place migration is required.
- Pure leech-location policy code may remain only as clearly dormant executable v1.1 specification, with no reachable v1 config or runtime wiring.
- Legacy one-shot attach remains available until replacement readiness is proven.

## Implementation Notes

Target docs/adr/0002-0004, docs/PROTOCOL.md, docs/CONFIG.md, docs/OPERATIONS.md, and the blocked publication/test task text. Preserve all existing safety invariants not contradicted by the final decisions.


## Completion Summary

- Aligned ADRs 0002-0004, protocol, configuration, operations, readiness, and test traceability with host-location-only v1 semantics.
- Documented session-keyed drop persistence across epochs/headless sessions, early reopen/undo, and automatic expiry only after accepted complete absence confirmations plus committed grace.
- Documented verified live-session inventory evidence and non-destructive degraded/replay/conflict handling.
- Documented explicit owner-only backup and pre-release controller re-enrolment without host-session or unrelated-state deletion.
- Corrected the blocked publication criteria and remaining adversarial matrix to remove both-mode v1 claims and require host convergence, leech-config rejection, and final drop semantics.
- Targeted stale-language and contract-trace checks passed.

### Files Changed

- docs/adr/0002-host-leech-terminal-slice-domain-and-lifecycle.md
- docs/adr/0003-terminal-slice-workspace-sharing-and-persistence.md
- docs/adr/0004-single-monitor-niri-spatial-mapping-policy.md
- docs/PROTOCOL.md
- docs/CONFIG.md
- docs/OPERATIONS.md
- docs/HOST_LEECH_READINESS.md
- docs/testing/host-leech-hermetic-matrix.md
- .frontloop/host-leech-terminal-slices/clarify/publish-slice-readiness-migration-rollback-and-consumer-contract.md
