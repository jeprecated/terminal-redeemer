# ADR 0005: Add global slice selection and live management

- **Status:** Accepted for v1.1 (disabled by default)
- **Date:** 2026-07-31
- **Decision owners:** Terminal Redeemer maintainers

## Context

The v1 controller safely projected every eligible source in selected named Niri workspaces and supported exact-source pickup plus session-keyed close/reopen. Its raw status JSON and exact-ID commands were sufficient control primitives, but they did not provide the intended Overton workflow: select every eligible Lattice terminal, subtract individual projections, and inspect or change the live slice from one active interface.

The inventory already includes every eligible source, including sources on unnamed host workspaces. The controller already owns selection, effects, and exact projection safety through its private serialized socket. The missing behavior therefore does not require another inventory, protocol, daemon, or authority.

## Decision

### 1. Additive desired-source policy

For an eligible source `s`, projection desire is:

```text
(all_eligible OR workspace_selected(s) OR picked_up(s)) AND NOT closed_by_user(s.session_id)
```

`all_eligible` is one durable global inclusion reason. It includes every current and future eligible source, including sources on unnamed host workspaces. It is additive: disabling it preserves selected workspaces and exact pickups and closes only projections with no remaining positive reason. Exact session-keyed close exclusions continue to override every positive reason and retain their existing lifetime.

Global enable/disable is idempotent and audited but deliberately not added to bounded undo history. The inverse operation is the other explicit toggle. This keeps prior schema-2 undo records compatible with the additive state extension. Workspace, pickup, close, and reopen undo behavior is unchanged.

`pickup-remove` removes only an exact pickup reason. `drop` remains an alias for session-keyed close; it is not redefined as pickup removal.

### 2. Unnamed sources

An eligible source on an unnamed host workspace may be desired by `all_eligible` or exact pickup and may receive an exact live-only attachment. An unnamed workspace has no cross-machine workspace identity, so the controller performs no spatial proposal for that source. This absence of placement is not a conflict or degraded result and must not create spatial state churn.

The live manager groups these sources under the synthetic display group `(unnamed)`. That label is presentation only and cannot collide with or become a persisted workspace key.

### 3. Routed launch remains workspace-selected

Global selection controls projection desire only. It does not cause every leech terminal launch to route to the host. `redeem slice launch` continues to route only when Leech mode is separately enabled and the exact current named workspace is explicitly selected. Mode-off and unselected launches retain their existing local behavior before any remote intent.

### 4. One controller-backed live manager

`redeem slice manage` is a Bubble Tea view and command client over the existing bounded owner-only controller socket. It displays discovery, desire, projection, close, connection, observation, and conflict facts as independent axes. It sends the same serialized control verbs used by the CLI and never reads or writes controller authority directly or executes effects.

Home Manager exports direct packaged Kitty/Redeem argv as `slice.manageCommand`. Consumers may bind that argv under a locally chosen Niri key. The module reserves and installs no management binding; the existing Mod+Return/Mod+W template remains unchanged.

### 5. Additive schema-2 compatibility

Inventory schema 1, RPC schema 1, and controller schema 2 remain unchanged. Controller state gains only optional `all_eligible`; false is omitted and old schema-2 state reads as false. A prior binary safely rejects authority containing active `all_eligible` as an unknown field rather than interpreting it incorrectly.

Safe downgrade order is therefore:

1. while the v1.1 controller is running, execute `redeem slice controller all-disable` and verify success;
2. stop and disable the controller service;
3. preserve the complete owner-only slice authority as required by the rollback procedure; and
4. deploy the prior package.

Because global toggles create no undo entries, disabling the field removes all v1.1-only persisted controller shape. Downgrading while it remains true fails closed with an unknown-field/invalid-state error; the configured restarting user service may repeat that failure until stopped. Operators must not delete or reinitialize authority to bypass it.

The in-place machine contract version advances from 1.0.0 to 1.1.0. Exact attachment, host ownership, bounded recovery, no-fallback launch behavior, disabled defaults, and the versioned artifact path remain unchanged.

## Consequences

### Positive

- One durable reason implements “all eligible terminals, minus explicit closes” for current and future sources.
- The TUI reuses the controller seam and cannot become a competing authority.
- Prior schema-2 state upgrades without reset, while active state fails closed under an unsafe downgrade.
- Named-workspace projection and routed-launch policy retain their existing meanings.

### Negative

- Unnamed sources attach without cross-machine spatial placement.
- A safe downgrade requires disabling global selection before replacing the package.
- The manager requires a terminal host; consumers must deliberately bind the exported direct argv.

## Non-goals

- Named reusable slices.
- Read-only/watch projection.
- Multi-monitor topology mapping.
- Slice clipboard synchronization.
- Leech-to-host spatial writeback.
- Automatic binding installation or machine activation.
- Changes to inventory/RPC schemas, exact attachment, or routed-launch ownership.
