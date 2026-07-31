---
title: Export keyboard-ready management integration and fix discoverability
priority: high
frontloop_approval_task: 825d83f26e1a3b5c4d26b920878c36f468ed5ae1b24877774b5dc032ea3e978b-4
---

## Goal

Make the existing focused close and new management TUI easy to bind from Niri using packaged direct argv, without silently activating either machine.

## Acceptance Criteria

- Home Manager exports read-only `slice.manageCommand` using packaged Kitty and Redeem argv to open `redeem slice manage`; no shell, `-c`, profile, or wrapper is introduced.
- Existing `slice.launchCommand` and `slice.closeFocusedCommand` remain compatible, and focused close keeps the same PID/app-ID/executable/full-argv/focus ownership reproof and safe fallback.
- The machine contract, strict schema, module evaluation, flake assertions, and consumer-contract tests agree on manage, all-selection, pickup-remove, and all read-only command exports.
- CLI help and README lead operators to continuous slice management, workspace/all selection, close/reopen, enablement, and the legacy mirror distinction.
- Documentation gives a consumer-owned Niri binding example for `manageCommand` but reserves no mandatory key and installs no binding automatically.
- Hermetic acceptance proves exact argv tokens/store paths; the live claim that a Niri binding opens the TUI is deferred to the approved two-machine smoke.

## Design Decisions

- Keep `close-focused` canonical; do not add an unshare alias.
- Reuse existing `slice.kittyCommand` and `slice.selfCommand`; add no launcher configuration.
- Do not alter the existing generated Mod+Return/Mod+W bindings or reserve a management key automatically.

## Implementation Notes

Primary files: modules/home-manager/terminal-redeemer.nix, flake.nix, contracts/host-leech-slices/v1, internal/consumercontract, cmd/redeem/main.go/tests, README/docs.


## Completion Summary

- Exported read-only shell-free Kitty/Redeem `slice.manageCommand` with exact generated config propagation.
- Preserved launch, close-focused, and existing generated Mod+Return/Mod+W integration semantics.
- Aligned strict contract/schema, module/flake assertions, packaged CLI checks, and consumer-contract tests for manage/all/pickup-remove surfaces.
- Improved CLI, README, config, operations, protocol, and readiness discoverability with a consumer-owned non-reserved binding example.
- Focused/full Go tests and Home Manager/NixOS/packaged contract checks pass; review found no blockers.

### Files Changed

- README.md
- cmd/redeem/main.go
- cmd/redeem/main_test.go
- contracts/host-leech-slices/v1/consumer-contract.json
- contracts/host-leech-slices/v1/consumer-contract.schema.json
- docs/CONFIG.md
- docs/HOST_LEECH_READINESS.md
- docs/OPERATIONS.md
- docs/PROTOCOL.md
- flake.nix
- internal/consumercontract/contract_test.go
- modules/home-manager/terminal-redeemer.nix
