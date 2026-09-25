# ADR 0002: Keep remote mirror continuity manual and bounded

- Status: accepted

## Context

Persistent Zellij sessions survive a viewer disconnect, but reopening several projections should not require reconstructing them individually. A temporary workspace-following view is also useful while actively working across Lattice and Overton. Neither need a distributed controller, durable projection registry, or general GUI replay system.

## Decision

Terminal Redeemer provides four separate remote workflows:

1. `mirror open` is a manual project/session picker over a fresh source snapshot. It can show visible and headless live sessions and opens only the user's explicit selection.
2. `mirror save` replaces one host/profile pin from fresh source metadata and exact live local PID/SSH/Zellij evidence. `mirror apply` preflights active sessions and reopens the available missing projections attach-only in captured order. Pins contain typed placement metadata only, live outside `checkpoints/`, and are not retention history.
3. `mirror follow` is a foreground TUI. It freezes one source and destination workspace identity, periodically reacquires complete snapshots, and adds bounded batches of missing visible exact sessions. It moves each new projection once, never closes or continually reconciles it, and stops with the foreground process.
4. `mirror new` gives Overton sole creation authority for one generated persistent session. After creation it best-effort invokes a bounded source-local attach helper. Optional source placement is an explicit argument; source-view failure cannot roll back the session or Overton view.

Projection identity comes from a current Niri window PID, complete unambiguous descendant process evidence and an authenticated persistent helper reporting its immutable session incarnation. Legacy deterministic SSH/Zellij argv remains recognizable. Presence is independent of readiness: an offline helper prevents duplicate launches and remains saveable against a fresh matching source snapshot. Titles are presentation only. Mutation is limited to positively verified owned windows and exact Niri window IDs.

The persistent-reconnect amendment permits a per-window terminal supervisor, bounded local Unix IPC and an on-demand host recovery process. The supervisor owns physical input and disposable SSH PTYs; it never launches replacement windows or queues disconnected input. Clipboard completion is bound to the originating helper/attempt/input generation. Recovery shares host checks, backoff and two pending attachment slots, not selection or placement. Each attachment verifies boot/socket-birth incarnation and real Zellij client readiness. Only fresh complete authoritative absence ends a retained window; failed, stale or incomplete observations do not.

`mirror new` delegates one headless source creation, captures its socket identity and returns a verified receipt before opening an attach-only supervisor. An uncertain creation result is reported without replay or later name-only adoption. These runtime mechanisms add no durable desired-window registry or follow policy.

Rolling local capture/resume remains a fifth, independent workflow. Its boot checkpoints do not read or write mirror pins.

## Consequences

- There is no durable projection registry, event history, controller, follower service, installed recovery daemon, timer, Niri rule, or persisted follow selection. The on-demand runtime processes and private IPC above are deliberate exceptions to the original no-runtime/no-RPC decision.
- Save/apply can restore sessions, captured opening order, destination workspaces, floating/tiled state, and supported sizes, but not exact columns or stacks.
- Follow reopens a manually closed eligible projection on a later healthy poll. The follower does not close windows; a window's helper can exit on validated detach or authoritative original-session absence.
- Finite per-poll and lifetime attempt limits bound detached launch ambiguity. Disconnected or degraded observations retain existing windows and retry with capped backoff.
- Pins retain the exact configured destination token. Shared recovery additionally keys the full configured transport/helper identity; aliases are not guessed to be canonical host identities.
- Activation, zero-output behavior, connection-loss recovery, version skew, and physical dual-host placement remain user-owned smoke evidence.

## Rejected alternatives

- **Durable attachment registry:** duplicates live process authority and introduces stale adoption and pruning problems.
- **General controller or event stream:** unnecessary lifecycle machinery for manual apply and additions-only temporary following. Bounded process-local status/input and health/admission IPC is permitted only for retaining an existing window.
- **Policy-driven close or continuous placement reconciliation:** risks mutating user-arranged or unrelated windows. Authoritative termination of the exact incarnation attached by an existing helper is not a desired-window policy.
- **Always-on follow service or Niri rule:** turns an explicit temporary view into hidden persistent policy.
- **Exact column/stack replay:** unsupported by the bounded native Niri actions used here.
