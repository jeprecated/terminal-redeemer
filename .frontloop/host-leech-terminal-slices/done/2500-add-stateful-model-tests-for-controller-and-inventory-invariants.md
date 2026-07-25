---
title: Add stateful model tests for controller and inventory invariants
priority: high
frontloop_approval_task: 851b810d76915c7af3c15fbbb6fd1a94e102cf7a03e434acca393808a841ad75-3
---

## Goal

Generate long randomized but reproducible event/action sequences and compare production transitions with a small reference model of the published v1 safety invariants.

## Acceptance Criteria

- The model covers complete/degraded/stale/duplicate/conflicting/replayed observations, revision advancement, source epoch rotation, live/headless/absent sessions, workspace selection, pickup, session-keyed drop/reopen/undo, reconnect, controller restart, and source/process loss.
- Generated sequences assert degraded or rejected evidence never advances destructive absence, session drops affect only exact verified sessions, host-location converges supported properties, and order remains report-only.
- Recovery deadlines never reset accidentally; disconnected stability, successor/cleanup gates, and same-token routed handoffs remain monotonic.
- Every failure prints a deterministic seed and minimized or replayable operation sequence; a fixed corpus of adversarial seeds runs in ordinary CI.
- Tests cover bounded state/tombstone behavior and assert no duplicate owned projection or host-target effect becomes reachable.
- The property suite passes under the race detector and is included in the hermetic Nix matrix.

## Design Decisions

- Use a deliberately smaller independent reference model rather than reproducing controller implementation structure.
- Prefer deterministic generated tests in normal CI; expensive exploration can run through an explicit extended-test command.

## Implementation Notes

Use Go standard-library random generation/shrinking helpers or a small in-repo model; do not introduce a network service or uncontrolled dependency.


## Completion Summary

- Added deterministic stateful controller models with explicit lifecycle preconditions/outcomes, mandatory non-vacuity witnesses, generated replay values, deadline/restart/successor/cleanup/reconnect/process-loss/undo/handoff coverage, and fixed/custom/extended seed support.
- Replaced the inventory full-Source expectation with an independent window-to-exact-active-session relation and conflict-multiset oracle plus mandatory shuffle, unrelated-input, duplicate-binding, and evidence-removal metamorphisms.
- Fixed retry recovery so unsafe sources cannot launch while bounded episodes still expire to disconnected with exact successor gates; added conflicted and deselected expiration regressions.
- Passed independent review, repeated/custom/extended/race model runs, required model/fuzz discovery, full Go/race/vet/module checks, hermetic matrix, module evaluation, and all offline flake checks.

### Files Changed

- internal/slicecontroller/model_test.go
- internal/sourceinventory/model_test.go
- internal/slicecontroller/engine.go
- internal/slicecontroller/controller_test.go
- internal/sourceinventory/fuzz_test.go
