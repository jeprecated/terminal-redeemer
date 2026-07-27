---
title: Prove every routed-launch crash boundary with process restart
priority: critical
frontloop_approval_task: 851b810d76915c7af3c15fbbb6fd1a94e102cf7a03e434acca393808a841ad75-2
---

## Goal

Systematically interrupt and restart the real subprocess harness at every durable routed-launch stage, proving idempotent recovery and host-work safety.

## Acceptance Criteria

- Crash injection covers pending, session_starting, session_created, socket_planned, kitty_prepared, kitty_starting, placed, proof_committed, and committed stages.
- Each case kills the responsible process after durable stage evidence, restarts from the same persisted state, and resolves through supported replay/reconnect behavior.
- Every case proves at most one token, Zellij session, Kitty/process side effect, Niri placement, source identity, controller handoff, and leech projection.
- No case switches to an ordinary same-name or prefix session, mints a replacement token, performs local fallback, deletes host work, or mutates the sentinel window.
- Definite pre-start failure, ambiguous post-start failure, response loss, cancellation, delayed child connect, delayed inventory, and crash during marker-checked cleanup have distinct assertions.
- Committed replay is effect-free; unresolved ambiguity remains inspectable and bounded rather than guessed away.
- The complete crash matrix is deterministic, time-bounded, and runs in the Nix sandbox.

## Design Decisions

- Journal stages are the exhaustive crash partition for v1 routed creation.
- A discovered implementation defect may be fixed only while preserving the published contract; product-semantic changes must return the task to clarification.

## Implementation Notes

Build on the subprocess harness rather than duplicating in-process routed tests. Provide per-stage failure diagnostics and retained temp-state opt-in for local debugging.

## Outcome

Prove every routed-launch crash boundary with process restart was implemented and validated within the recorded acceptance boundaries.
