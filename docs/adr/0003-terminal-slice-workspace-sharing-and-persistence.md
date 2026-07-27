# ADR 0003: Define terminal-slice workspace sharing and persistence semantics

- **Status:** Accepted for v1 (disabled by default)
- **Date:** 2026-07-25
- **Decision owners:** Terminal Redeemer maintainers

## Context

[ADR 0002](0002-host-leech-terminal-slice-domain-and-lifecycle.md) defines the host-authoritative terminal-slice domain, exact live-only attachment, and this live-slice formula:

```text
(selected static workspace OR pickup inclusion) AND NOT close exclusion
```

At proposal time it intentionally left workspace normalization, per-source override lifetime, recovery across observations and source epochs, and controller persistence to later decisions. This ADR made those semantics deterministic before the revisioned protocol, controller, and routed-launch implementations selected fields and storage representations.

### Final v1 amendment

Manual close/drop authority is keyed by the exact verified Zellij SessionID, not the epoch-scoped source ID. It survives ordinary source replacement, headless windows, and source epochs. Explicit reopen or applicable undo clears it early; otherwise only consecutive accepted complete `live_session_ids` absence plus the committed grace deadline expires it automatically. Presence resets evidence, and degraded/duplicate/stale/conflicting/replayed observations never advance it. This final amendment supersedes older exact-source close and no-override-inheritance wording below while leaving exact-source pickups and bounded connection lineage unchanged. Controller state schema 2 fails closed on the old experimental representation; operators back up the entire controller authority directory and explicitly re-enrol rather than migrate in place.

The host-authoritative workspace reconciliation, persistence, controller, and routed launch described by this amended ADR remain disabled by default. Host spatial writeback is outside the controller model. The implementation preserves the executable constraints from the [live-only Zellij attachment spike](../spikes/0001-zellij-live-only-attachment.md) and the [Niri direct-IPC spike](../spikes/0002-niri-direct-ipc.md).

## Decision drivers

- Preserve host ownership of source windows, sessions, processes, and execution.
- Make every safe open host terminal discoverable without publication while keeping automatic projection understandable.
- Preserve manual local close across polling and restart without suppressing unrelated future windows.
- Distinguish incomplete observation, source lifecycle, local desire, attachment connection, and launch intent.
- Bound automatic recovery, including recovery that crosses a source-epoch boundary.
- Make local policy, ownership, retry, and routed-launch decisions crash-safe and inspectable.
- Leave wire representation, timing values, serialization, and command naming to their owning tasks.

## Decision

### 1. Namespace, inventory, and eligibility

Every controller record is qualified by an abstract host identity and abstract leech identity. The MVP configuration accepts exactly one host/leech pair, but stored selection, source, projection, recovery, audit, and routed-launch records still belong to that pair's namespace. Deployment nicknames and the legacy capture `host` label are not durable slice identities.

The host inventory exposes every eligible source in the configured host namespace. There is no host-side publish operation or per-window allowlist. Selection controls projection desire, not discoverability.

An eligible source is exactly one currently open host Kitty OS window bound one-to-one to exactly one verified active/live Zellij session. Source identity and verified session identity are distinct. Missing, dead or resurrectable sessions, duplicate session bindings, ambiguous matches, incompatible metadata, and any other failure of the one-window/one-live-session invariant are explicit, inspectable conflict or ineligible outcomes. They are never alternative attach targets. Exact attachment cannot create, resurrect, terminate, or prefix-match a session.

Source IDs are opaque and scoped to one source epoch. This ADR consumes them as exact values but does not define their digest, component encoding, or protocol representation. A source ID from one epoch is never equal to, and never has identity continuity with, a source ID from another epoch. Section 7 defines a narrow lineage operation that may transfer state to a successor; lineage is explicit rebinding, not source-ID equality.

### 2. Canonical static-workspace selection

Selection is by a canonical, case-normalized key derived from a static Niri workspace name. The selected set persists canonical keys, not runtime Niri workspace IDs, display spellings, or workspace positions. Unnamed workspaces are not selectable by this policy.

The original task split assigned the exact versioned normalization algorithm, accepted character repertoire, and encoding to task 0005; the implemented v1 protocol now defines them. The following ADR semantic rules apply regardless of that representation:

- names equivalent under the selected normalization version denote one key, so repeated additions are idempotent;
- removal by an equivalent spelling removes that one key;
- an empty or invalid normalized result is rejected rather than stored;
- display spelling may be shown but is not identity; and
- if one complete authoritative host observation contains multiple distinct static workspace names that normalize to the same key, that key and the sources in those workspaces are reported as a normalization conflict. The controller does not pick one spelling, workspace, or source arbitrarily and does not perform automatic projection from the conflicted key until a later complete observation resolves the collision.

A degraded observation cannot establish or clear a normalization collision because it cannot replace authoritative inventory.

### 3. Desired-source formula and selection operations

For an eligible source `s` in the latest accepted complete authoritative inventory:

```text
workspace_selected(s) := selected workspace keys contains s.workspace key
picked_up(s)           := an exact-source pickup inclusion applies to s
closed(s)              := a session-keyed close exclusion applies to s.session_id
wanted(s)              := (workspace_selected(s) OR picked_up(s)) AND NOT closed(s)
```

This is ADR 0002's live-slice formula without an additional implicit selection reason. The live slice is recomputed policy, not a named or separately owned collection. `wanted` expresses desire only; conflict, exact-binding verification, and the exhausted-successor gate in section 7 independently determine whether a wanted source is attachable. Those attachment/binding checks do not add to or rewrite the formula.

The operations have these deterministic meanings:

- **Add workspace:** atomically persist the canonical key first. Reconciliation then makes every currently eligible, non-conflicted matching source wanted unless its exact close exclusion applies. Every later eligible source in that workspace is evaluated by the same formula automatically.
- **Remove workspace:** atomically remove only that workspace reason. A locally owned projection is detached only if no exact pickup reason remains. Removal never changes host work, erases an independent pickup, or manufactures a close exclusion.
- **Pickup:** persist an inclusion for one exact eligible source. It may make a source outside selected workspaces wanted. It is not a session-name or workspace-wide inclusion.
- **Manual local close:** atomically persist the exact-session `closed_by_user` record before detaching or closing the positively owned leech projection. The source remains discoverable, and its workspace and pickup reasons remain intact. An ordinary presence poll or any rejected/degraded observation cannot clear the exclusion or reopen the projection; only explicit reopen/undo clears it early, while bounded confirmed complete session absence plus committed grace expires it automatically.
- **Reopen:** atomically clear the applicable close exclusion. Reopen is not persisted as a positive inclusion and does not reset connection recovery. A projection is wanted afterward only if the workspace or pickup reason still exists. An applicable undo has the same local inverse effect.
- **Undo:** reverse the latest still-eligible local selection action within retained undo history and atomically commit the inverse plus its audit event. It never reverses host lifecycle or a remote creation effect. If no retained action remains applicable, it has no target and must report that result rather than guessing.

A manual close during attachment recovery stops attachment attempts and detaches the local projection, but it does not reset an already-running source-recovery deadline. This permits the close exclusion to participate in the narrowly bounded lineage rule in section 7. Reopen during stable `disconnected` clears only the exclusion; explicit reconnect is still required to restart exhausted connection recovery.

### 4. Independent state axes

The controller persists and reports orthogonal facts rather than one combined state enum:

1. **Host source lifecycle and binding:** eligible, conflict/ineligible, temporarily absent under confirmation, confirmed closed, or replaced, together with the exact source/session binding evidence appropriate to the current epoch.
2. **Observation quality:** complete authoritative inventory or degraded/incomplete observation.
3. **Leech desire:** selected-workspace reason, exact pickup inclusion, and exact `closed_by_user` exclusion.
4. **Attachment connection and binding:** `connected`, bounded `reconnecting`, or stable `disconnected` for a wanted projection, plus any durable successor gate that withholds attachment without changing desire.
5. **Routed-launch intent:** including the separate `launch_pending` outcome for an uncertain host creation response.

A recovery record or successor gate coordinates bounded work across these axes but does not collapse them. In particular, `closed_by_user` is not a connection state, source-gone is not transport loss, degraded is not absence, a successor gate is not a close exclusion, and `launch_pending` is not a generic connection status.

A persisted observation that a projection was connected is not proof that it remains connected after controller restart. Live connection is re-observed. By contrast, a stable `disconnected` result and the unspent bounds of an active recovery episode are durable policy state.

### 5. Monotonic authoritative-snapshot acceptance

Before a complete snapshot can change current authority, the controller serially compares it with a durably retained accepted source epoch and revision for the configured host/leech namespace. The implemented v1 protocol owns the wire fields and representation; this ADR requires only these abstract ordering semantics:

- the first valid complete snapshot establishes the accepted epoch and revision;
- in the accepted epoch, a lower revision is stale and rejected before reconciliation;
- a semantically identical duplicate of the accepted revision is idempotent and causes no semantic state transition. A same-revision payload that differs from the accepted semantic inventory is a conflict and is not accepted;
- a higher complete revision in the accepted epoch may atomically replace the accepted inventory and revision;
- a valid replacement epoch is accepted once as a serialized epoch transition. That transition durably retires the prior epoch before new-epoch reconciliation, and the controller never rolls back to it; and
- every later snapshot from a retired epoch is rejected as replay, regardless of its revision. A revision from one epoch is never numerically compared with a revision from another.

A stale, duplicate, conflicting, or retired-epoch snapshot cannot begin, increment, reset, or otherwise advance source-absence evidence. In particular, duplicate receipt does not count as another complete absence and does not move or refresh any time boundary. A separately committed finite absence deadline may elapse under the downstream controller policy, but duplicate traffic is not evidence for that transition. Only an accepted complete inventory can establish new source lifecycle facts or a new epoch.

A degraded/incomplete observation is reported outside this acceptance sequence and leaves the durable accepted epoch, revision, and inventory unchanged.

### 6. Bounded recovery and restart

Unexpected transport or attachment loss for a wanted source enters a finite recovery episode and changes the connection axis to `reconnecting`. The episode records a durable generation and absolute exhaustion boundary sufficient to preserve the original budget across process failure. While running, the implementation may also use monotonic time for scheduling, but restart computes only the remaining portion of the already-committed bound. Restart, a repeated poll, a degraded response, or a failed attempt never grants a fresh budget.

Successful exact verified attachment within the active episode changes the connection axis to `connected`. Exhaustion changes it to stable `disconnected`, stops all automatic attachment attempts, retains the binding, exact verified session evidence, and operator-relevant outcome in current state, and establishes the durable successor gate defined in section 7 for that evidence. Explicit reconnect may start a new bounded episode only when the applicable source or uniquely provable successor is eligible and desired. Reconnect never clears `closed_by_user`, creates or resurrects a session, changes host ownership, or mints a replacement routed-launch token.

An accepted complete observation that temporarily omits a tracked source begins or advances bounded source-absence confirmation/recovery. Until absence is confirmed, the controller retains the last authoritative binding, policy, applicable overrides, ownership evidence, and local projection; one newly accepted missing snapshot is not by itself host close. If the attachment itself is lost, attachment recovery proceeds independently under its finite bound.

A degraded or incomplete observation is different: it retains the last complete authoritative inventory and cannot start or advance disappearance confirmation, establish host close, retire a source, or authorize projection closure. Transport degradation may explain attachment loss, but it does not turn that loss into source lifecycle evidence.

This ADR does not prescribe the exact retry count, duration, schedule, backoff, or source-close grace. The v1 implementation selects finite values and records their defaults in [the protocol](../PROTOCOL.md) and consumer contract.

### 7. Epoch replacement, explicit lineage, and successor gating

An accepted source-epoch replacement invalidates every old source ID. Old and new sources have no identity continuity, even when titles, PIDs, runtime window IDs, workspace names, or session names look similar. Those properties cannot transfer an override or binding.

There is one bounded lineage exception. During a recovery episode that was already active before the epoch change was accepted, the controller may atomically establish lineage from an old source to one successor only when all of the following hold:

- the recovery episode has not exhausted;
- old and candidate records are in the same configured host/leech namespace;
- the candidate is an eligible source in the replacement epoch;
- the candidate is bound to the same exact verified live Zellij session identity as the old source; and
- exactly one candidate satisfies all conditions.

The atomic transition retires the old active binding, binds the distinct successor source ID, records the lineage, and carries only applicable pickup, projection-ownership, and remaining recovery state. The independently session-keyed close continues to apply without being transferred between source IDs. The transition preserves the original recovery exhaustion boundary. It neither equates the two source IDs nor rewrites audit history.

Zero candidates means no rebind and recovery remains unresolved until another accepted authoritative observation or exhaustion. More than one candidate is an explicit conflict: no candidate is chosen and no attachment occurs. An epoch change cannot retroactively open a recovery episode merely to transfer old state.

#### Exhausted-successor gate

When recovery exhausts, the controller durably retains a successor gate on the attachment/binding axis. The gate is keyed by the old configured host/leech namespace and the old exact verified Zellij session evidence. A replacement-epoch candidate satisfies the old-successor predicate only when it is eligible in that namespace and has that same exact verified session evidence.

A matching candidate may remain `wanted` under the unchanged workspace/pickup/close formula, but it is not attachable and cannot be automatically adopted while the gate is unresolved. Explicit reconnect must re-observe that exactly one eligible candidate satisfies the predicate, atomically bind that distinct source ID, resolve the gate, and start a new bounded episode. Zero candidates leaves the gate unresolved; multiple candidates are conflict. A candidate with genuinely different verified session evidence does not satisfy the predicate and follows normal automatic projection in a selected workspace without inheriting old state.

The gate is current authority, not audit history. It survives controller restart and history pruning. It is retired only by explicit successful resolution, authoritative retirement of the old-source intent under the source-close rules, or an explicit operator action whose shape is defined downstream. It cannot expire merely because audit retention or wall-clock history limits are reached.

#### Replacement without an active or exhausted episode

If epoch replacement is accepted when an old source has neither an already-active recovery episode nor an exhausted-successor gate, the controller performs a no-continuity replacement transition. It atomically marks every old source ID replaced, stops old retries, and retires old bindings, pickup overrides, recovery facts, and ownership mappings from current matching. Independently session-keyed close records remain current while their exact sessions remain live. The controller then detaches only positively owned old projections and completes that cleanup before evaluating any new-epoch source, so an old and new projection cannot coexist for the same replacement transition.

No pickup, recovery, projection-ownership, or binding state is carried. Every new-epoch source is evaluated normally as a distinct new source identity, while an independently persisted exact-session close still suppresses a same-session successor until explicit reopen/undo or bounded confirmed absence expiry. A selected eligible source with a different session may project automatically, but only after old owned-projection cleanup. That evaluation is not source-ID continuity or rebinding.

A genuinely new session/source never satisfies an exhausted-successor gate. In a selected, non-conflicted workspace it follows the normal formula and is projected automatically without inheriting an old pickup inclusion, close exclusion, connection outcome, or ownership mapping. Thus an old manual close cannot suppress an unrelated later window, while bounded recovery can preserve intent across a proven compositor-epoch interruption.

### 8. Deterministic event semantics

| Event | Required result |
|---|---|
| Workspace is added | Commit the canonical selection, then request projections for all current and future eligible matching sources according to the exact formula and independent attachment/binding gates. |
| Workspace is removed | Remove only that reason; detach only positively owned projections with no remaining pickup reason; never affect host work. |
| Source moves between host workspaces | Keep its exact identity, pickup/close, binding, connection, and recovery facts. On the next accepted higher complete revision, recompute only its workspace reason. Moving in requests projection unless closed; moving out removes projection desire unless picked up. |
| Eligible source is picked up | Commit an inclusion for that exact source; no other source or later identity inherits it except through section 7 lineage. |
| Leech projection is manually closed | Commit `closed_by_user` before closing only the owned local projection; preserve inventory and underlying selection reasons; do not auto-reopen. |
| Source is reopened | Clear only the applicable close exclusion. Do not add pickup, resolve a successor gate, or reset an exhausted reconnect state. |
| Attachment transport is lost | Preserve desire and source lifecycle; enter or continue bounded `reconnecting`, then stable `disconnected` on exhaustion. |
| Observation is degraded/incomplete | Preserve the accepted epoch, revision, authoritative inventory, and projections. Do not advance source absence or perform destructive reconciliation. |
| Higher complete revision is accepted | Atomically advance the accepted revision and reconcile its inventory. It may establish new source facts or advance absence confirmation under finite controller policy. |
| Complete revision is duplicate, lower, conflicting, or from a retired epoch | Treat a semantically identical duplicate as an idempotent no-op; reject all other listed inputs. Never advance absence evidence, roll back an epoch, or perform destructive reconciliation. |
| Accepted complete inventory temporarily omits a source | Begin or advance bounded absence confirmation for the tracked source; preserve state and projection until confirmation. A same-epoch exact return in a later accepted revision resumes the binding idempotently. |
| Host source close is confirmed | Retire the source, its source-bound active overrides/binding, and any applicable successor gate from current matching; stop its retries and close only a positively owned leech projection. Record an inspectable tombstone subject to bounded history retention. Never terminate Zellij or other host work. |
| Epoch replacement with active unexhausted recovery | Accept the new epoch once and invalidate old IDs. Apply section 7 lineage only to one exact-session successor; zero candidates remain unresolved and multiple candidates are conflict. |
| Epoch replacement after recovery exhaustion | Preserve stable `disconnected` and the durable successor gate. A matching candidate may be wanted but is not attachable until explicit reconnect proves uniqueness and starts a new bounded episode. |
| Epoch replacement with neither active recovery nor successor gate | Atomically mark old IDs replaced, stop retries, and retire old bindings and state; detach only positively owned old projections before evaluating new sources. Carry no old state. Evaluate every new source normally as a distinct identity and never coexist with its old projection. |
| Controller restarts | Load one validated committed state, accepted epoch/revision and replay protection, original retry/absence bounds, successor gates, and stable disconnected outcomes; treat prior `connected` as needing observation, resume idempotently, and never duplicate a projection or launch. |
| Fresh namespace is initialized | Under serialized exclusive initialization, atomically create one validated empty generation only for a newly enrolled namespace proven never initialized. Do not reconcile or launch as part of initialization. |
| A later genuinely new window appears | Give it its own opaque identity. If eligible in a selected workspace and not matched by a successor gate, project it normally; never inherit old exact-source overrides. |
| A routed launch response is uncertain | Keep the same durable token and `launch_pending` intent; resolve or reconnect only that intent, never create a replacement or automatically fall back locally. |

Confirmed host close requires the implemented controller's finite complete-snapshot confirmation policy. Confirmation takes precedence over recovery: it retires source-bound current matching state rather than keeping a dead source reconnecting. Retired records may remain in bounded audit history but cannot match a future source.

### 9. Atomic current state

The controller has one logical current-state commit boundary per host/leech namespace. A transition publishes either the complete previous state or the complete next state, never a mix. The persisted current state includes, as applicable:

- namespace enrollment and initialization authority;
- the accepted source epoch, revision, authoritative inventory, and retired-epoch replay protection;
- the canonical selected workspace set;
- exact active pickup and close overrides;
- source/session bindings, explicit lineage, and projection ownership evidence;
- active recovery generations and exhaustion bounds;
- stable disconnected outcomes and unresolved successor gates needed to prevent automatic retry or adoption;
- routed-launch intents and tokens, including unresolved outcomes; and
- the current audit/undo boundary.

This is a semantic transaction requirement, not a choice of file format or serialization. The existing [`checkpoints.Store.Write`](../../internal/checkpoints/store.go) and [`storelock.Acquire`](../../internal/storelock/lock.go) exclusive-writer, durable atomic-replacement pattern is suitable implementation evidence, while the existing append-only [`events` store](../../internal/events/store.go) alone is not an atomic current-state authority for a multi-record controller transition.

State-changing operator requests are serialized. A current-state transition is made durable before reporting success or starting a related non-idempotent side effect. In particular, manual close persists its exclusion before local detach, and routed launch persists its token before any host creation request. Reconciliation after a crash repeats only idempotent effects for the committed intent.

An explicit serialized initializer may create a validated empty current generation only for a newly enrolled host/leech namespace with authoritative evidence that it has never been initialized or used. It uses the same exclusive-writer and atomic-commit boundary, and concurrent initializers cannot both succeed. Initialization itself performs no projection reconciliation or host creation. Ordinary startup never substitutes for this explicit initialization operation.

After initialization, startup accepts only a complete, validated current generation for the configured namespace. Missing state after initialization or use, or missing state for a namespace without valid fresh never-initialized enrollment evidence, is missing authority and fails safe exactly like corrupt, incompatible, or internally contradictory authority: it is surfaced for inspection and cannot be silently reinitialized, authorize destructive projection reconciliation, authorize a host creation, or replace a token. Repair, explicit decommission/re-enrollment, and migration behavior are owned by later implementation work.

### 10. Bounded audit and undo history

Current authority and history are distinct. The controller keeps a finite, inspectable audit/undo history under bounded retention limits chosen by implementation work. History identifies its host/leech namespace and records enough ordering and outcomes to explain initialization, snapshot acceptance/rejection, epoch replacement, selection changes, close/reopen/undo, recovery exhaustion, successor-gate creation/resolution, lineage/rebinding, source retirement, and routed-launch resolution.

Pruning history never mutates current authority. In particular, pruning must never remove or expire:

- accepted epoch/revision state or retired-epoch replay protection;
- an active pickup or close override;
- an active source binding or recovery episode;
- a stable `disconnected` outcome or unresolved successor gate that still gates automatic retry or adoption; or
- an unresolved routed-launch token or launch intent.

Such records remain in current state for as long as their semantics apply, regardless of audit limits. Once a source is authoritatively retired, its non-matching tombstone is history and may eventually age out; retirement cannot cause its override to match a later source. A resolved launch record may enter normal retention only after its durable token-to-result obligations are no longer unresolved. Exact limits, presentation, and storage layout remain implementation details outside this ADR and are bounded in v1.

### 11. Routed-token persistence

One routed-launch token denotes one host creation intent. It is namespaced to the configured host/leech pair and is durable before remote side effects. Lost responses, controller restart, attachment retries, and source polling reuse that token; they do not mint another token or trigger automatic local fallback.

A committed token-to-session/source result remains distinct from attachment connection state. `launch_pending` can coexist with stable `disconnected`. Explicit reconnect continues resolution or attachment for the same token. The implemented routed-launch boundary supplies the host transaction journal, token format, deterministic session-name algorithm, and protocol exchange; those details remain outside this ADR.

## Failure behavior

- Unsafe source/session bindings and workspace-normalization collisions are inspectable conflicts, never arbitrary choices.
- Exact live attachment failure cannot fall through to session creation, resurrection, prefix matching, or another candidate.
- Degraded inventory preserves authority and cannot close a projection.
- Lower revisions and retired-epoch replays are rejected; semantically identical duplicates are no-ops, and conflicting same-revision snapshots are conflict. None can advance absence confirmation or roll back an epoch.
- Complete temporary absence is bounded confirmation, not immediate host close.
- Ambiguous cross-epoch successor evidence is conflict. It does not transfer state.
- Retry or recovery exhaustion is durable `disconnected`; its successor gate prevents a matching wanted candidate from automatic attachment until explicit reconnect resolves it.
- Epoch replacement without active or exhausted recovery cleans up only positively owned old projections before distinct new sources are evaluated and carries no old state.
- Manual close remains discoverable and closed while its exact session is live. Explicit reopen/undo clears it early; otherwise only bounded consecutive accepted complete session absence plus committed grace expires it automatically.
- Missing authority after initialization/use is never silently treated as fresh state. Persistence validation failure prevents destructive reconciliation and non-idempotent launch effects.
- Leech close, controller exit, disconnect, source retirement, or history pruning never terminates host work.

## Consequences

### Positive

- Workspace policy automatically covers both current and later eligible sources without publication.
- Exact overrides survive ordinary polls, moves, restart, and narrowly proven bounded successor recovery.
- Opaque epoch-scoped identity remains strict while explicit lineage handles a recoverable compositor replacement.
- Monotonic accepted epoch/revision state prevents stale inventory from driving absence or epoch rollback.
- Stable disconnected, exhausted-successor, and unresolved launch outcomes survive crashes without infinite retry, automatic re-adoption, or duplicate creation.
- Current authority cannot disappear as a side effect of bounded audit retention.

### Negative

- Operators must explicitly reopen manually closed projections while their sessions remain live and explicitly reconnect exhausted ones; confirmed absent-session expiry is intentionally bounded rather than immediate.
- Normalization collisions and ambiguous successors remain visible conflicts until external facts change.
- An exhausted same-session successor remains wanted-but-not-attachable until explicit reconnect resolves its durable gate.
- Replacement outside active or exhausted recovery requires old owned-projection cleanup before a same-session new identity can project without inheritance.
- Crash-safe multi-concern current state is more demanding than an append-only activity log.

## Alternatives considered

### Named or reusable slices

Rejected for MVP. The live slice is the formula over selected workspaces and exact per-source overrides, not a published named object.

### Host publication or a per-window allowlist

Rejected. Every eligible open host Kitty/live-Zellij binding is discoverable; the leech chooses projection policy.

### Reopen immediately on the next poll

Rejected. Reopening a still-live session on the next poll would discard explicit local close intent. Reopen or applicable undo clears `closed_by_user` early; otherwise only bounded consecutive accepted complete session absence plus committed grace expires it automatically.

### Retry indefinitely

Rejected. Recovery has a finite bound and a stable operator-resumable `disconnected` outcome.

### Treat same session as source-ID equality across epochs

Rejected. Source IDs remain opaque and epoch-scoped. A unique exact-session successor may receive an explicit atomic lineage binding only within the bounded rules above.

### Let append-only history be controller authority

Rejected. Selection, override, recovery, projection ownership, and launch-token transitions require one logical atomic current-state commit. An event stream may supplement audit but cannot expose a partially committed current transition.

## Compatibility and sequencing

This accepted ADR narrows ADR 0002 without changing one-shot mirror or prior-boot resume behavior. It does not endorse the legacy read-only/watch surface; pinned Zellij supports only the proven exact interactive attachment for this domain.

The protocol representation, controller, and routed-launch implementation preserve the independent axes and rebinding constraints here. The controller and Leech mode remain disabled by default and require explicit operator enablement.

## Explicit deferrals and non-goals

This ADR does not define:

- protocol fields, schemas, source digests, source-epoch/revision wire representation, or initializer record encoding;
- the exact normalization encoding, algorithm, accepted character set, or version;
- retry durations, retry counts, backoff, polling intervals, or source-close grace values;
- storage file format, serialization, path layout, or migration encoding;
- CLI or control-socket command names;
- routed token format, session-name algorithm, or host transaction journal format;
- spatial mapping details, location writeback, or exact live order correction;
- named reusable slices, host publication, headless-session inventory, or arbitrary GUI projection;
- multi-host or multi-leech operation in MVP, despite mandatory namespacing;
- consumer configuration, mono-nix changes, service activation, or machine activation.

## Validation criteria

Acceptance review confirms that:

- every eligible open Kitty/exact-live-Zellij source is discoverable without publication;
- the exact ADR 0002 live-slice formula is preserved and reopen is not an inclusion;
- workspace normalization collisions are deterministic while encoding remains outside this ADR and is defined by the implemented protocol;
- source IDs remain opaque and epoch-scoped, and cross-epoch transfer is explicit lineage only;
- lineage requires an already-active unexhausted recovery episode, one exact verified session successor, and the configured namespace; ambiguity is conflict;
- accepted epoch/revision state is durable and monotonic: lower revisions and retired epochs are rejected, duplicates are idempotent without absence progress, and accepted epoch transitions never roll back;
- after exhaustion, a durable non-prunable exact-session successor gate leaves matching wanted candidates non-attachable until explicit reconnect uniquely resolves them, while genuinely different sessions project normally;
- epoch replacement without active or exhausted recovery retires old state, cleans up only positively owned old projections before evaluating distinct new sources, and carries no state or identity continuity;
- manual close, add/remove/move, degraded and complete absence, confirmed close, epoch replacement, restart, exhaustion, and later new windows have deterministic outcomes;
- source lifecycle, observation quality, desire, attachment/binding gates, connection, and launch intent remain independent;
- retry budget cannot reset on controller restart and infinite retry is impossible;
- a serialized initializer creates empty authority only for a newly enrolled, provably never-initialized namespace, while missing-after-use state fails safe;
- current state is one logical atomic commit and is independent of bounded audit retention;
- accepted snapshot ordering, active overrides, successor gates, stable disconnected outcomes, and unresolved routed tokens cannot be history-pruned;
- all records are host/leech namespaced while MVP admits only one configured pair; and
- no text makes protocol fields, source digest, timing values, serialization, or CLI names part of this ADR, and the host-authoritative controller/routed launch remain disabled by default.
