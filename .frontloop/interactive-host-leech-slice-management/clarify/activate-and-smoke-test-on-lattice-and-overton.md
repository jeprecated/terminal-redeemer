---
title: Activate and smoke-test on Lattice and Overton
priority: medium
frontloop_approval_task: 825d83f26e1a3b5c4d26b920878c36f468ed5ae1b24877774b5dc032ea3e978b-6
---

## Goal

After the immutable package and hermetic gates pass, deploy one reviewed revision and prove the complete operator workflow on disposable workspaces before enabling daily-use bindings.

## Acceptance Criteria

- One immutable package revision is deployed to Lattice and Overton with controller and Leech mode initially off and existing authority backed up per readiness guidance.
- Workspace mode and all mode project current and later eligible sources once, including the approved unnamed-source behavior, while a sentinel unrelated window remains untouched.
- The management binding opens the terminal-hosted live TUI; TUI and focused-close actions close/reopen only owned Overton projections and preserve Lattice Kitty/Zellij work.
- The smoke covers restart, degraded inventory, bounded reconnect exhaustion, explicit reconnect, response loss, rollback, binding removal, and absence of duplicate host/local work or automatic local fallback after remote intent.
- Only sanitized pass/fail and timestamps are recorded; no credentials, tokens, sockets, private argv, titles, environment values, or session contents are captured.

## Design Decisions

- Keep deployment opt-in and consumer-owned.
- Run physical smoke only after all preceding tasks and explicit approval.

## Implementation Notes

Existing work deliberately excluded mono-nix/consumer activation. This task requires the target consumer configuration location, machine access, and an approved maintenance window.

## Questions

### Q1: Which consumer repository/configuration and approved Lattice/Overton maintenance window should own the final activation and credentialed two-machine smoke?
