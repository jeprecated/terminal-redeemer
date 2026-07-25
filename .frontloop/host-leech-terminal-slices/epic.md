---
title: Project host terminal slices onto a leech workstation
status: active
---

## Goal

Let a weaker leech workstation opt into selected open Kitty/Zellij windows from an authoritative host, interact with those sessions, and project their Niri spatial organization without transferring session ownership or regressing the proven one-shot attach workflow.

## MVP boundaries

- One active monitor on the host and one on the leech.
- Static Niri workspace names select automatic projection; explicit pickup/close/reopen overrides operate per source window.
- Concurrent host/leech Zellij attachment and differing client sizes are accepted.
- MVP projects workspace, tiled/floating state, and proportional size. Exact live column-order synchronization is a stretch goal; MVP preserves initial order where practical and reports drift.
- Super+W closes only the leech projection. Host work stays alive and discoverable; reopening is explicit through Terminal Redeemer.
- Disconnect recovery is bounded. Successful recovery within the retry window may resume projections; exhaustion enters a stable disconnected state until explicit reconnect.
- Routed Super+Enter creates exactly one host terminal and keeps connecting to that same idempotent launch. An uncertain host outcome never triggers automatic local fallback.
- Clipboard synchronization, multi-monitor mapping, named reusable slices, arbitrary GUI projection, and screen/video streaming are outside MVP.

## Implementation gates and execution order

1. Prove exact live-only Zellij attachment.
2. Prove Niri direct-IPC inventory and safe mutations.
3. Codify the host-leech domain and lifecycle decisions using the spike evidence.
4. Define workspace sharing/persistence and the stable revisioned protocol.
5. Harden transport, packaging, and versioned host RPC.
6. Define the single-monitor spatial policy.
7. Implement ownership-safe controller reconciliation.
8. Implement idempotent routed host terminal launch.
9. Close the hermetic adversarial matrix.
10. Publish readiness, migration, rollback, and the immutable consumer contract.

Protocol, controller, or routed-launch implementation must not start until both executable spike tasks pass. Task dependencies are authoritative even though critical Frontloop filenames share a priority prefix.
