# terminal-redeemer

`terminal-redeemer` provides separate, bounded terminal workflows for Niri, Kitty, and Zellij:

1. **`Mod+Return` — local terminal.** Niri launches Kitty directly; Redeem is not involved.
2. **`Mod+Shift+Return` — new dual-visible Lattice terminal.** Overton alone creates the persistent session, then Redeem best-effort opens an attach-only source Kitty.
3. **`Mod+Ctrl+Return` — manual picker.** Browse or reopen visible and headless live Lattice sessions without persistent selection.
4. **Pinned save/apply.** Manually replace and later apply one exact projection set; it is independent of rolling checkpoints.
5. **Foreground workspace follow.** Temporarily add missing visible projections from one runtime-selected workspace until the command exits.
6. **Same-boot and reboot recovery.** Rolling local checkpoints let `resume --all` reconcile current exact ACTIVE sessions after Niri restarts and restrict reboot resurrection to the newest prior-active allow-list.

The Home Manager module exports the three mirror shortcuts in one opt-in Niri fragment and exports a separate opt-in recovery startup fragment. It also exports direct argv for new/open/save/apply/follow, but installs no follow service, timer, rule, or saved selection.

## Remote sessions

```bash
redeem mirror new --host lattice --source-workspace agentleman  # create once, view on both hosts
redeem mirror open --host lattice                               # project-first picker
redeem mirror list --host lattice      # non-interactive live inventory
redeem mirror save --host lattice      # replace the pinned live projection set
redeem mirror apply --host lattice     # manually reopen that pinned set
redeem mirror follow --host lattice    # temporarily follow one selected workspace
```

Closing an Overton mirror window detaches it; the Zellij session continues on Lattice and remains discoverable. Newly launched mirror windows keep one local helper across transport loss and reconnect only to their original session incarnation. Offline typing, paste and retry Enter are discarded, not replayed. One on-demand recovery process shares host checks and admits at most two pending attachments. Existing direct-SSH windows remain recognizable but are not retrofitted.

`mirror new` invokes creation once on the source, receives the exact incarnation, then opens an attach-only persistent view. A lost creation receipt is an explicit uncertain outcome: no automatic retry or name-only adoption. See [persistent reconnect requirements and acceptance](docs/testing/persistent-mirror-reconnect.md).

`mirror save` always refreshes the remote snapshot and trusts authenticated helper identity and complete process ancestry below owned Niri window PIDs (or legacy exact SSH/Zellij evidence); titles are presentation, so ambiguous or untracked windows are reported and excluded. The persisted host is the exact SSH destination token, not a canonical-host claim for aliases. Save atomically replaces one mode-0600 pin per host/profile under `STATE_DIR/mirror/pins/`, outside rolling checkpoints. `mirror apply` refreshes and preflights exact ACTIVE sessions, skips already projected sessions, never creates missing sessions, and opens the rest in locally captured order. Each new window is moved once and receives supported floating/tiled size actions; exact Niri column/stack reconstruction is intentionally unsupported. Both commands support side-effect-free `--dry-run`.

`mirror follow` is a foreground-only temporary additions loop. Select a current source workspace with arrow keys; printable `j` and `k` filter normally. Redeem freezes the matching local workspace identity, polls complete source snapshots no faster than two seconds, opens only missing visible exact ACTIVE sessions, and moves each new projection once. Defaults are four detached launch attempts per poll and 64 for the run; attempts are charged before invocation so an uncorrelated launch cannot bypass `--max-total`. `--max-per-poll`, `--max-total`, and `--interval` are explicit overrides. It never closes, reorders, resizes, or continually repositions windows. Press `q` or Ctrl+C to stop.

### Picker controls

The picker groups window-backed sessions by exact Niri workspace identity, using a readable fallback label for unnamed workspaces, and keeps live headless sessions visible in a separate **Headless Zellij** section. Project and JJ-workspace identities use the same stable, path-derived coloured chip treatment as Mono/Auto's project footer; identity is resolved on the source host so canonical repository/workspace labels remain accurate. Type any printable character—including `j` and `k`—to filter by project, activity, session, workspace, or CWD. Only `↑` and `↓` move the current row.

- `Space` toggles the current session.
- `Ctrl+A` toggles all sessions matching the current filter.
- `Enter` opens checked sessions in discovery order, or the current session when none are checked.
- `Esc` clears a non-empty filter; press it again to cancel.

Automation can continue to bypass the picker with `--all`, repeatable `--session NAME`, or one-based `--select N`.

`mirror new` remains safe under partial failure: the Overton command is the sole creation authority, delegating one bounded creation to the source before opening its persistent viewer. Then one bounded source-local helper opens an attach-only Lattice Kitty. `--source-workspace NAME_OR_NUMBER` optionally moves that source window once; omission uses normal Niri placement. A missing display or source placement failure is reported as a warning without killing the session or Overton view. Creation and exact attachment require the upgraded source helper; upgrade the source first. Redeemer uses the source machine's `zellij` from `PATH`, with no version pin or release allowlist. Unsupported command or IPC behavior fails closed. With no connected monitor, Niri may retain the source Kitty without making it physically visible.

Source-side and owned-window support commands are:

```text
redeem mirror snapshot
redeem mirror status
redeem mirror close --host lattice
redeem mirror paste-image
```

To open a remote service or file in the local browser, run `redeem mirror forward --host lattice 5173` (a remote port) or `redeem mirror forward --host lattice ~/proj/report.html` (a remote file or directory, served by the source's `python3`). Redeem picks a free local port, waits until the remote end answers HTTP, prints the URL and runs `xdg-open`. Each forward is a detached `ssh -L` that closes itself: a port tunnel stays up for 5 minutes and then until its last connection closes; a file server exits after 5 minutes without a request. There is nothing to list or clean up.

## Terminal recovery

```bash
redeem capture once              # atomically refresh this boot's rolling checkpoint
redeem resume --all --dry-run    # inspect same-boot ACTIVE or reboot prior-active recovery
redeem resume --all              # idempotently reconcile the full eligible set
redeem resume                    # narrower, prior-visible manual resume remains available
redeem prune run                 # bound retained boot checkpoints
redeem doctor                    # read-only recovery and mirror diagnostics
```

Capture stores one complete checkpoint per boot, host, and profile, including an exact ACTIVE-session allow-list and sticky placement. Publication holds the shared writer/operation lock and uses a mode-0600 temporary file, file fsync, atomic rename, and directory fsync. On the same boot, `--all` uses the current exact ACTIVE catalog and sticky placement; after reboot it selects the newest matching prior recovery point and permits only names in that prior-active allow-list. Unrelated resurrection-cache entries are never candidates.

`--max-age` blocks stale prior dead-session resurrection and warns about stale sticky placement, but does not block attaching a session that is currently exact ACTIVE. Named workspaces are preferred over output/index and index-only fallbacks; `doctor` warns about tracked placement that depends on unnamed indices. Recovery launches Kitty directly with `zellij attach -- <session>`: it never creates, uses attach-or-create, or passes force-run flags. It reconciles existing windows before missing ones, then restores deterministic terminal column order when each target occupies its own column. Stacked rows are reported unsupported rather than guessed, and unrelated window ordering is not reconstructed.

## Setup

- [Configuration](docs/CONFIG.md)
- [Operations and physical smoke checklist](docs/OPERATIONS.md)
- [Prior-boot resume decision](docs/adr/0001-resume-zellij-terminals-in-niri.md)
- [Bounded mirror continuity decision](docs/adr/0002-bounded-mirror-continuity.md)

Home Manager can schedule capture and optionally install the `redeem resume --all` oneshot for initial graphical-session startup. When enabled, consumers must include the generated `resume.niriIntegrationFragment` exactly once in Niri's configuration; its single native `spawn-at-startup` hook synchronously imports the new compositor's graphical environment, including `NIRI_SOCKET`, before restarting that same service on every compositor start. The fragment supplies restart behavior and `onStartup` alone supplies only the initial graphical service. Periodic capture remains ordered afterward. Startup recovery is disabled by default. Disable any competing startup terminal restorer before enabling it.

Physical deployment, activation, dual-host validation, and reboot testing remain user-owned.
