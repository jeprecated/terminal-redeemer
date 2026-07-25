# Host/leech slice readiness and consumer contract

## Technical contract and verification

The host/leech terminal-slice MVP is opt in and disabled by default. The
repository provides the implementation, hermetic evidence, module outputs, and
consumer integration template. Machine activation, Niri binding installation,
and physical two-machine validation are explicit operator actions.

The versioned technical artifacts are:

- [`consumer-contract.json`](../contracts/host-leech-slices/v1/consumer-contract.json),
  which records protocol, compatibility, runtime defaults, configuration,
  binding behavior, legacy compatibility, and watch/fallback guarantees;
- [`consumer-contract.schema.json`](../contracts/host-leech-slices/v1/consumer-contract.schema.json),
  which strictly validates those fields; and
- [`niri-bindings.kdl.in`](../contracts/host-leech-slices/v1/niri-bindings.kdl.in),
  which supplies the opt-in binding template.

Validate the schema, technical content, and package membership with:

```bash
python3 scripts/tests/host-leech-consumer-contract.py
nix build .#host-leech-consumer-contract
nix build .#checks.x86_64-linux.host-leech-consumer-contract -L
```

The package check compares the packaged contract, schema, and source template
byte-for-byte with the repository files, validates the contract against its
schema, and checks the generated store-path binding fragment. Before enabling
on machines, run the physical operator smoke described below.

## Terms and ownership

- **Host**: owns the authoritative Kitty window, Zellij session, processes,
  execution, files, and work. Host source existence is learned only from a
  complete revisioned inventory.
- **Leech**: owns controller desire and a positively identified local Kitty
  projection. Closing a projection never closes host Kitty, Zellij, or work.
- **Source**: one eligible open host Kitty window bound one-to-one to one exact,
  currently live Zellij session in one source epoch.
- **Selected workspace**: a static Niri workspace name included by controller
  policy. Current and future eligible sources in that workspace are desired.
- **Pickup**: an explicit exact-source inclusion outside workspace selection.
- **Close/drop**: a `closed_by_user` exclusion keyed by exact verified Zellij
  SessionID plus an owned local-window close. The source remains discoverable,
  and the drop survives source replacement, epochs, and headless windows while
  the session is live. Reopen/undo clears it early; otherwise confirmed complete
  session absence plus committed grace expires it automatically.
- **Reopen**: resolves a current source and clears that session exclusion. It does not manufacture a new host
  source or refresh an exhausted reconnect budget.
- **Undo**: reverses the latest still-applicable local controller action; it does
  not reverse host lifecycle.
- **Leech mode**: the separately persisted routed-launch choice. It defaults
  off and is inspected or changed with `redeem slice mode status|enable|disable`.
- **Projection**: a local interactive client of the exact host Zellij session;
  it is not a copy of host execution ownership.

The intended consumer keys are Super+Enter (Niri `Mod+Return`) for
`redeem slice launch` and Super+W (Niri `Mod+W`) for
`redeem slice close-focused`. **Super+W closes only the positively owned leech
projection. Host work remains alive and discoverable. While the exact session
remains live, the projection remains `closed_by_user` until explicit `redeem
slice controller reopen --source-id ...` or applicable undo; reconnect alone
does not reopen it. If the session is permanently absent, consecutive accepted
complete absence evidence plus the committed grace expires the drop
automatically.**

## Behavior and state

The live-slice formula is:

```text
(selected static workspace OR exact pickup) AND NOT closed_by_user
```

Complete host revisions add or update current eligible sources automatically.
Titles, CWDs, window order, and prefix session names are never identity or
ownership evidence. One persisted projection mapping, exact app ID, Niri PID,
configured Kitty executable, byte-for-byte argv, compositor epoch, and current
process evidence are required before a local close or mutation.

Controller connection states are observable and independent from host source
and leech desire:

| State | Meaning and transition boundary |
| --- | --- |
| `launching` | Exactly one local mapping was persisted before its Kitty side effect. |
| `connected` | Exact socket-isolated Zellij readiness and current local ownership were both proved. |
| `reconnecting` | A finite persisted episode is in progress with bounded exponential attempts. |
| `disconnected` | The original retry deadline/attempt budget is exhausted; no background retries continue. Only explicit reconnect starts another bounded episode. |
| `closed_by_user` | Local desire exclusion committed before close; host/source remains discoverable. Reopen/undo clears early; bounded confirmed session absence plus grace expires automatically. |
| source-gone grace | Only higher accepted complete revisions advance absence; configured grace or consecutive confirmation must complete before owned cleanup. |
| conflict/degraded | Evidence is incomplete, contradictory, stale, or unsafe; no destructive action is authorized. |

Distributed defaults are a 2-second poll interval, 5-second local-control bound,
30-second reconnect window, 5-second source-gone grace, and two consecutive
complete absences. Retry deadlines and attempts survive controller restart.
A prior `connected` record is downgraded to re-observation on startup. Successful
recovery inside the original window can uniquely re-adopt the projection or an
exact same-session successor. Zero/ambiguous successor evidence remains a
lineage conflict; exhausted matching successors are gated until explicit
reconnect. Degraded snapshots, transport loss, stale/duplicate/conflicting
revisions, and isolated process-query failures never advance disappearance or
close a window.

### Spatial authority

MVP supports exactly one active output on each machine. Static canonical
workspace name is cross-machine identity; raw Niri IDs are same-epoch mutation
targets only.

- `host_location` is the only supported v1 mode: host workspace, floating/tiled
  state, and proportional width/height continuously converge the owned leech
  projection. Leech divergence in those properties is reverted, never written
  back; order drift remains report-only.
- `leech_location` is dormant v1.1 specification only. Home Manager, config
  validation, controller state, production RPC, and effect execution expose no
  v1 host-writeback path.
- rollback to `host_location`: deterministic host-to-leech synchronization;
  failures remain visible and cannot affect Zellij ownership or unrelated
  windows.

Size uses logical-output percentages and remains approximate when working-area
exclusive zones, scale, or terminal cell grids differ. Initial `(column,tile)`
order is best effort; later drift is reported only. Column-order writeback is not
MVP.

### Residual coupling and race limits

These are accepted limitations, not permission to relax identity or fallback
rules:

- Concurrent host and leech Zellij clients share Zellij's minimum-client grid.
  A smaller client can constrain the shared grid and cause visible terminal
  reflow on the other client; v1 does not promise independent grids.
- Placement is approximate and proportional. Niri working areas, output scale,
  decorations, Kitty font/cell metrics, and the shared Zellij grid can prevent
  pixel-identical geometry even after every supported property converges.
- The contract is pinned-version coupled to Niri 25.11 and Zellij 0.43.1 on both
  machines. A version change requires a new compatibility review and smoke; an
  additive wire schema alone does not prove executable compatibility.
- SSH transport ambiguity means a timed-out or lost response can hide host work
  that was already created. Such an intent remains `pending` or becomes stable
  `disconnected`; it never authorizes automatic local fallback.
- Routed launch crosses Kitty start, PID-to-Niri-window correlation, helper
  readiness, inventory publication, and controller handoff boundaries. Delays,
  process-correlation races, PID reuse, or ambiguous candidates remain bounded
  pending/degraded/disconnected evidence and cannot authorize duplicate creation
  or adoption by app ID, title, order, or nearest window.

## Routed launch contract

With Leech mode off, or on a current static workspace that is not selected,
`redeem slice launch` invokes ordinary configured local Kitty before creating
any remote intent. Existing local-terminal behavior therefore remains unchanged
outside routing scope.

On a selected workspace with Leech mode on:

1. persist one random 256-bit token, deterministic path-bounded Zellij session,
   workspace, deadline, and pending controller handoff before SSH;
2. send one versioned host `launch` request;
3. host crash-safely journals and creates or replays exactly one isolated
   detached Zellij session and one Kitty window for that token;
4. correlate exact Kitty PID to Niri, ensure/move to the matching host workspace
   with `focus:false`, and freshly re-prove epoch, inventory, process, session,
   workspace, and source identity before commit;
5. hand the stable source identity directly to the controller and attach the
   local projection through the exact live-only wrapper.

After the first request, recovery uses only `token_replay` for the same
`{token,session,workspace}`. Lost/duplicate responses, delayed inventory,
cancellation, post-start errors, or uncertain transport remain `pending`; bounded
exhaustion becomes stable `disconnected`. Continue only with:

```bash
redeem slice launch --reconnect-token <original-64-hex-token>
```

Reconnect never mints another token or session. There is **no automatic local
fallback after remote intent**, because the host may already have created work.
Only a host-journal-proven non-creation is terminal `not_created`, and it still
requires explicit operator action rather than silently changing execution
ownership.

## Protocol, compatibility, and security

- Inventory and host RPC negotiate additive schema version 1; local controller
  state/control uses schema version 2 after the session-drop authority change.
  Unknown breaking fields/enums require a new version. Legacy
  unversioned `mirror snapshot/list/open` is not reinterpreted.
- Inventory consumes official direct Niri newline-delimited socket IPC through
  explicit successful `ConfigLoaded`, joins Outputs separately, and publishes
  only a complete validated topology. Every successfully completed authoritative
  poll advances the monotonic revision, even when its semantic hash is unchanged;
  degraded polls retain the last revision.
- A source epoch is random public identity tied privately to Linux boot plus
  Niri socket device/inode. Runtime ID reuse across epoch changes cannot preserve
  source identity. Degraded observations retain prior authority and cannot
  authorize disappearance.
- Pinned compatibility is Niri 25.11 and Zellij 0.43.1. Every Niri action is
  direct, exact-ID, non-focus where applicable, and verified after write.
- Exact attachment exposes only the verified live session socket through a
  same-filesystem hard link in marker-owned mode-0700 state, uses an empty cache,
  scrubs nested Zellij environment, and pins `on-force-close detach`. It never
  creates, resurrects, prefix-matches, or terminates a session.
- New slice execution is argv-based with packaged/store-path binaries and no
  `sh -c`, login shell, or profile. Graphical context is exactly
  `NIRI_SOCKET`, `WAYLAND_DISPLAY`, and `XDG_RUNTIME_DIR`; values never enter wire
  or state.
- SSH host keys, known-hosts, authentication, authorization, credentials,
  agents, and account policy are operator-owned. Terminal Redeemer weakens none
  of them. Operator-supplied SSH options remain trusted configuration.
- Slice clipboard is read-only `false` for first rollout. Legacy
  `mirror.clipboard.enabled` is independent.

## Consumer package, module, and binding contract

The reviewed consumer surface is:

| Surface | Contract |
| --- | --- |
| package | `packages.x86_64-linux.terminal-redeemer` / `apps.x86_64-linux.redeem` |
| machine-readable artifacts | `packages.x86_64-linux.host-leech-consumer-contract` |
| flake metadata | `lib.sliceConsumerContract` |
| modules | `homeManagerModules.terminal-redeemer`, `nixosModules.terminal-redeemer` |
| runtime schemas | inventory/RPC schema version 1; controller state/control schema version 2 |
| helper argv | read-only `slice.launchCommand`, `slice.closeFocusedCommand` |
| generated KDL | read-only `slice.niriIntegrationFragment` |

The generated fragment is available from module evaluation; the packaged
artifact contains `share/terminal-redeemer/host-leech-slices/v1/niri-bindings.kdl`.
Its source template is
[`niri-bindings.kdl.in`](../contracts/host-leech-slices/v1/niri-bindings.kdl.in):

```kdl
binds {
    Mod+Return { spawn "/nix/store/<terminal-redeemer>/bin/redeem" "slice" "launch"; }
    Mod+W { spawn "/nix/store/<terminal-redeemer>/bin/redeem" "slice" "close-focused"; }
}
```

This is an opt-in template, not a patch to an existing `binds` block. A consumer
must merge it without replacing unrelated bindings and only after reviewing the
package and configuration. The package path must come from the selected flake
output; do not copy the illustrative path.

Typed rollout settings are under `programs.terminal-redeemer.slice`: `sourceHost`,
packaged command overrides, trusted `transportOptions`, positive transport
bounds, optional private attachment roots, `leechMode.enable`, and controller
`enable`, namespace, and timing. Home Manager exposes no v1 location-authority
or host-write-authorization setting; rendered controller configuration is fixed
to host-location with host writeback disabled. Both Leech mode and controller
service default off; clipboard is fixed off. See
[CONFIG.md](CONFIG.md) for the exact YAML and module keys and
[PROTOCOL.md](PROTOCOL.md) for wire/state rules.

## Upgrade and explicit controller re-enrolment order

Do not reuse, delete, hand-edit, overlay, or generation-merge slice authority.
Its enrolled namespaces are separate from capture/history and legacy mirror
state. Backup copies are forensic evidence only: they are not ordinary rollback
restore points and never authorize replacement of newer or unresolved live
authority.

1. **Verify, do not activate.** Build the proposed source and contract package,
   validate the contract/schema and package-byte checks, run `nix flake check`,
   and inspect the consumer configuration diff. Use one package revision on host
   and leech.
2. **Quiesce and back up both authorities separately.** Keep Leech mode off and
   controller disabled. Stop an existing controller and prevent host RPC launch
   writers while copying. Preserve the complete host `stateDir/slice/`, complete
   leech `stateDir/slice/`, and each configured YAML into distinct owner-only,
   read-only forensic locations with their capture time/generation recorded.
   Never merge the host/leech copies, restore individual files, follow unexpected
   symlinks, or expose socket values, tokens, journals, or state in logs/reviews.
3. **Upgrade host first.** Deploy the package without activation, verify exact
   `redeem slice rpc` liveness/schema and pinned Niri/Zellij versions, then
   initialize source inventory/token authority only if never enrolled. Missing
   state after enrollment is an error, not permission to reinitialize.
4. **Upgrade leech and handle the controller schema explicitly.** Deploy the
   same contract. If old experimental controller authority exists, stop/disable
   the controller; copy the entire owner-only `stateDir/slice/controller/`
   directory to a separate owner-only forensic backup and verify it; then, only
   with explicit operator approval, rename/remove that controller directory and
   run `redeem slice controller init`. Never remove source inventory/token state,
   host Kitty/Zellij work, unrelated config, or legacy mirror state, and never
   overlay the backup into fresh authority. Inspect status, select disposable
   static workspaces, and run the controller in the foreground with mode off.
5. **Prove host-location projection.** Complete the hermetic gate and every
   applicable non-secret operator smoke below. V1 is fixed to host-location.
6. **Opt in controller service.** Enable only the reviewed Home Manager service,
   verify singleton/journal status, and keep routed launch mode off.
7. **Opt in routed launch.** Verify mode-off/unselected local behavior, then
   explicitly enable Leech mode. Add consumer Super+Enter only after response-loss
   and no-fallback smoke passes. Add Super+W only after ownership-safe close and
   explicit reopen smoke passes.

Controller state schema 2 deliberately rejects old schema-1 source-keyed drops
and experimental leech authority; there is no in-place translation. Inventory
wire schema 1 remains additive with required complete live-session evidence.
Upgrade never rewrites legacy mirror payloads, capture events, resume
checkpoints, source-inventory/token authority, or host Kitty/Zellij work.

## Downgrade and rollback

Rollback stops new control; it is not a cleanup command:

1. remove/disable the consumer Super+Enter and Super+W bindings;
2. `redeem slice mode disable` while the current binary is still present;
3. stop the controller service and set `slice.controller.enable = false` and
   `slice.leechMode.enable = false` in the consumer proposal;
4. preserve `stateDir/slice/`, especially host token journals, pending routed
   intents, controller exclusions/gates, and source inventory identity;
5. select the previous package and rebuild only after confirming it will
   ignore (not rewrite) additive slice state;
6. use explicit legacy interactive attach or the current binary's explicit
   reconnect/reopen before downgrade when access to known host work is needed.

Do **not** delete projections as rollback, clear token journals, rerun init over
used state, terminate host Kitty/Zellij sessions, or issue broad Niri closes.
Stopping/disabling the controller leaves host sessions, pending routed launches,
and unrelated local windows untouched. A still-open projection may be closed by
its normal window control, which detaches only; a `closed_by_user` projection is
recovered with explicit Terminal Redeemer reopen. A stable disconnected routed
intent is recovered with explicit same-token reconnect. Legacy
`redeem mirror open --mode attach` remains the proven independent manual access
path when its snapshot contains the live session.

If the older package cannot understand current slice state, leave the state
untouched and slice services/bindings off. **An older backup must never replace,
overlay, or be merged into any current namespace that has a newer generation or
unresolved authority.** Open older backups only from a separate offline forensic
path. Host work is authoritative and does not depend on leech state being
readable.

A same-generation forensic namespace replacement is exceptional recovery, not
rollout rollback. Before even considering it, quiesce every writer and first
preserve fresh, complete, separate copies of the current host and leech
namespaces. Version-matched read-only inspection must then prove all of the
following on both current copies and the proposed copy:

- no local routed intent is `pending` or `disconnected`;
- no host RPC token-journal record exists in any pending, starting, created,
  placed, committed, failed, or otherwise replay-relevant stage;
- no controller launch handoff, cleanup/successor gate, unresolved lineage, or
  reconnect/spatial recovery remains;
- no pickup, `closed_by_user`, workspace selection, spatial origin/baseline,
  source-gone evidence, or other current override would be removed or regressed;
- enrollment identity, epoch/revision/tombstone authority, generation, and every
  non-prunable record are exactly the same—not merely semantically similar; and
- no current file is missing, corrupt, unreadable, newer, or ambiguous.

There is no ordinary operator shortcut around this proof. If any item is
present, differs, cannot be decoded by the matching version, or is uncertain,
do not restore. Use non-destructive disablement, preserve both namespaces, and
recover work with explicit same-token reconnect/reopen or legacy exact attach.
This is the only safe rollback path for pending or unresolved work.

## Pre-smoke evidence layers

Repository evidence is layered; no aggregate coverage percentage or passing
unit suite substitutes for a stronger boundary:

- focused unit/integration tests prove local invariants;
- deterministic stateful model tests compare generated histories with an
  independent v1 reference model;
- native Go fuzz targets reject hostile wire and persisted state without panic,
  authority creation, or unbounded growth;
- packaged two-node subprocess and routed crash matrices cross actual
  process/socket/fsync/restart boundaries;
- flake-locked Niri 25.11 and Zellij 0.43.1 checks prove the pinned executable
  contracts;
- deterministic soak detects state/resource growth, retry-budget resets,
  duplicate effects, and temporary-resource leaks; and
- the physical credentialed two-machine smoke remains a separate mandatory
  activation gate.

Run the short secret-free soak with:

```bash
scripts/tests/host-leech-soak.sh --iterations 2000
```

For a longer validation run, retain the concise JSON counter/limit summary from:

```bash
scripts/tests/host-leech-soak.sh --iterations 10000 > host-leech-soak-summary.json
```

The summary contains no token, source/session identity, socket/path, environment,
credential, title, or private-state value. Its routed reconstruction counters
require one host session, host Kitty, placement, committed source, and local
projection launch across exactly two same-token transport attempts; pending
cleanups and every routed effect have explicit caps. Critical coverage is recorded in
[`host-leech-coverage-baseline.json`](testing/host-leech-coverage-baseline.json)
by risk family and named function. The versioned baseline is compared exactly;
any percentage or serialization drift fails until its per-risk change is
reviewed and a new baseline version is published. It remains one evidence layer,
not a global percentage gate.

`scripts/tests/host-leech-layer-smoke.sh --require` is the mandatory property and
fuzz smoke command. It discovers and runs every deterministic model test and
native fuzz target; removal of either evidence layer fails acceptance. Complete
the hermetic/Nix gate and physical smoke before machine enablement.

## Operator smoke matrix

Hermetic repository acceptance is mandatory first:

```bash
scripts/tests/host-leech-consumer-contract.py
scripts/tests/host-leech-layer-smoke.sh --require
scripts/tests/host-leech-hermetic-matrix.sh
scripts/tests/host-leech-soak.sh --iterations 2000
nix flake check
```

Automated checks use fixtures/private temporary state and do not connect to SSH,
inspect live sessions, read credentials, or activate machines. The following
checks are explicit, non-secret operator evidence on disposable named workspaces
with an unrelated sentinel window. Record pass/fail and sanitized IDs/timestamps,
not socket paths, environment values, argv dumps, tokens, credentials, titles,
or private state.

| Area | Required operator observation |
| --- | --- |
| Compatibility coupling | Both machines run the same package revision; liveness negotiates inventory/RPC schema 1 and controller schema 2; Niri 25.11 and Zellij 0.43.1 exact checks pass. Review and re-test any executable-version change before enablement. |
| Workspace creation | A missing static workspace names the exact trailing empty workspace, creates a replacement trailing empty workspace, and rejects exact/case-normalized duplicates. |
| Selection/new sources | Selecting a workspace projects its current and newly opened eligible host terminals exactly once; unselecting follows policy without touching unrelated windows. |
| Exact attach/concurrency and reflow | Host and leech clients interact concurrently with the same exact live session; resize the smaller client and record Zellij minimum-client-grid reflow without claiming independent grids; dead/cache-only and prefix names are rejected; force-close detaches and host work survives. |
| Routed launch | Mode off and unselected Super+Enter remain local; selected mode creates one token/session/host Kitty, places it without focus change, and projects that exact source. |
| Replay/transport ambiguity | Lose the first success response so host creation is ambiguous, delay inventory/projection readiness, replay the same token, and observe pending/disconnected inspectability with no second host session, Kitty, or local fallback. |
| Pickup/close/reopen/undo/drop expiry | Pickup includes one exact source; Super+W closes only its owned leech window; host remains discoverable; the session-keyed drop survives source and source-epoch replacement plus a live headless interval; reconnect does not reopen; duplicate/stale/degraded/conflicting revisions do not advance expiry; explicit reopen/undo clears early, while configured consecutive accepted higher complete absences plus committed grace expire the drop automatically. |
| Host-location/approximation | Host workspace/floating/size project proportionally and revert supported leech divergence; record approximation from working-area/scale/cell-grid differences; initial order is best effort and later drift is report-only. |
| V1 authority lockdown | Home Manager/config/state reject leech-location and write authorization; production spatial RPC and host-target effects are unavailable. |
| Equal/different outputs | Membership/state remain exact on one output each; percentage size is sensible and approximation reasons report dimension/scale/working-area/cell-grid differences. |
| Reconnect | Induce loss, recover within the original window, then separately exhaust to stable disconnected; observe no retries after exhaustion and successful explicit reconnect on the same source/token. |
| Source disappearance/epoch | Degraded polls close nothing; only complete confirmation/grace closes owned projection; restart/epoch replacement never reuses numeric ID or launches before old cleanup proof. |
| Revision/duplicate suppression | Confirm each successful complete poll advances revision even when semantics are unchanged; repeat/reorder revisions and responses and restart the controller; same-revision replay is idempotent, conflict is non-authoritative, and exactly one local mapping and one host transaction remain. |
| Process-correlation races | Delay Kitty PID visibility, helper readiness, source publication, and handoff; inject ambiguous/reused PID candidates and verify bounded pending/degraded/disconnected outcomes with no app-ID/title/order fallback or duplicate creation. |
| Ownership-safe close | Alter app/PID/executable/argv evidence and confirm close refuses; exact owned close emits only local `CloseWindow`; sentinel and host session remain untouched. |
| Rollback preservation | Remove bindings, disable mode/controller, and verify host sessions, pending routed-launch token/journal, retained host/leech state, open unrelated windows, and legacy one-shot attach remain available. |
| Validation isolation | Run repository validation with fixtures and private temporary state only; do not connect to live SSH endpoints, inspect credentials/agents/socket values/private live state, install bindings, or activate machines. |

Any failed row blocks consumer activation. Do not weaken identity, host-key,
complete-snapshot, or no-fallback rules to make a smoke pass.

## Legacy coexistence and retirement gate

Legacy one-shot `mirror snapshot/list/open/status/close` remains a separate,
proven compatibility path. Interactive attach still scrubs nested Zellij state
and closing its owned local window does not kill the remote session. Pinned
Zellij 0.43.1 has no watch command: watch returns a clear unsupported outcome and
is not a compatibility promise. Legacy clipboard behavior remains independent;
the slice rollout stays clipboard-off.

The slice controller is an opt-in replacement only after the full hermetic gate,
contract verification, and every applicable operator row passes for the selected
package revision. Do not retire legacy attach until routed response-loss replay,
manual close/reopen, concurrent-client host survival, controller restart,
reconnect exhaustion/recovery, rollback, and ownership-safe close have been
recorded. Treat retirement/removal as an explicit consumer change, never an
automatic consequence of installing the contract package.

## MVP limits

MVP includes one host/leech pair, one active output each, static workspace
selection, exact live projection, bounded controller/routed recovery,
workspace/floating/proportional size, and report-only order drift.

Future/stretch work is exact live column-order synchronization, multi-monitor or
docked topology mapping, slice clipboard synchronization, named slices, and
read-only/watch projection. Concurrent Zellij clients may have different
terminal sizes. Pinned watch is unsupported. None of those gaps permits a title,
prefix, resurrection, raw-ID, automatic fallback, or destructive ownership
shortcut.
