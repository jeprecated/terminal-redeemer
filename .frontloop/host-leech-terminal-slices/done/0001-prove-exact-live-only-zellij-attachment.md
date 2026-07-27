---
title: Prove exact live-only Zellij attachment
priority: critical
---

## Goal

Prove a packaged host-side attachment wrapper that can connect a leech projection to exactly one active pinned Zellij session without prefix matching, resurrection, or host-session termination when the projection closes.

## Acceptance Criteria

- Use the flake-locked Zellij 0.43.1 in an isolated executable test harness.
- Create a private mode-0700 ZELLIJ_SOCKET_DIR tree on the same filesystem as the real session socket and prove that a hard link exposes only the exact verified live session.
- Prove a symbolic link is unsuitable or otherwise ensure the wrapper uses a socket entry recognized by Zellij's file-type filtering.
- Use a dedicated empty shim XDG_CACHE_HOME and prove dead or missing sessions fail without resurrection.
- Prove a requested session cannot fall through to a unique-prefix sibling.
- Pin on-force-close to detach and prove closing the leech client never quits the host session, even with hostile user configuration.
- Scrub nested-Zellij variables, enforce pinned-version compatibility, validate session/path length bounds, and clean stale private attachment directories safely.
- Return exact argv, filesystem layout, exit-status semantics, and residual risks for implementation; block downstream attachment work if the proof fails.

## Design Decisions

- Live-only attachment is required for projections; attach-or-create and resurrection are forbidden.
- Use a hard link rather than a symlink unless the executable spike disproves the source-level finding.
- Closing a projection detaches only the leech client and never terminates host work.
- Concurrent host/leech clients and Zellij minimum-grid behavior are accepted.

## Implementation Notes

This is an implementation gate for transport, reconciliation, and routed launch. Relevant pinned source is zellij 0.43.1; source review found that live session discovery requires a directory entry whose file type is a socket, resurrection layouts come from XDG cache, and unique-prefix matching must be structurally excluded.

## Outcome

Prove exact live-only Zellij attachment was implemented and validated within the recorded acceptance boundaries.
