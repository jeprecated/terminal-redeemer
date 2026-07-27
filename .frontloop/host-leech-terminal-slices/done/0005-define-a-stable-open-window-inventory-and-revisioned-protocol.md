---
title: Define a stable open-window inventory and revisioned protocol
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-3
---

## Goal

Evolve source inventory into an additive stable protocol that reconciles eligible open Kitty/Zellij windows across polls, reconnects, and Niri epochs without conflating Niri windows, Kitty processes, and Zellij sessions.

## Acceptance Criteria

- Depend on the passing Niri direct-IPC and live-only Zellij spike contracts; do not invent fallback semantics when either proof fails.
- Add an explicit schema version and compatibility policy while preserving current one-shot snapshot/list/open consumers.
- Build authoritative host inventory from Niri's local event-stream initial replay through `ConfigLoaded`, query Outputs separately, and validate all workspace/window/output references before publishing a complete snapshot.
- Give each eligible source window an opaque stable identity derived from `{source epoch, runtime Niri window ID}`; carry the raw runtime ID only as same-epoch evidence.
- Define source epoch as a persisted random UUID rotated when boot identity or private Niri socket filesystem identity changes, without serializing socket paths or values.
- Bind each source identity one-to-one to exactly one verified active Zellij session; distinguish active, dead/resurrectable, missing, ambiguous-prefix, and duplicate-session outcomes.
- Carry verified session identity plus workspace name, runtime workspace evidence, output logical geometry/scale, exact `(column,tile)` observation, tiled/floating state, and logical size metadata required by MVP spatial policy.
- Persist a monotonic revision that advances only when a complete semantic inventory changes; update `observed_at` on every complete poll even when revision is unchanged.
- Mark incomplete Niri/output/Zellij observations as degraded with machine-readable reasons, retain the last authoritative revision, and forbid degraded observations from driving destructive disappearance.
- Provide deterministic ordering, freshness based on source revision plus leech receive time, source-epoch replacement, full-resync, stale/out-of-order rejection, and duplicate revision idempotence.
- Reject or explicitly report one-window/one-session invariant conflicts and hostile/malformed metadata.
- Remain scoped to eligible open host Kitty/Zellij windows rather than switching primary discovery to headless session inventory.
- Test legacy payloads, version negotiation, replay, epoch rotation, runtime-ID reuse, complete/degraded transitions, duplicates, hostile metadata, and source clock skew.

## Design Decisions

- The host may use Niri's event stream internally; the leech-facing protocol remains revisioned full snapshots with bounded polling for MVP.
- Source identity is an opaque epoch-namespaced digest, not a raw Niri ID, title, order, CWD, or timestamp.
- Window and Zellij session identities are distinct fields with a one-to-one MVP policy.
- Degraded snapshots are informative only and cannot authorize closes.
- Event streaming from host to leech is deferred.

## Implementation Notes

Depends on the domain ADR, workspace-sharing semantics, and both executable spike tasks. Work in a direct Niri socket adapter, mirror/slice schema code, crash-safe epoch/revision state, fixtures, CLI JSON contracts, and public protocol docs.

## Outcome

Define a stable open-window inventory and revisioned protocol was implemented and validated within the recorded acceptance boundaries.
