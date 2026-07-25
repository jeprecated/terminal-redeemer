---
title: Define a stable open-window inventory and revisioned protocol
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-3
---

## Goal

Evolve source inventory into an additive stable protocol that reconciles open Kitty/Zellij windows across polls and reconnects without conflating Niri windows, Kitty processes, and Zellij sessions.

## Acceptance Criteria

- Add an explicit schema version and compatibility policy while preserving current one-shot snapshot consumers.
- Give each eligible open source window a stable identity namespaced by host/source epoch and distinct from Zellij session, runtime Niri ID, order, title, and CWD.
- Carry verified Zellij session and host Niri workspace/output/placement metadata required by the selected spatial policy.
- Provide monotonic revisioned full snapshots or equivalent events with deterministic order, freshness, source epoch, and resync semantics.
- Keep multiple windows for one session distinct and make duplicate payloads/revisions idempotent.
- Define machine-readable outcomes for disappearance, source epoch changes, stale snapshots, malformed entries, and metadata changes.
- Remain scoped to open eligible Kitty/Zellij windows rather than switching primary discovery to headless session inventory.
- Test legacy payloads, negotiation, revision replay, identity stability, epoch replacement, duplicates, and hostile metadata.

## Design Decisions

- Protocol evolution is additive and current snapshot/list/open consumers remain compatible.
- Window and Zellij session identities are distinct.
- Use revisioned full snapshots with bounded polling initially; event streaming is deferred.
- Runtime Niri IDs are source-epoch evidence, not durable cross-epoch identity.

## Implementation Notes

Depends on the domain ADR and share/slice decision. Work primarily in internal/mirror snapshot/protocol code, fixtures, CLI JSON contracts, and public docs. Specify identity and epoch behavior before implementation.
