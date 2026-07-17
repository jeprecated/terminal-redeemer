---
title: Expose configurable startup resume and migration guidance
priority: medium
---

## Goal

Finish the operator contract: manual one-command restore by default, optional startup automation invoking the same path, diagnostics, and guidance for retiring duplicate host-local scripts.

## Acceptance Criteria

- `restore.onStartup` is typed in Home Manager/NixOS configuration and defaults to false.
- When enabled, the graphical-session service waits for Niri IPC and invokes the same `redeem resume` command rather than implementing separate logic.
- Startup execution is idempotent and does not conflict with the capture service.
- `redeem doctor` reports capture/resume prerequisites and actionable failures.
- README, CONFIG, and OPERATIONS document manual resume, dry run, startup automation, stale/empty candidates, failure statuses, retention implications, and rollback.
- Migration guidance explicitly requires disabling host-local startup restoration before enabling Terminal Redeemer startup resume.
- Module evaluation tests and CLI documentation tests pass.
- `go test ./...` and `nix flake check 'path:.'` pass.

## Design Decisions

- Manual `redeem resume` is the distributed default.
- Automation is configuration, not a second restore implementation.
- GUI applications remain opt-in and outside default resume scope.

## Implementation Notes

Primary areas: internal/config, internal/doctor, cmd/redeem, Home Manager/NixOS modules, README.md, docs/CONFIG.md, docs/OPERATIONS.md, and flake evaluation checks. Consumer-repository removal of legacy Niri scripts is a follow-up rollout action, not an in-repo code change. ADR: docs/adr/0001-resume-zellij-terminals-in-niri.md.


## Completion Summary

- Added typed `restore.onStartup` policy across Go, Home Manager, and NixOS configuration with a false distributed default.
- Added a bounded graphical-session Home Manager service that invokes the canonical idempotent `redeem resume` path and performs bounded Niri readiness waiting.
- Expanded `redeem doctor` with read-only boot, history integrity, Niri, Zellij, Kitty launcher, policy, and startup-service diagnostics.
- Aligned doctor event integrity with replay's malformed-trailing-record tolerance and moved default Niri queries to direct argv execution.
- Documented manual resume, dry-run, startup automation, statuses, retention, migration ordering, rollback, and host-local duplicate restoration removal.
- Added module evaluation and CLI/doctor/readiness tests, including disabled-service absence and enabled exact command/lifecycle assertions.
- Passed Opus ITERATE-to-ACCEPT judgment, parent `go test ./...` (235 tests), resume race tests (39 tests), and `nix flake check 'path:.'` (11 checks).

### Files Changed

- README.md
- cmd/redeem/main.go
- cmd/redeem/main_test.go
- docs/CONFIG.md
- docs/OPERATIONS.md
- flake.nix
- internal/config/config.go
- internal/config/config_test.go
- internal/doctor/checks.go
- internal/doctor/checks_test.go
- internal/niri/command_snapshotter.go
- internal/niri/command_snapshotter_test.go
- internal/resume/readiness.go
- internal/resume/readiness_test.go
- modules/home-manager/terminal-redeemer.nix
- modules/nixos/terminal-redeemer.nix
