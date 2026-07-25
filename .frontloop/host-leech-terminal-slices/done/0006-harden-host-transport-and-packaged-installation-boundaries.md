---
title: Harden host transport and packaged installation boundaries
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-5
---

## Goal

Make host-leech inventory, exact attachment, and mutation predictable under packaged noninteractive operation without absorbing credential management or leaking graphical-session secrets.

## Acceptance Criteria

- Depend on the passing Niri and Zellij spike contracts.
- Use packaged/store-path executables for self, Kitty, remote transport, and Zellij while retaining validated argv overrides; use the official direct Niri socket protocol with a pinned-version compatibility check for inventory and actions.
- Remove `sh -lc`, login-shell, and interactive-profile dependence from every slice path.
- Add a versioned JSON `redeem slice rpc` envelope with schema negotiation, typed outcomes, bounded request size, and verbs needed for snapshot, workspace ensure, idempotent launch, token query/replay, and liveness.
- Persist idempotency tokens crash-safely before side effects so replay returns the same host terminal/source identity rather than creating duplicates.
- Implement the proven exact live-only `redeem slice attach` wrapper: private mode-0700 same-filesystem socket tree, hard link for only the exact verified session, empty shim cache, nested-Zellij scrub, pinned `on_force_close=detach`, bounded path lengths, typed exits, and stale-directory GC.
- Obtain source graphical-session/Niri context through a bounded allowlist mechanism that does not inspect profiles, logs, credentials, or private files and never serializes socket values.
- Keep host-key verification, authentication, authorization, and agents an explicit operator boundary with no weakened defaults.
- Add configurable positive timeouts, keepalive, cancellation, bounded exponential retry/backoff, and actionable errors.
- Define `pending`/`disconnected` for ambiguous transport outcomes; never auto-launch a local fallback when the host may have created work.
- Disable clipboard transfer by default for the new controller while preserving the legacy one-shot clipboard setting independently.
- Add module evaluation and flake checks without editing mono-nix or activating hosts.
- Test hostile quoting/metadata, argv boundaries, protocol replay, timeout/cancellation, clean environment, graphical-context failure, exact attach exits, path limits, and operator-owned authentication settings.

## Design Decisions

- Terminal Redeemer does not manage credentials, host keys, or account authorization.
- Direct Niri IPC replaces shell-driven Niri commands in the slice path.
- Host RPC is additive and versioned; legacy one-shot commands remain separate.
- Uncertain routed launch never causes automatic local fallback.
- Clipboard is disabled for the first slice rollout.

## Implementation Notes

Depends on the domain ADR, stable protocol, and both executable spikes. Focus on direct socket/RPC runners, token state, exact attachment, config/module boundaries, and operations docs.


## Completion Summary

- Added strict schema-v1 host slice RPC with bounded negotiation, typed snapshot/workspace/liveness/idempotent-launch/token verbs, strict response correlation, and request-wide deadlines.
- Implemented crash-safe no-replace launch-token authority persisted before side effects, conservative pending ambiguity, direct packaged Kitty launch, and bounded direct OpenSSH transport without authentication or host-key weakening.
- Implemented exact interactive live-only Zellij attachment through marker-owned mode-0700 hard-link isolation, empty cache, nested-environment scrub, detach-on-force-close, typed exits, and ownership-proven stale GC.
- Added allowlisted graphical-context resolution, pinned direct Niri compatibility/actions, exact trailing-workspace ensure semantics, and symlink-safe private state boundaries.
- Added packaged executable injection, Home Manager evaluation, independent slice clipboard disablement, operations/config/protocol docs, and adversarial timeout/replay/path/auth/environment tests.
- Passed full uncached Go tests, full race suite, vet, full Nix flake checks, packaged smokes, and final independent review after resolving all correctness and security findings.

### Files Changed

- cmd/redeem/main.go
- cmd/redeem/main_test.go
- internal/config/config.go
- internal/config/slice_test.go
- internal/niriipc/client.go
- internal/niriipc/client_test.go
- internal/sliceattach/
- internal/sliceenv/
- internal/slicerpc/
- internal/slicetransport/
- modules/home-manager/terminal-redeemer.nix
- flake.nix
- docs/PROTOCOL.md
- docs/OPERATIONS.md
- docs/CONFIG.md
- README.md
