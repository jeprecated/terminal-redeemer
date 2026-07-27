---
title: Revalidate routed launch and the adversarial matrix after controller correction
priority: critical
frontloop_approval_task: b8392b2aa50f804dc7f7fe8e05230b9ff1a8e118ae81ce3c82c52657d40afa61-3
---

## Goal

Audit the completed routed-launch and adversarial work against the corrected controller semantics, fixing only cross-layer regressions and missing evidence.

## Acceptance Criteria

- A routed launch creates exactly one host Zellij session/window per token, returns/adopts stable source identity, and places it through safe Niri IPC without duplicate creation.
- Ambiguous transport outcomes remain pending or disconnected, replay/adopt the same idempotency token, and never trigger automatic local fallback.
- Local fallback occurs only when slice mode is off or the workspace is definitively unselected before any host creation attempt.
- A fresh routed session cannot be suppressed by a drop override belonging to a different verified session.
- The hermetic matrix covers exact-session attach, prefix/dead-session rejection, session-keyed drop persistence across epochs, bounded confirmed-absence expiry, host-location reversion, leech-location config rejection, partial inventory, ownership adoption, reconnect exhaustion, token replay, crash boundaries, and host-session survival.
- Pinned-version spike scripts and all Go, race, vet, module, package, and flake checks pass without live-network, credential, or compositor dependence.

## Design Decisions

- Preserve existing routed-launch token-journal and pending-state architecture where it already satisfies the contract.
- Do not activate hosts, inspect credentials, or retire legacy one-shot behavior during validation.

## Implementation Notes

Audit internal/slicelaunch, internal/slicerpc, controller handoff, source inventory, and the completed 2500 tests. This is a corrective regression pass, not a redesign.

## Outcome

Revalidate routed launch and the adversarial matrix after controller correction was implemented and validated within the recorded acceptance boundaries.
