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

## Outcome

Align controller state, inventory, and reconciliation with final v1 contracts was implemented and validated within the recorded acceptance boundaries.
