---
title: Align controller state, inventory, and reconciliation with final v1 contracts
priority: critical
frontloop_approval_task: b8392b2aa50f804dc7f7fe8e05230b9ff1a8e118ae81ce3c82c52657d40afa61-2
---

## Goal

Apply the corrected contracts to reachable v1 code while preserving the already-built ownership, recovery, cleanup, and monotonic-observation safety mechanisms.

## Acceptance Criteria

- Configuration and Home Manager module evaluation reject leech_location and leechWriteAuthorized=true for v1; no host spatial writeback path is reachable from supported configuration.
- Host-location reconciliation converges owned projections back to authoritative host workspace, tiled/floating state, proportional width, and proportional height; live order remains report-only and no focus-dance correction is introduced.
- Manual drops are persisted by verified Zellij session identity rather than source ID and survive same-session source replacement across epochs.
- A dropped session remains dropped while live but headless; expiry occurs only after the configured consecutive complete-session-absence evidence plus committed grace deadline.
- Degraded, duplicate, stale, or retired-epoch observations do not advance absence evidence or delete overrides.
- Controller status exposes persisted session-keyed drops and enough expiry evidence for diagnosis.
- The revised state schema fails closed on old experimental authority and provides the documented backup/re-enrolment path without deleting host sessions.
- Hermetic tests cover all corrected semantics and preserve positive Kitty ownership, exact attach, cleanup barriers, and host-session non-destruction.
- go test ./..., go test -race ./..., go vet ./..., module evaluation, and package/Nix checks pass.

## Design Decisions

- Reuse the verified SessionID based on boot ID plus Zellij socket identity and name.
- Include verified live-session IDs in complete inventory semantic hashing so catalog changes advance revisions.
- Keep exact existing-column reorder unsupported and non-mutating.

## Implementation Notes

Likely areas: internal/config, modules/home-manager, cmd/redeem, internal/slicecontroller, internal/slicelayout, internal/sourceinventory, and protocol/state fixtures. Treat previously completed task 0008 as substantial reusable implementation, not as a restart.


## Completion Summary

- Locked supported v1 configuration/runtime to host-location only and made host spatial writeback unreachable.
- Changed complete inventory to carry/hash verified live-session IDs and advance revision on every successful complete poll while degraded/replay/conflict observations remain non-authoritative.
- Migrated controller authority to schema 2 session-keyed drops that survive epochs/headless sessions and expire only after accepted complete absence confirmations plus committed grace.
- Made host-location reconciliation converge workspace, tiled/floating state, width, and height while keeping order report-only.
- Added explicit fail-closed pre-release backup/re-enrolment guidance and broad hermetic regression coverage.
- Resolved two independent review rounds; final reviewer reported PASS with no blockers.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- internal/config/config.go
- internal/config/slice_test.go
- internal/slicecontroller/types.go
- internal/slicecontroller/engine.go
- internal/slicecontroller/controller_test.go
- internal/slicelayout/policy.go
- internal/slicelayout/policy_test.go
- internal/sliceprotocol/types.go
- internal/sliceprotocol/validate.go
- internal/sliceprotocol/canonical.go
- internal/sliceprotocol/protocol_test.go
- internal/sourceinventory/publisher.go
- internal/sourceinventory/sourceinventory_test.go
- modules/home-manager/terminal-redeemer.nix
- flake.nix
- docs/PROTOCOL.md
- docs/CONFIG.md
- docs/OPERATIONS.md
- docs/HOST_LEECH_READINESS.md
- docs/adr/0002-host-leech-terminal-slice-domain-and-lifecycle.md
- docs/adr/0003-terminal-slice-workspace-sharing-and-persistence.md
- docs/adr/0004-single-monitor-niri-spatial-mapping-policy.md
- docs/testing/host-leech-hermetic-matrix.md
