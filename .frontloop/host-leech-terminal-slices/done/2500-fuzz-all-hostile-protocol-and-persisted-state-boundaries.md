---
title: Fuzz all hostile protocol and persisted-state boundaries
priority: high
frontloop_approval_task: 851b810d76915c7af3c15fbbb6fd1a94e102cf7a03e434acca393808a841ad75-4
---

## Goal

Add native Go fuzz targets and seed corpora for externally supplied or crash-sensitive decoding, canonicalization, normalization, argv, and state boundaries.

## Acceptance Criteria

- Fuzz targets cover inventory envelopes, RPC/control requests and responses, controller state, token journals, canonical hashing, workspace normalization, process argv/environment metadata, and crash-truncated/duplicate-key JSON.
- Targets assert no panic, bounded input/output/state growth, deterministic canonicalization, valid UTF-8 handling, strict unknown/duplicate-key rejection, and no accepted unsafe path/session/argv identity.
- Rejected persisted input is never rewritten, re-enrolled, or converted into fresh authority.
- Seed corpora include current valid fixtures plus hostile Unicode, NUL/control data, size boundaries, revision extremes, path limits, duplicate keys, truncation at every byte class, and legacy payloads.
- A deterministic short fuzz smoke runs in Nix/CI; documented longer fuzz commands are available for pre-release testing.
- Any discovered defect receives a fixed regression seed and focused unit test.

## Design Decisions

- Use native Go fuzzing so failures are reproducible and corpora remain reviewable.
- Fuzzing must be hermetic and must not invoke live Niri, SSH, credentials, or user state.

## Implementation Notes

Keep fuzz corpora bounded and avoid repository-generated cache artifacts. Consider shared invariant helpers only when they do not couple fuzz oracles to implementation details.


## Completion Summary

- Added 17 hermetic native fuzz targets covering protocol/inventory envelopes, RPC/control, controller and journal persistence, canonicalization, normalization, argv/environment metadata, duplicate keys, truncation, Unicode, size, revision, path, and legacy boundaries.
- Added descriptor-validated cap+1 persisted reads and sparse oversized-file regressions so corrupt authority is rejected before unbounded allocation, never rewritten, and never silently re-enrolled.
- Bounded inventory encoding before caller I/O with zero-partial-output regression while preserving additive v1 input compatibility and the published normalization algorithm/digest.
- Made the required Nix layer execute generated mutations after complete seed baselines for every target; documented longer campaigns and retained no generated corpus artifacts.
- Fixed all independent review findings and passed focused/race fuzzing, all package/race/vet/module checks, required layer, hermetic matrix, and all offline flake checks; independent final review passed.

### Files Changed

- .gitignore
- docs/PROTOCOL.md
- docs/testing/host-leech-hermetic-matrix.md
- internal/safefile/read.go
- internal/slicecontroller/bounded_read_test.go
- internal/slicecontroller/control.go
- internal/slicecontroller/fuzz_test.go
- internal/slicecontroller/store.go
- internal/slicecontroller/types.go
- internal/sliceenv/fuzz_test.go
- internal/sliceenv/resolver.go
- internal/slicelaunch/bounded_read_test.go
- internal/slicelaunch/fuzz_test.go
- internal/slicelaunch/launch.go
- internal/sliceprotocol/codec.go
- internal/sliceprotocol/fuzz_test.go
- internal/sliceprotocol/protocol_test.go
- internal/sliceprotocol/workspace.go
- internal/slicerpc/bounded_read_test.go
- internal/slicerpc/fuzz_test.go
- internal/slicerpc/protocol.go
- internal/slicerpc/server.go
- internal/slicerpc/slicerpc_test.go
- internal/slicerpc/token_fuzz_test.go
- internal/slicerpc/token_store.go
- internal/slicetransport/client.go
- internal/slicetransport/client_test.go
- internal/slicetransport/fuzz_test.go
- internal/sourceinventory/bounded_read_test.go
- internal/sourceinventory/fuzz_test.go
- internal/sourceinventory/store.go
- internal/zellijlive/process_fuzz_test.go
- scripts/tests/host-leech-hermetic-matrix.sh
- scripts/tests/host-leech-layer-smoke.sh
