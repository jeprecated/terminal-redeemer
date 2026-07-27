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

## Outcome

Fuzz all hostile protocol and persisted-state boundaries was implemented and validated within the recorded acceptance boundaries.
