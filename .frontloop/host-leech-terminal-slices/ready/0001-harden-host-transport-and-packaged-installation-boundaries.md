---
title: Harden host transport and packaged installation boundaries
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-5
---

## Goal

Make host-leech execution predictable under packaged noninteractive operation without absorbing credential management or leaking graphical-session secrets into configuration or protocol data.

## Acceptance Criteria

- Use packaged/store-path executables for self, snapshot, Niri, Kitty, remote transport, and Zellij where modules can supply them, while retaining validated argv overrides.
- Use a documented clean noninteractive remote environment, scrub nested-Zellij variables, avoid login-profile dependence, and quote all metadata.
- Obtain source graphical-session/Niri context through a bounded mechanism that does not inspect profiles, logs, credentials, or private files and never serializes socket values.
- Keep host-key verification, authentication, authorization, and agents an explicit operator boundary with no weakened defaults.
- Add configurable positive timeouts, keepalive, cancellation, bounded retry/backoff, and actionable errors.
- Disable clipboard transfer by default for the new slice controller's first rollout while preserving legacy one-shot behavior.
- Add module evaluation checks without editing mono-nix or activating hosts.
- Test argv boundaries, hostile quoting, timeout/cancellation, clean environment, graphical-context failure, and operator-owned authentication settings.

## Design Decisions

- Terminal Redeemer does not manage credentials, host keys, or authorization.
- Packaged execution must not depend on interactive shell profiles.
- Clipboard is disabled for the first slice rollout.
- Consumer mono-nix edits and host activation are outside this epic.

## Implementation Notes

Depends on the domain ADR and stable protocol. Focus on remote runner/config/module boundaries and operations docs. Add safe slice-specific defaults without silently altering proven one-shot behavior.
