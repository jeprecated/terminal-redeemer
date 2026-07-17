# Operations

## Dependencies and platform boundary

History restore and live mirroring are separate paths. Live mirroring currently requires:

- `ssh` and source-side `redeem mirror snapshot`
- local Kitty and Niri for opening/listing/closing mirror windows
- source-side Zellij
- `wl-paste`, `scp`, and Kitty remote control when the image bridge is enabled

Commands are configurable. `redeem doctor` always checks the existing capture/restore dependencies; when `mirror.sourceHost` is configured it additionally checks mirror SSH, launcher, Niri, and enabled clipboard/SCP executables. Doctor does not connect to the source.

A non-Niri compositor cannot provide owned-window status/close. A non-Kitty launcher must implement the Kitty flags/control behavior used by the planner. Failures are reported rather than silently operating on unrelated windows.

## Home Manager and NixOS

Enable `programs.terminal-redeemer.enable = true`. Home Manager writes `~/.config/terminal-redeemer/config.yaml`, installs the selected package, and optionally manages capture/prune timers. The capture timer starts and stops with `graphical-session.target`, waits one configured interval before its first activation, and repeats every configured interval while the graphical session is active. Its default interval is 60 seconds (`capture.interval = "60s"`). Each activation runs the same `redeem capture once` full reconciliation available to operators. A failed Niri windows/workspaces query exits the oneshot visibly in the user journal without appending a partial checkpoint; the next timer activation retries from a fresh full query.

The NixOS wrapper requires the Home Manager NixOS module and forwards `programs.terminal-redeemer.users.<name>`.

Use build/evaluation before activation:

```bash
nix flake check 'path:.'
```

## Source setup and smoke checks

On the source host:

```bash
redeem mirror snapshot
```

The command emits one JSON object containing `host`, `profile`, `generated_at`, and ordered `windows`. Terminal windows may contain top-level and nested `zellij_session`, plus `terminal.cwd`. The consumer rejects malformed JSON and incomplete required envelope/window metadata.

On the consuming host:

```bash
redeem mirror list --host source.example
redeem mirror open --host source.example --all --dry-run
```

`--dry-run` on `open` still acquires/validates the snapshot but does not run Kitty. For fully offline checks, add `--snapshot-file PATH`.

## Owned-window lifecycle

Terminal Redeemer marks mirror Kitty windows with `mirror.appID`. Status and close first decode `niri msg -j windows`, then filter by exact app ID. With `--host`, they additionally require the generated title prefix `<host>[`; `--all-hosts` removes only that host filter, never the app-ID ownership filter.

```bash
redeem mirror status --host source.example
redeem mirror close --host source.example --dry-run
redeem mirror close --host source.example
```

Always inspect dry-run output before destructive close. Closing a local mirror window does not kill its remote Zellij session.

## Image bridge

Each launched window gets a unique Kitty control socket and Ctrl+V mapping. `paste-image`:

1. reads advertised Wayland MIME types;
2. chooses the first configured supported image MIME;
3. reads binary clipboard bytes into a mode-0600 uniquely named local file;
4. creates the quoted remote directory through SSH and copies with SCP;
5. injects the identical remote path into that Kitty instance;
6. removes the local temporary file.

The remote file is intentionally retained for the remote consumer. Arrange separate `/tmp` cleanup according to source policy. If clipboard inspection/data is unavailable or not an image, raw Ctrl+V is forwarded. SSH/SCP failures are errors and do not inject a nonexistent path.

## Security assumptions

- Hosts and snapshot metadata are validated/quoted. Local process execution uses explicit argv rather than `sh -c`.
- SSH necessarily sends a remote shell command. Snapshot argv, CWD, session name, and remote mkdir path use POSIX single-quote escaping, covered by tests.
- SSH/SCP option lists and executable paths are operator-controlled configuration. Treat the YAML as trusted: SSH options such as `ProxyCommand` can intentionally execute local programs.
- The app ID is the ownership boundary for close. Do not assign it to unrelated applications.
- SSH host keys, authentication, authorization, remote command availability, and remote temp-file confidentiality remain the operator's responsibility.
- The image bridge copies clipboard data to the configured source host. Disable it for sensitive workflows or untrusted hosts.

## Troubleshooting

- `source host is required`: set `mirror.sourceHost` or pass `--host`.
- `decode/malformed remote mirror snapshot`: verify the remote `redeem` version and run its snapshot command directly.
- SSH failure: test normal non-mutating SSH separately; inspect `sshCommand`, `sshOptions`, and `snapshotCommand`.
- no Kitty/Zellij windows: source snapshot windows need `app_id: kitty` and Zellij session metadata.
- Niri/Wayland error: run from the graphical user session and verify `NIRI_SOCKET`; status/close do not support other compositors yet.
- launcher failure: verify Kitty accepts `--detach`, `--class`, `--listen-on`, `--override`, and `-e`.
- image fallback only: inspect `wl-paste --list-types`, configured MIME preference, Kitty remote-control socket, and SCP command/options.
- nested key interception: use the default fresh-Kitty launcher; the remote attach/watch command clears Zellij environment variables.

## Prior-boot resume

```bash
redeem resume --dry-run  # selection and reconciliation only
redeem resume            # apply the same plan
```

The dry run is non-mutating: it reads complete checkpoints, current Niri workspaces/windows and process metadata, and `zellij list-sessions --short`; it never attaches, creates, launches, or moves anything. Output starts with the selected prior boot ID and capture time, followed by stable `resume_item` records and a status-count summary. Applying adds `restored` results and separately reports `layout_status`; the other item statuses are `ready`, `already_open`, `unavailable`, `degraded`, `stale`, `failed`, and `skipped`.

The newest prior-boot candidate is authoritative even when it is empty. `empty`, `stale`, and `not_found` candidate statuses are visible no-ops rather than reasons to select older history. Their output includes actionable guidance to use `redeem restore tui` or `redeem restore apply --at ...` for explicit forensic selection, including legacy records without boot IDs.

Apply launches each Kitty process directly, with no outer shell, and passes exact attach-only argv: `zellij attach <session>`. Zellij environment variables are removed to avoid accidental nested-session behavior. Resume accepts a launch only when all required evidence is present:

1. the launched process supplies a positive PID;
2. exactly one Niri window with that client PID appears before the configured timeout;
3. a live descendant process has exact argv `zellij attach <session>`; and
4. after the Niri move succeeds, the same window ID and PID is observed on the resolved runtime workspace.

There is no app-ID, creation-order, or nearest-window fallback. A launcher that forks/daemonizes or a Kitty/Niri combination that does not preserve client PID identity is reported as `failed`. Correlation or attachment timeout kills the launched process so it cannot leak an unowned terminal. A failed required workspace move is also `failed`, but the successfully attached terminal is deliberately left open; a rerun detects it as `already_open` and does not create a duplicate.

Workspace resolution uses captured durable metadata in this order: exact name, output plus index, then index. See `restore.unresolvedWorkspace` in `docs/CONFIG.md` for unresolved-target behavior. With the `current` policy, an attached session whose target cannot be resolved remains `degraded`, never `restored`. Floating state and supported width/height actions are attempted only after required placement; column ordering is reported as unsupported because Niri cannot target that action by window ID safely. Optional layout failures do not change a required `restored` result.

An `already_open` result comes from a current terminal with the same verified Zellij session, or from the exact `/proc` attachment evidence checked immediately before each launch. Mere presence in Zellij's session list means available, not open. Missing sessions and attach exits are `unavailable` and are never recreated.

## Existing capture/restore operations

```bash
redeem capture once
redeem history list
redeem history inspect --at <RFC3339>
redeem restore apply --at <RFC3339> --dry-run
redeem restore tui
redeem prune run --days 30
```

Replay discards a malformed trailing event after a crash but reports corruption if malformed data appears before a later record. Snapshots remain an optional optimization. Capture and prune coordinate through a crash-recoverable advisory lock; a leftover `meta/lock` file is harmless, while prune still reports an active writer when the lock is held.

## Deferred work

This milestone intentionally does not provide continuous reconciliation, an always-running daemon, duplicate-window suppression across repeated `open` calls, or a pane-rich full-screen mirror TUI. The Go discovery/planning interfaces are intended to support those later without moving application logic back into host configuration repositories.
