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

## Outcome

Build a hermetic two-node subprocess acceptance harness was implemented and validated within the recorded acceptance boundaries.
