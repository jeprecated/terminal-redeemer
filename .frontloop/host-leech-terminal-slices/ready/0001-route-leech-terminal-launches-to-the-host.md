---
title: Route leech terminal launches to the host
priority: critical
operator_follow_up: 2026-07-25
---

## Goal

Make Leech mode feel local by routing a terminal launch from the leech's current selected Niri workspace to the host, creating the authoritative host Kitty/Zellij session there, and projecting it back immediately with a visible local fallback.

## Acceptance Criteria

- Add an explicit Leech mode that can be enabled, disabled, and inspected without changing the existing local terminal launcher when disabled.
- Expose a packaged launch command suitable for a leech Niri Super+Enter binding; this repository provides the command/module contract but does not edit mono-nix bindings.
- Determine the leech's current static workspace name and route to the host only when that workspace is selected for projection.
- Create exactly one host Kitty window with exactly one new uniquely named Zellij session in the matching host workspace; never attach-or-create ambiguously or reuse an unrelated session.
- Return a stable source-window identity and reconcile the corresponding leech projection within a bounded time, without creating a second local window for the same source.
- Keep all process execution argv-based, validate workspace/session metadata, scrub nested-Zellij variables, and avoid shell/profile dependence or command injection.
- If host transport, workspace creation, Kitty launch, Zellij launch, or projection confirmation fails, report the failure and apply the configured local fallback policy without leaving an unowned duplicate host or leech window.
- Support an explicit no-fallback mode as well as the operator-approved local Kitty fallback.
- When the current workspace is not selected or Leech mode is off, launch the ordinary local Kitty behavior.
- Add hermetic tests for mode off, selected/unselected workspace, host success, timeout, partial host creation, projection delay, duplicate responses, fallback, no-fallback, and cancellation.
- Document that the host remains the execution/work owner and the leech window is only an interactive projection.

## Design Decisions

- Super+Enter is the intended consumer binding for the packaged launch command.
- In Leech mode, selected workspaces route new terminals to the host first.
- The host-created Kitty window and Zellij session are authoritative; the leech receives a projection.
- Ordinary local Kitty launch is the fallback and remains the behavior outside selected workspaces.
- One Kitty window maps to one Zellij session.
- This task does not edit mono-nix or activate either machine.

## Implementation Notes

Depends on workspace sharing semantics, the stable inventory protocol, single-monitor spatial mapping, transport hardening, and controller reconciliation. Define a transactional launch result so retries cannot create duplicate host sessions. Consumer keybinding and rollout remain a later mono-nix task after an immutable reviewed release.
