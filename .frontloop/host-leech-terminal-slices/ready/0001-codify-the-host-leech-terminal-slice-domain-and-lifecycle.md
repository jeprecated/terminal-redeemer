---
title: Codify the host-leech terminal-slice domain and lifecycle
priority: critical
frontloop_approval_task: f6b6bc66e0c143f308f29c30f292e245967ee8ee6200408eca153257505d50f5-1
---

## Goal

Add a domain ADR that gives host/workhorse, leech/workstation, share, slice, spatial projection, ownership, lifecycle, and interactive attachment one precise meaning before protocol or controller work begins.

## Acceptance Criteria

- Define the host as the authoritative machine where Niri, Kitty, Zellij, and agent sessions run, and the leech as the machine rendering and interacting with selected projections.
- Define all eligible open host Kitty windows backed one-to-one by Zellij sessions as discoverable resources without a host publish step.
- Define the live slice as selected static workspace names plus explicit per-window pickup/drop overrides; named slices are not required initially.
- Define Leech mode, host-location mode, leech-location mode, spatial projection, fidelity, degraded outcomes, ownership, pickup/drop/undo, and lifecycle without implying screen streaming.
- State that dropping, closing, disconnecting, or stopping a leech projection never kills or creates the host Zellij session.
- Record explicit non-goals: screen/video streaming, initial headless-session inventory, arbitrary GUI projection, first-rollout clipboard synchronization, mono-nix edits, and host activation.
- Keep current one-shot mirror behavior supported until replacement readiness is proven.

## Design Decisions

- Lattice is the host/workhorse and remains authoritative; Overton is the leech/workstation.
- The primary resource is an open host Kitty window with exactly one verified Zellij session, not every headless Zellij session.
- Static workspace names select automatic projection; individual terminals may be picked up or dropped as overrides.
- Leech mode may route new terminal creation to the host with a visible local fallback.
- Host-location and leech-location modes have distinct layout authority.
- This is native terminal projection through leech Niri/Kitty, not screen streaming.
- Leech lifecycle operations never own or terminate host work.
- The initial topology is one monitor on each machine.

## Implementation Notes

Depends on no other task. Produce a public ADR under docs/adr and minimal navigation updates only. Keep prior-boot resume and live host-leech projection as separate domains.
