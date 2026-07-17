# ADR 0001: Resume Zellij terminals in their Niri workspaces

- **Status:** Proposed
- **Date:** 2026-07-17
- **Decision owners:** Terminal Redeemer maintainers

## Context

Zellij can serialize and resurrect terminal sessions after a reboot or crash, but it does not know which Niri workspace contained the Kitty OS window attached to a session. Kitty likewise does not own compositor placement. Niri exposes current window and workspace state over IPC, but its numeric window and workspace IDs are runtime-local.

A power-loss incident demonstrated the resulting gap: the Zellij sessions remained recoverable, while the durable association between each session and its compositor placement did not. A host-local Niri script separately saved application placement and Kitty working directories, but not Zellij session identity. On startup it therefore recreated Kitty windows in approximately the right layout while starting unrelated Zellij sessions.

Terminal Redeemer already has most of the required domain model and mechanisms:

- capture and historical replay of Niri windows and workspaces;
- terminal enrichment with CWD and a verified Zellij session tag;
- Zellij attach planning;
- Niri workspace movement after launch; and
- interactive and timestamp-based restore flows.

However, periodic Terminal Redeemer capture can be disabled by consumers, captured records do not identify their boot epoch, the current restore path can create a missing Zellij session, and new windows are correlated by application ID and launch order. Those properties are not sufficient for safe, automatic-capable crash recovery.

## Decision drivers

- Recover the exact captured Zellij sessions rather than merely opening replacement terminals.
- Restore the correct Niri workspace reliably after an unclean shutdown.
- Make repeated invocation safe and avoid duplicate Kitty windows.
- Provide a single manual command while allowing consumers to automate the same operation.
- Avoid creating a new Zellij session under a stale captured name.
- Remain useful when optional layout details or applications cannot be restored.
- Preserve historical restore and live-mirroring behavior.
- Make the last successful capture durable across sudden power loss.
- Consolidate ownership in Terminal Redeemer rather than maintaining host-specific session scripts.

## Decision

### 1. Terminal Redeemer owns the session-to-placement record

Terminal Redeemer will be the canonical owner of the association:

```text
Zellij session identity
  -> Kitty window
  -> Niri workspace reference
  -> optional Niri layout attributes
  -> terminal CWD
  -> capture timestamp and boot ID
```

The canonical terminal identity is the verified Zellij session name. Capture will continue to prefer process environment and arguments, with a verified title-derived value only as a fallback. A title alone is not a durable identity.

A captured workspace reference will contain, when available:

- workspace name;
- workspace index;
- output name; and
- the runtime workspace ID as historical evidence only.

Numeric Niri workspace IDs will never be used directly across boots. Restore resolves a target by workspace name first, then output plus index, then index. An unresolved target is reported as degraded and follows a configurable fallback policy rather than being silently treated as a successful placement.

The capture model may also retain column order, floating state, and tile dimensions. Correct workspace placement is required for the first implementation; finer layout restoration is best effort and must be reported separately.

### 2. Captures are boot-aware and power-loss durable

Every new event and snapshot will record the current Linux boot ID. The store will retain history across boots subject to the existing retention policy.

Capture will run a complete reconciliation on a configurable periodic timer. Each run queries Niri windows and workspaces and refreshes process-derived terminal metadata, so window moves, workspace changes, and Kitty-to-Zellij associations appear in the next checkpoint without a long-running event subscriber. Its single-writer exclusion must be crash-recoverable, using an advisory lock or boot-aware stale-owner detection rather than a persistent lock file that can permanently block capture after power loss. Concurrent writers fail visibly and do not corrupt the store.

A completed checkpoint must survive abrupt power loss:

- append-only events are flushed with `fsync` before being acknowledged;
- snapshots/checkpoints are written to a temporary file, flushed, atomically renamed, and followed by a directory `fsync`;
- replay may discard only a malformed trailing event while retaining earlier complete events; and
- a failed or partial current-boot capture cannot replace the previous boot's restore candidate.

The recovery point objective is one capture interval plus normal scheduling delay. The distributed default interval is 60 seconds. Operators may shorten or lengthen it, and `redeem capture once` remains available for an immediate checkpoint after a deliberate layout change.

Existing records without a boot ID remain available to explicit timestamp-based restore. They are not silently treated as an authoritative previous-boot candidate for automatic restoration.

### 3. Add an idempotent `redeem resume` command

The primary operator workflow will be:

```bash
redeem resume
```

`redeem resume` selects the latest valid checkpoint from a boot other than the current boot and restores terminals only by default. It reports the selected boot and capture time before applying. It does not silently skip over a newer prior boot merely because that boot contains no eligible terminals; in that case it reports an empty candidate and directs the operator to `redeem restore tui` or explicit `redeem restore apply --at ...` selection. Those existing commands remain the historical and forensic interfaces.

The command will:

1. wait for Niri IPC to become available;
2. select and validate the prior-boot checkpoint according to configured age policy;
3. inspect current Kitty windows and their Zellij session identities;
4. skip sessions already represented by a current window;
5. attempt only captured Zellij sessions that can be attached or resurrected;
6. launch one Kitty window for each remaining session;
7. correlate each launch to the exact Niri window;
8. move it to the resolved workspace;
9. apply optional layout attributes on a best-effort basis; and
10. emit a structured summary of restored, already-open, unavailable, degraded, and failed items.

Repeated execution against unchanged state must create no additional windows.

The resume path will use `zellij attach <session>` without `--create`. An unavailable or non-resurrectable session is skipped and reported. The existing attach-or-create behavior may remain available for explicit historical restore, but it is forbidden in crash-resume reconciliation.

`redeem resume --dry-run` will perform selection and reconciliation without launching or moving windows.

GUI application restoration is opt-in and outside the default `resume` scope.

### 4. Correlate launches by identity, not ordering

The current application-ID/creation-order heuristic is insufficient when multiple Kitty windows are launched or unrelated windows appear concurrently.

The resume implementation will launch Kitty through an argv-based launcher that retains the child PID, observe Niri until the window with the corresponding client PID appears, and then operate on that exact Niri window ID. Launches are reconciled individually with a bounded timeout. If a configured launcher cannot provide a reliable correlation identity, the item fails or degrades explicitly; it is not assigned another session's workspace by order.

This preserves Kitty's normal application ID and existing Niri window rules.

### 5. Manual and startup policies use the same command

Startup restoration is configurable and disabled by default:

```yaml
restore:
  onStartup: false
```

With `onStartup: true`, the Home Manager module installs a graphical-session user service that invokes `redeem resume` after Niri IPC becomes available. It does not implement a separate restore algorithm.

Additional policy remains configurable, including checkpoint age, unresolved-workspace behavior, capture intervals, and whether non-terminal applications are eligible. The safe distributed defaults are terminal-only, no session creation, and no startup restore.

### 6. Retire host-local duplicate ownership after migration

Consumer configurations may temporarily keep existing session scripts while adopting capture-only behavior, but only one component may perform startup restoration.

After Terminal Redeemer resume is deployed and verified, consumers will remove host-local Niri session save/restore scripts, timers, and startup hooks. Terminal Redeemer's capture and restore configuration becomes the sole owner of this workflow.

## Failure behavior

Resume is item-isolated: one unavailable session, failed launch, or failed workspace move does not prevent independent items from being attempted.

The command distinguishes at least:

- `restored`: session attached and required workspace placement applied;
- `already_open`: matching current window found;
- `unavailable`: captured session cannot be attached or resurrected;
- `degraded`: session restored but workspace or optional layout could not be fully applied;
- `failed`: launch or required correlation failed; and
- `stale`: no checkpoint satisfies the configured age/boot policy.

A session is never reported as restored solely because Kitty was launched. Successful session attachment and required workspace reconciliation must both be evidenced.

## Consequences

### Positive

- A power failure no longer destroys the association between terminal work and desktop organization.
- Manual users get one recovery command; automated users reuse the same idempotent path.
- Terminal-only defaults limit surprising application launches.
- Boot-aware history prevents current-startup activity from hiding the intended recovery state.
- Exact window correlation removes a known source of cross-session misplacement.
- Host configuration becomes smaller and product ownership becomes explicit.

### Negative

- Durable checkpoints add filesystem writes and require careful corruption tests.
- Full reconciliation every interval performs more repeated IPC and process inspection than change-driven capture, though the default one-minute cadence keeps this bounded.
- PID correlation constrains compatible terminal launchers; unsupported launchers must degrade explicitly.
- Exact column sizing and floating restoration depend on Niri IPC/action capabilities and may remain best effort.
- Existing consumers need a coordinated migration to avoid duplicate restoration.

## Alternatives considered

### Keep the host-local Niri scripts

Rejected. They duplicate product responsibility, identify terminals only by application/CWD, and cannot safely reattach the captured Zellij session.

### Rely on Zellij resurrection alone

Rejected. Zellij has no compositor workspace knowledge.

### Rely on Kitty session files

Rejected. Kitty can describe its internal tabs and windows, but Wayland compositor workspace placement belongs to Niri.

### Restore by application ID and launch order

Rejected for resume. Concurrent windows and variable startup timing can assign the wrong session to a workspace.

### Subscribe continuously to Niri's event stream

Rejected for the initial implementation. Sub-minute capture is not required for the recovery goal, while a long-running subscriber adds reconnect, debounce, lifecycle, and desynchronization complexity. A configurable one-minute full capture provides an acceptable recovery point and is easier to inspect and operate. Event-driven capture can be reconsidered if periodic reconciliation proves too expensive or insufficient.

### Always restore automatically

Rejected as the distributed default. Some users prefer an explicit command, stale checkpoints may be surprising, and GUI-session readiness differs between hosts. Automation remains a configuration choice.

### Restore all GUI applications by default

Rejected. Application state and duplicate behavior vary substantially, while the immediate recoverable identity contract is strongest for Kitty and Zellij.

## Compatibility and migration

1. Extend event/snapshot decoding additively so existing history remains readable.
2. Introduce boot-aware durable capture while preserving `capture once`, historical replay, and mirror snapshot output.
3. Add `redeem resume --dry-run` and item-level reconciliation tests.
4. Add the applying `redeem resume` path and Home Manager `restore.onStartup` option.
5. Enable Terminal Redeemer capture in a pilot consumer while leaving startup restore disabled.
6. Exercise manual power-loss/reboot recovery and idempotence.
7. Remove the pilot's host-local session scripts.
8. Allow other consumers to opt into startup restore.

## Validation criteria

The decision is successfully implemented when tests and an operator smoke test demonstrate that:

- two captured Zellij sessions in different named Niri workspaces return to those workspaces after a simulated new boot;
- a second `redeem resume` invocation creates no windows;
- an already-open session is skipped;
- a missing session is reported and is not recreated;
- an unrelated Kitty window opening during resume cannot steal another session's placement;
- current-boot captures do not replace the selected previous-boot checkpoint;
- a truncated trailing event after simulated power loss preserves the last complete checkpoint;
- unresolved workspace and stale-checkpoint policies are visible in dry-run and execution summaries;
- startup automation, when enabled, calls the same resume path; and
- historical restore and live mirror tests continue to pass.

## Non-goals

- Making Niri persist its own runtime IDs.
- Replacing Zellij's session serialization.
- General compositor support in this decision.
- Automatically restoring arbitrary GUI application state.
- Guaranteeing pixel-identical layout where Niri does not expose sufficient restore actions.
