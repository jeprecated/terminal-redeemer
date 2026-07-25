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


## Completion Summary

- Aligned deterministic routed metadata/conflict responses across server, transport, and router as terminal no-fallback outcomes.
- Made host routed attachment exact-session-only with journaled marker/socket inode identity and a packaged host-attach helper that owns the private namespace until the pinned Zellij child exits.
- Preserved crash-safe same-token replay, no duplicate host session/Kitty/placement effects, and host-session survival across helper cleanup.
- Added production-path regressions for delayed child lookup after source-shaped proof, prefix/dead-session refusal, same-name replacement, crash replay, deterministic responses, and different-session drop isolation.
- Added strict hermetic Niri 25.11 contract mode plus failed-sentinel and exact-action adversaries while preserving the live nested-compositor operator gate.
- Updated the executable matrix to exact current tests and corrected stale completed-task claims.
- Full Go, race, vet, module verification, 16-package hermetic matrix, live/locked spikes, package/module builds, and offline flake checks passed; final citation resolver passed.

### Files Changed

- cmd/redeem/main.go
- internal/sliceattach/attach.go
- internal/sliceattach/attach_test.go
- internal/slicelaunch/launch.go
- internal/slicelaunch/launch_test.go
- internal/slicerpc/server.go
- internal/slicerpc/token_store.go
- internal/slicerpc/routed_launch_test.go
- internal/slicetransport/client.go
- internal/slicetransport/client_test.go
- internal/niri/ipc_spike_test.go
- internal/niri/testdata/ipc-actions.jsonl
- internal/niri/testdata/ipc-event-stream-failed-config.jsonl
- scripts/spikes/niri-direct-ipc.sh
- scripts/spikes/niri-direct-ipc-probe.py
- scripts/tests/host-leech-hermetic-matrix.sh
- docs/testing/host-leech-hermetic-matrix.md
- flake.nix
- .frontloop/host-leech-terminal-slices/done/2500-prove-host-leech-slices-with-hermetic-adversarial-tests.md
