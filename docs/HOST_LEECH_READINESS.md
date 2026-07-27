# Host/leech slice readiness and consumer contract

## Authoritative references

The host/leech terminal-slice MVP is opt in and disabled by default. Installing
the package does not activate the controller, routed launch mode, or Niri
bindings.

Use the versioned machine-readable artifacts in
[`contracts/host-leech-slices/v1`](../contracts/host-leech-slices/v1/) for the
consumer surface. The const-rich JSON Schema owns strict structure and exact
semantic values; runtime-coupled Go tests own protocol constants, pinned
versions, and defaults.

The detailed behavior is intentionally not repeated here:

- [ADR 0002](adr/0002-host-leech-terminal-slice-domain-and-lifecycle.md)
  defines domain ownership and lifecycle.
- [ADR 0003](adr/0003-terminal-slice-workspace-sharing-and-persistence.md)
  defines workspace selection and persistence.
- [ADR 0004](adr/0004-single-monitor-niri-spatial-mapping-policy.md)
  defines host-authoritative spatial behavior and the live Niri proof.
- [PROTOCOL.md](PROTOCOL.md) defines wire, identity, revision, attachment,
  recovery, and routed-launch rules.
- [CONFIG.md](CONFIG.md) lists exact YAML and module options and defaults.
- [OPERATIONS.md](OPERATIONS.md) owns generic deployment, service, security,
  state, and incident guidance.

The accepted v1 limitations remain one active output per machine, approximate
proportional placement, report-only live order drift, Niri 25.11, Zellij
0.43.1, shared minimum-client-grid reflow, and no automatic local fallback
after an ambiguous remote launch.

## Contract and repository verification

```bash
check-jsonschema \
  --schemafile contracts/host-leech-slices/v1/consumer-contract.schema.json \
  contracts/host-leech-slices/v1/consumer-contract.json
go test ./internal/consumercontract
nix build .#host-leech-consumer-contract
nix build .#checks.x86_64-linux.host-leech-consumer-contract -L
```

The flake check validates the JSON against the strict schema, runs compact
negative mutations across drop, exact command argv, authority, revision,
limitation, and no-fallback values, compares packaged source members
byte-for-byte, verifies the generated binding template, and checks the packaged
CLI surface. `internal/consumercontract` independently
compares contract defaults, protocol versions, normalization, and pinned
component versions with production Go constants.

The reviewed consumer outputs remain:

| Surface | Output |
| --- | --- |
| package/app | `packages.x86_64-linux.terminal-redeemer`, `apps.x86_64-linux.redeem` |
| contract artifacts | `packages.x86_64-linux.host-leech-consumer-contract` |
| flake metadata | `lib.sliceConsumerContract` |
| modules | `homeManagerModules.terminal-redeemer`, `nixosModules.terminal-redeemer` |
| helpers | read-only `slice.launchCommand`, `slice.closeFocusedCommand` |
| Niri fragment | read-only `slice.niriIntegrationFragment` |

The generated fragment is an opt-in template. Merge it without replacing
unrelated bindings. `Mod+Return` runs `redeem slice launch`; `Mod+W` runs
`redeem slice close-focused`, which may close only a positively owned leech
projection and never host work.

## Upgrade and explicit controller re-enrolment

Do not hand-edit, overlay, generation-merge, or casually restore slice
authority. Host and leech namespaces are separate from capture/history and
legacy mirror state. Backups are forensic evidence, not ordinary rollback
points.

1. **Verify without activating.** Build the proposed package and contract,
   validate the schema/package checks, run `nix flake check`, inspect the
   consumer configuration diff, and select one package revision for both
   machines.
2. **Quiesce and preserve both authorities separately.** Keep Leech mode off and
   the controller disabled. Stop existing controller and host RPC launch
   writers. Copy the complete host `stateDir/slice/`, complete leech
   `stateDir/slice/`, and each YAML file into separate owner-only, read-only
   forensic locations. Record capture time and generation without logging
   tokens, sockets, identities, argv, credentials, or private state.
3. **Upgrade host first.** Deploy without activation. Verify exact RPC liveness,
   schema negotiation, and pinned Niri/Zellij versions. Initialize source
   inventory/token authority only on a machine that has never been enrolled;
   missing state after enrolment is an error.
4. **Upgrade leech and handle controller schema explicitly.** If experimental
   controller authority exists, stop and disable the controller, preserve its
   entire directory, and only with explicit operator approval rename/remove
   that controller directory and run `redeem slice controller init`. Never
   remove host inventory/token state, Kitty/Zellij work, legacy mirror state, or
   unrelated configuration, and never overlay an old backup into new authority.
5. **Prove host-location projection.** Run all hermetic checks and applicable
   physical smoke rows below with mode off and disposable named workspaces.
6. **Enable the controller service.** Enable only the reviewed Home Manager
   service and verify singleton/journal status while routed launch remains off.
7. **Enable routed launch last.** First prove mode-off and unselected launches
   remain local. Then enable Leech mode. Install `Mod+Return` only after the
   response-loss/no-fallback smoke and `Mod+W` only after ownership-safe
   close/reopen smoke.

Controller schema 2 intentionally rejects old schema-1 source-keyed drops and
experimental leech authority; there is no in-place translation. Upgrade never
rewrites legacy mirror payloads, capture events, resume checkpoints, host
inventory/token authority, or host Kitty/Zellij work.

## Downgrade and rollback

Rollback stops new control; it is not a cleanup operation:

1. remove or disable consumer `Mod+Return` and `Mod+W` bindings;
2. run `redeem slice mode disable` while the current binary is available;
3. stop the controller and set both controller and Leech mode options false;
4. preserve all `stateDir/slice/` authority, especially token journals, routed
   intents, exclusions, cleanup/successor gates, and source identity;
5. select the previous package only after proving it ignores rather than
   rewrites additive slice state; and
6. use explicit same-token reconnect, reopen, or legacy exact attach before
   downgrade when known host work must remain accessible.

Do not delete projections as rollback, clear token journals, rerun init over
used state, terminate host Kitty/Zellij, issue broad Niri closes, or replace a
newer namespace with an older backup. Disabling services leaves host sessions,
pending launches, and unrelated windows untouched.

A same-generation forensic replacement is exceptional recovery. Quiesce every
writer, preserve fresh complete host and leech copies, and use a version-matched
read-only tool to prove exact enrollment identity, epoch/revision/tombstones,
generation, and every non-prunable record. It must also prove there is no
pending/disconnected intent, replay-relevant host journal, handoff, cleanup or
successor gate, unresolved lineage/recovery, pickup, drop, selection, or spatial
origin that would be removed. Any difference, corruption, missing file, newer
generation, or uncertainty blocks restore; keep services off and recover work
non-destructively instead.

## Automated pre-smoke gate

```bash
scripts/tests/host-leech-layer-smoke.sh --require
scripts/tests/host-leech-hermetic-matrix.sh
scripts/tests/host-leech-soak.sh --iterations 2000
nix flake check
```

The matrix uses fixtures, fakes, deterministic clocks, subprocess helpers, and
private temporary state. The soak keeps cap, effect-cardinality, retry-budget,
and resource-leak assertions in process; it emits ordinary Go test output, not
a separate status protocol. A longer pre-release run is:

```bash
scripts/tests/host-leech-soak.sh --iterations 10000
```

Optional coverage reporting uses native Go coverage and is not an exact
acceptance baseline:

```bash
go test -coverprofile=/tmp/host-leech.cover ./internal/...
go tool cover -func=/tmp/host-leech.cover
```

These evidence layers remain distinct: focused unit/integration tests,
deterministic controller model histories, native fuzz targets, packaged
subprocess/crash tests, pinned executable checks, bounded soak, and the mandatory
credentialed physical smoke. No aggregate percentage substitutes for a stronger
boundary.

## Physical operator smoke

Run on disposable named workspaces with an unrelated sentinel window. Record
pass/fail and sanitized timestamps, never credentials, tokens, socket paths,
private state, environment values, titles, or argv dumps.

| Area | Required observation |
| --- | --- |
| Compatibility | Both machines use one package revision; inventory/RPC schema 1, controller schema 2, Niri 25.11, and Zellij 0.43.1 pass. |
| Local boundary | Mode-off and unselected `Mod+Return` remain ordinary local launch. |
| Selection | Selected current and newly opened eligible sources project once; unselection touches no unrelated window. |
| Exact attach | Concurrent host/leech clients use the exact live session; dead, cache-only, and prefix names fail; detach and resize preserve host work while documenting shared-grid reflow. |
| Routed launch/replay | Selected mode creates one token/session/host Kitty and exact projection; a lost first response remains inspectable and same-token replay creates no duplicate or local fallback. |
| Close/drop/reopen | `Mod+W` closes only exact owned leech state; the session-keyed drop survives source/epoch replacement and live headless intervals; only explicit reopen/undo or confirmed complete absence plus grace clears it. |
| Spatial | Host workspace, floating/tiled mode, and proportional size converge on the leech; supported leech drift reverts, initial order is best effort, later order drift is report-only, and approximation is recorded. |
| Recovery | In-window recovery retains its original budget; exhaustion becomes stable disconnected; explicit reconnect uses the same source/token. |
| Disappearance/revision | Degraded, stale, duplicate, conflicting, and retired observations close nothing; accepted complete revisions alone advance absence; restart/epoch rotation never reuses raw identity. |
| Ownership/process races | PID/helper/inventory delay and ambiguous or reused candidates remain bounded and cannot authorize app-ID/title/order fallback, host mutation, duplicate creation, or unrelated close. |
| Rollback | Removing bindings and disabling mode/controller preserves sessions, journals, state, sentinel windows, and legacy exact attach. |
| Isolation | Repository validation does not connect to live SSH, inspect credentials/agents/private sessions, install bindings, or activate machines. |

Any failed row blocks consumer activation. Do not weaken host-key, complete
snapshot, exact identity, process ownership, or no-fallback rules to make a
smoke pass.

## Legacy coexistence and retirement

Legacy one-shot `mirror snapshot/list/open/status/close` remains independent.
Interactive attach still scrubs nested Zellij state, detaches without killing
remote work, and provides the manual recovery path. Pinned Zellij 0.43.1 has no
watch command; watch remains explicitly unsupported. Legacy clipboard behavior
is separate and slice clipboard remains off.

Do not retire legacy attach until routed response-loss replay, ownership-safe
close/reopen, concurrent-client host survival, controller restart, reconnect
exhaustion/recovery, rollback preservation, and the full physical smoke have
been recorded for the selected immutable package revision. Retirement is a
separate approved consumer change, never a side effect of package installation.
