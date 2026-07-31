---
title: Extend hermetic and packaged acceptance for interactive slices
priority: low
frontloop_approval_task: 825d83f26e1a3b5c4d26b920878c36f468ed5ae1b24877774b5dc032ea3e978b-5
---

## Goal

Prevent the new global selection and TUI surfaces from weakening the existing crash, ownership, transport, and no-duplicate guarantees before any machine activation.

## Acceptance Criteria

- Focused Go tests and race checks pass for controller, control socket, CLI, slicetui, consumer contract, and relevant process seams.
- State/control fuzzing and the deterministic model cover all toggles, pickup removal, close/reopen composition, response bounds, restart, epoch replacement, and hostile JSON.
- Soak and subprocess tests cover realistic all-mode fanout, partial effect timeout/failure convergence, controller restart, exact focused close safety, and no duplicate projection/host work.
- `scripts/tests/host-leech-layer-smoke.sh --require`, the hermetic matrix, the 2000-iteration soak, JSON-schema/package checks, and `nix flake check 'path:.'` pass.
- Test traceability docs map every new contract to named tests and preserve separation from live credentials, agents, Niri sessions, and consumer activation.
- No completion claim relies on a live TTY or two-machine environment; those checks remain in the rollout gate.

## Design Decisions

- Prefer unit/model tests for TUI state and subprocess tests only for real process/socket seams.
- Keep all automated validation hermetic and credential-free.

## Implementation Notes

Likely files: scripts/tests/host-leech-{layer-smoke,hermetic-matrix}.sh, internal/subprocessacceptance, internal/hostleechsoak, docs/testing/host-leech-hermetic-matrix.md, plus focused package tests.


## Completion Summary

- Added slice TUI to the mandatory hermetic package matrix and documented v1.1 traceability.
- Extended packaged two-node acceptance for all-eligible selection, additive fallback, restart, cardinality, and host-work preservation.
- Centralized failed launch-batch recovery so poll and control paths converge after partial fanout failures.
- Added serialized focus-required close rollback so failed fresh-focus/effect proof leaves no durable close, undo, or later unguarded mutation while generic close/drop remains unchanged.
- Ran focused/full/race/vet, all discovered fuzz targets, model, 2000-iteration soak, layer smoke, native/packaged hermetic matrices, strict contract checks, packaged crash/subprocess acceptance, and full `nix flake check`; final gates pass.
- Final adversarial correctness and simplicity reviews are clean after fixes.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- docs/OPERATIONS.md
- docs/testing/host-leech-hermetic-matrix.md
- internal/slicecontroller/control.go
- internal/slicecontroller/controller_test.go
- internal/slicecontroller/engine.go
- internal/slicecontroller/local.go
- internal/subprocessacceptance/harness_test.go
- scripts/tests/host-leech-hermetic-matrix.sh
