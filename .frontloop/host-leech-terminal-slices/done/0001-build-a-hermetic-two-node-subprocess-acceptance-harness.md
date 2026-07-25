---
title: Build a hermetic two-node subprocess acceptance harness
priority: critical
frontloop_approval_task: 851b810d76915c7af3c15fbbb6fd1a94e102cf7a03e434acca393808a841ad75-1
---

## Goal

Exercise the packaged host/leech workflow through real redeem subprocess, CLI, Unix-socket, persistence, and remote-command boundaries without credentials, live machines, or consumer activation.

## Acceptance Criteria

- The harness starts isolated simulated host and leech environments with separate config, state, runtime, cache, and process namespaces.
- A shell-inert fake SSH transport invokes the packaged host `redeem slice rpc` boundary locally and preserves real request/response framing, exit behavior, cancellation, and lost-response injection.
- Scripted Niri Unix-socket servers provide deterministic event replay, Outputs, actions, delayed windows, ID reuse, restart epochs, and unrelated sentinel-window evidence.
- The harness exercises controller enrolment/run/status, workspace selection, routed launch, host token journal, inventory publication, handoff, projection lifecycle, close/drop/reopen/undo, reconnect, disablement, and rollback using actual CLI/subprocess boundaries.
- Pinned real Zellij 0.43.1 is used for exact-session lifecycle where practical; Kitty/process behavior is represented by a controlled subprocess shim with exact argv/environment/process-tree evidence.
- Assertions prove exactly one host session/window intent, no automatic local fallback after routing, no host-session kill, no unrelated Niri mutation, and effect-free committed replay.
- The harness runs hermetically in the Nix sandbox with network, credentials, ambient graphical variables, and user state unavailable.

## Design Decisions

- Use real packaged redeem processes and filesystem/socket boundaries rather than another in-process interface test.
- Do not install keybindings, access SSH agents, activate Lattice/Overton, or touch mono-nix.
- Controlled shims must emulate process boundaries only; domain decisions remain exercised through production code.

## Implementation Notes

Add a dedicated tests/integration or internal test harness plus a Nix check. Preserve the existing unit/hermetic matrix as the faster layer.


## Completion Summary

- Added a Linux hermetic two-node acceptance harness using packaged redeem subprocesses, isolated host/leech roots, shell-inert fake SSH, scripted Niri Unix sockets, controlled Kitty/process trees, and pinned real Zellij 0.43.1.
- Proved packaged inventory/controller enrolment, singleton, selection, routed lost-success same-token replay, committed journal, automatic handoff/projection, exact PTY attachment, close/drop/reopen/undo/reconnect/close-focused, ID-reuse refusal, cancellation, disablement, rollback, epoch rotation, and sentinel/host-session safety.
- Proved host-location workspace/mode/width/height convergence, report-only order drift, exact-ID non-focus verify-after-write effects, one injected spatial failure plus bounded recovery, and zero host writeback.
- Fixed three contract-preserving defects exposed by subprocess testing: Linux process-tree traversal across non-leader threads, projection launch retirement honoring RetryWindow, and bounded exact Zellij catalog visibility after session creation.
- Hardened harness cleanup with pidfd-first owner/start identity, exact NUL-delimited environment matching, scoped signaling/wait/reap, leak assertions, and hostile PID/owner/zombie/embedded-marker regressions.
- Added a dedicated Nix sandbox check and testing traceability; repeated local, race, fresh and forced Nix lifecycle runs plus final independent review passed with no blockers.

### Files Changed

- internal/subprocessacceptance/harness_test.go
- internal/zellijlive/process.go
- internal/zellijlive/zellijlive_test.go
- internal/slicecontroller/engine.go
- internal/slicecontroller/controller_test.go
- internal/slicerpc/server.go
- internal/slicerpc/routed_launch_test.go
- flake.nix
- docs/testing/host-leech-hermetic-matrix.md
