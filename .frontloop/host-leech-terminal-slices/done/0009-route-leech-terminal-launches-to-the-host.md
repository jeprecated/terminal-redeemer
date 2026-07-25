---
title: Route leech terminal launches to the host
priority: critical
operator_follow_up: 2026-07-25
---

## Goal

Make Leech mode feel local by making a selected-workspace terminal key create exactly one authoritative host Kitty/Zellij terminal and keep connecting the leech projection to that same idempotent launch.

## Acceptance Criteria

- Depend on workspace sharing, stable inventory, single-monitor spatial mapping, hardened versioned RPC, controller reconciliation, and both passing executable spikes.
- Add an explicit Leech mode that can be enabled, disabled, and inspected without changing the existing local terminal launcher when disabled.
- Expose a packaged launch command suitable for a leech Niri Super+Enter binding; provide the command/module contract without editing mono-nix bindings.
- Determine the leech's current static workspace name and route to the host only when that workspace is selected for projection.
- Before remote side effects, persist a unique idempotency token and deterministic collision-resistant Zellij session name within pinned socket-path limits.
- Make the host replay or create exactly one detached Zellij session and exactly one Kitty window for the token, correlate exact Kitty PID to Niri window, place it in the matching host workspace, and commit `{token, session, source identity}` crash-safely.
- Use pinned live-only attachment to connect the leech projection; never attach-or-create ambiguously, resurrect dead state, reuse an unrelated session, or prefix-match another name.
- Return the stable source identity when available and hand it directly to the controller so poll latency cannot create a second local projection.
- If the transport response is lost, replay/query the same token and keep trying to attach to the same session during the bounded reconnect window; never create a second host terminal.
- After retry exhaustion, persist and report `pending/disconnected`, stop retrying indefinitely, and let explicit reconnect continue the same token/session.
- Never automatically launch a local fallback when the host may have created work. Ordinary local Kitty remains the behavior only when Leech mode is off or the current workspace is not selected.
- If host absence and token non-creation are definitively proven, report failure and require an explicit user action rather than silently changing execution ownership.
- Keep all execution argv-based, validate workspace/session/token metadata, scrub nested-Zellij variables, and avoid shell/profile dependence or command injection.
- Add hermetic tests for mode off, selected/unselected workspace, first success, token replay, lost response after host creation, projection delay, disconnect exhaustion, explicit reconnect, duplicate responses, host absence, cancellation, and no automatic fallback.
- Document that the host remains execution/work owner and the leech window is only an interactive projection.

## Design Decisions

- Super+Enter is the intended consumer binding.
- In Leech mode, selected workspaces create host work first and then connect locally to that exact work.
- A token identifies one durable launch intent; reconnect never mints a replacement token.
- Uncertain host outcome becomes pending/disconnected, not local fallback.
- Local Kitty remains normal behavior outside Leech mode or outside selected workspaces.

## Implementation Notes

Define the host transaction journal and controller handoff before implementing effects. Consumer keybinding and rollout remain a later mono-nix task after an immutable reviewed release.


## Completion Summary

- Added disabled-by-default inspectable Leech mode and packaged `redeem slice launch` contract while preserving unchanged local Kitty behavior when mode is off or the current workspace is unselected.
- Implemented durable pre-remote launch intents with one random token, deterministic path-bounded session, serialized same-token replay, bounded reconnect/exhaustion, and no local fallback after intent creation.
- Extended host RPC with an enrolled crash-safe staged journal that creates or replays exactly one isolated Zellij session and Kitty window, correlates PID/Niri/workspace, and commits only after fresh epoch/fingerprint/inventory/process reproof.
- Implemented strict routed response/source tuple validation, runtime-window controller handoff, complete-authority matching, terminal non-creation resolution, and same-token explicit reconnect.
- Hardened missing-after-use host authority, fresh-session collision handling, effective socket path limits, transaction-identity gates before every resumed side effect, direct argv/clean environment, and exact live-only attachment boundaries.
- Passed full Go/race/vet/Nix/module/package checks and final independent correctness/adversarial reviews after resolving all identity, replay, journal, and crash-window findings.

### Files Changed

- internal/slicelaunch/
- internal/slicerpc/protocol.go
- internal/slicerpc/token_store.go
- internal/slicerpc/server.go
- internal/slicerpc/routed_launch_test.go
- internal/slicetransport/client.go
- internal/slicetransport/client_test.go
- internal/slicecontroller/types.go
- internal/slicecontroller/engine.go
- internal/slicecontroller/control.go
- internal/slicecontroller/controller_test.go
- internal/config/config.go
- internal/config/slice_test.go
- cmd/redeem/main.go
- cmd/redeem/main_test.go
- modules/home-manager/terminal-redeemer.nix
- flake.nix
- docs/PROTOCOL.md
- docs/OPERATIONS.md
- docs/CONFIG.md
- README.md
