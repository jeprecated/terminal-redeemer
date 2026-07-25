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



## Completion Summary

- Added deterministic 2,000–50,000 event soak coverage across observation churn, lifecycle/drop/reconnect/restart paths, real routed Router/Server/token/intent/controller reconstruction, exact attachment namespaces, helpers, and bounded resource/state caps.
- Proved same-token response-loss replay preserves absolute deadline and consumed attempts across reconstruction, yields exactly one host session/Kitty/placement/source/projection effect and two transport attempts, and cannot issue post-deadline calls or local fallback.
- Added strict secret-free soak summaries, explicit PendingCleanups/authority/resource/effect caps, documented 10,000-event evidence, and exact risk-family/named-function coverage baselines with stale-drift rejection.
- Integrated actual generated model/fuzz smoke, soak, coverage, pinned Niri/Zellij, packaged subprocess/crash, module, consumer-contract, and offline checks into the hermetic/Nix evidence layer while retaining candidate_not_activated.
- Passed independent final review after correcting stale coverage, routed retry/cardinality evidence, and absolute deadline enforcement; physical two-machine smoke and immutable pin approval remain separate gates.

### Files Changed

- internal/hostleechsoak/soak_test.go
- internal/slicelaunch/launch.go
- internal/slicelaunch/launch_test.go
- scripts/tests/host-leech-soak.sh
- scripts/tests/host-leech-coverage.py
- scripts/tests/host-leech-layer-smoke.sh
- scripts/tests/host-leech-hermetic-matrix.sh
- docs/testing/host-leech-coverage-baseline.json
- docs/testing/host-leech-hermetic-matrix.md
- docs/HOST_LEECH_READINESS.md
- flake.nix
