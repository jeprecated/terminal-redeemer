---
title: Add bounded soak and publish the strengthened pre-smoke evidence
priority: high
frontloop_approval_task: 851b810d76915c7af3c15fbbb6fd1a94e102cf7a03e434acca393808a841ad75-5
---

## Goal

Run sustained virtual-time and subprocess churn to detect growth, leak, retry, and compaction failures, then update the acceptance index with the new pre-live confidence boundary.

## Acceptance Criteria

- A deterministic soak processes thousands of complete/degraded observations, session/source churn events, routed intents, drops, reconnects, and periodic restarts under bounded virtual or accelerated time.
- The soak asserts configured caps for controller/token state, tombstones, audit history, prepared namespaces, temporary caches, goroutines, file descriptors, and child processes.
- Retry deadlines and attempt budgets remain stable across restart; no duplicate host or projection effects accumulate.
- A short hermetic soak runs in the Nix matrix; a longer documented pre-release command produces a concise machine-readable summary without secrets.
- Coverage reporting is added for critical host-leech packages and records the baseline without using a superficial global percentage as the sole gate.
- The testing/readiness docs distinguish unit, property, fuzz, subprocess, real pinned-component, soak, and still-required physical two-machine evidence.
- All Go, race, vet, module, subprocess, crash, property, fuzz-smoke, soak, package/module, locked-spike, consumer-contract, and offline flake checks pass.
- No physical machine activation, credential inspection, keybinding installation, source-pin approval, or legacy retirement occurs.

## Design Decisions

- Soak tests must be deterministic and bounded enough for CI; longer runs remain explicit.
- Publication remains candidate_not_activated until a later physical smoke and separately approved immutable repository pin.

## Implementation Notes

Execute after the preceding test layers exist. Update docs/testing/host-leech-hermetic-matrix.md and HOST_LEECH_READINESS.md with exact commands and residual gaps.

## Outcome

Add bounded soak and publish the strengthened pre-smoke evidence was implemented and validated within the recorded acceptance boundaries.
