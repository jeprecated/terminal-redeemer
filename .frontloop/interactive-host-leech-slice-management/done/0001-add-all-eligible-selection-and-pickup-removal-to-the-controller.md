---
title: Add all-eligible selection and pickup removal to the controller
priority: critical
frontloop_approval_task: 825d83f26e1a3b5c4d26b920878c36f468ed5ae1b24877774b5dc032ea3e978b-2
---

## Goal

Extend the existing desired-state model with one additive global reason and expose the already-implemented pickup removal path without changing host wire protocols or ownership effects.

## Acceptance Criteria

- State persists `all_eligible` with `omitempty`; old schema-2 authority loads as false, and false state remains readable by the prior release.
- `State.Wanted` implements the approved additive formula; `all-enable` and `all-disable` are idempotent serialized controller operations and `pickup-remove` calls existing `Engine.Pickup(source,false)`.
- Global toggles are audited but do not add a new undo kind, preventing downgrade-incompatible undo residue; existing workspace/pickup/close undo behavior remains unchanged.
- Enabling all projects each current and later eligible non-closed source exactly once, including unnamed sources; disabling it closes only projections lacking another workspace/pickup reason.
- Session-keyed close exclusions, reopen, source/epoch replacement, degraded observations, recovery budgets, successor gates, and launch handoffs retain their current semantics.
- All-eligible does not alter `slice launch` routing; only explicit selected named workspaces route Super+Enter remotely.
- Unnamed sources skip spatial-plan construction, retain no flapping spatial conflict, and cause no extra per-poll state commits.
- A timeout, mid-list launch failure, or restart during all-enable converges on later polls rather than leaving permanent launching records; this is tested at a realistic fanout size.
- Controller unit/model/fuzz/soak tests cover global selection, partial effects, restart, epoch changes, caps, and close/reopen composition.

## Design Decisions

- Reuse the existing reconciliation and effect execution paths.
- Keep `drop` as the documented alias for session-keyed close; add a distinct `pickup-remove` operation.
- Do not make all-enable/all-disable undoable; direct toggling is the inverse.
- Do not add inventory or RPC fields.

## Implementation Notes

Primary files: internal/slicecontroller/types.go, engine.go, control.go, store.go and tests; cmd/redeem/main.go and tests. Add the unnamed-workspace spatial guard in cmd/redeem/main.go. Preserve the unrelated active devenv migration diff.


## Completion Summary

- Added optional all-eligible controller authority and additive desired-source reconciliation.
- Exposed serialized all-enable/all-disable and pickup-remove operations while preserving drop-as-close and routed-launch selection.
- Recovered interrupted fanout launches and skipped unnamed-source spatial planning without churn.
- Added controller, CLI, model, fuzz, soak, compatibility, and fanout tests; full Go and focused race suites pass.
- Fresh review found no task-scoped blockers.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- internal/hostleechsoak/soak_test.go
- internal/slicecontroller/control.go
- internal/slicecontroller/controller_test.go
- internal/slicecontroller/engine.go
- internal/slicecontroller/fuzz_test.go
- internal/slicecontroller/model_test.go
- internal/slicecontroller/types.go
