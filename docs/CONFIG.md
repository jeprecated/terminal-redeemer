# Configuration

## Precedence

`redeem` resolves values in this order; later sources win:

1. built-in defaults
2. YAML (`${XDG_CONFIG_HOME:-~/.config}/terminal-redeemer/config.yaml`)
3. per-command CLI flags

`--config PATH` chooses a file explicitly. A missing/invalid explicit file is an error, except that `doctor` continues so `config_load` can report it. Repeatable mirror flags (`--ssh-option`, `--snapshot-arg`, `--scp-option`, `--mime-type`) replace their configured list on the first occurrence and append subsequent occurrences.

There are no host-specific defaults. `mirror.sourceHost` defaults to empty, so remote operations require configuration or `--host`.

## YAML schema

```yaml
stateDir: /home/user/.terminal-redeemer
host: local                    # capture/history partition identity
profile: default

capture:
  interval: 60s
  snapshotEvery: 100
  niriCommand: niri msg -j windows

processMetadata:
  whitelist: []
  whitelistExtra: []
  includeSessionTag: true

retention:
  days: 30

restore:
  onStartup: false            # manual resume is the distributed default
  appAllowlist: {}
  appMode: {}                  # per_window or oneshot
  reconcileWorkspaceMoves: true
  workspaceReconcileDelay: 1200ms
  maxCheckpointAge: 24h       # implicit resume only; explicit restore is unaffected
  unresolvedWorkspace: current # current, skip, or fail
  resumeTimeout: 10s          # bound for Niri readiness and each apply phase
  resumePollInterval: 100ms   # readiness/evidence polling cadence
  terminal:
    command: kitty
    zellijAttachOrCreate: true

mirror:
  sourceHost: ""
  sshCommand: ssh
  sshOptions: []
  snapshotCommand: [redeem, mirror, snapshot]
  launcherCommand: kitty
  selfCommand: redeem
  appID: terminal-redeemer-mirror
  defaultMode: attach          # attach or watch
  openDelay: 150ms
  niriCommand: niri
  clipboard:
    enabled: true
    command: wl-paste
    scpCommand: scp
    scpOptions: []
    kittyCommand: kitty
    tempDir: /tmp              # absolute; same path is used remotely
    mimeTypes: [image/png, image/jpeg, image/webp, image/gif]
```

`host` and `mirror.sourceHost` are deliberately different: `host` labels locally captured history and source-side snapshot JSON, while `mirror.sourceHost` is the SSH destination used by a consuming machine.

## Resume policy

`restore.onStartup` is policy consumed by the Home Manager module and reported by `redeem doctor`; it defaults to `false`. Setting it in a hand-written YAML file does not itself install a service. The Home Manager option `programs.terminal-redeemer.restore.onStartup = true` renders the same YAML value and installs the graphical user service. The NixOS wrapper exposes the same typed per-user option at `programs.terminal-redeemer.users.<name>.restore.onStartup`, while Home Manager remains the owner of the user service.

`redeem resume --dry-run` considers only complete, boot-aware checkpoints for the configured `host` and `profile`. It selects the newest checkpoint whose Linux boot ID differs from the current boot before checking whether that checkpoint is empty or stale. Legacy history without `boot_id` remains available to `restore apply --at` and `restore tui`, but is never selected implicitly.

`restore.maxCheckpointAge` defaults to the conservative 24 hours. An older selected candidate is reported as `stale`; resume does not fall back to an older checkpoint. `restore.unresolvedWorkspace` controls a per-terminal result when no current workspace matches by name, output plus index, or index:

- `current` (default): plan it as `degraded`, leaving eventual placement on the current workspace;
- `skip`: do not plan that terminal; or
- `fail`: report the item as `failed`.

CLI `--max-age` and `--unresolved-workspace` override these values for one invocation. `--timeout` and `--poll-interval` override the bounded in-process Niri-readiness wait and apply waits. Resume completes Niri readiness before checkpoint selection; the successful read is reused for initial reconciliation rather than queried twice. Historical restore settings, including `terminal.zellijAttachOrCreate`, do not weaken resume: resume launches Kitty directly with argv ending in `zellij attach -- <session>` (the delimiter safely supports leading-dash names) and never uses attach-or-create. `terminal.command`/`--launcher-command` must name a Kitty executable directly, not a shell command or daemonizing wrapper; a launcher whose returned PID never appears as the Niri client fails explicitly rather than falling back to app ID or window order.

## Mirror flag mapping

Common remote snapshot flags on `list` and `open`:

- `--host`
- `--ssh-command`
- repeatable `--ssh-option`
- repeatable `--snapshot-arg` (the complete remote argv list)
- `--snapshot-file` (test/offline input; bypasses SSH)

Launch overrides on `open`:

- `--mode`, `--launcher-command`, `--self-command`, `--app-id`, `--open-delay`
- `--no-clipboard`

Owned-window overrides on `status`/`close`:

- `--host` or `--all-hosts`
- `--app-id`, `--niri-command`

Clipboard overrides on `paste-image`:

- `--host`, `--ssh-command`, repeatable `--ssh-option`
- `--scp-command`, repeatable `--scp-option`
- `--clipboard-command`, `--kitty-command`, `--kitty-to`, `--temp-dir`
- repeatable `--mime-type`

The Kitty mapping supplies `--host` and `--kitty-to` automatically. `KITTY_LISTEN_ON` is the manual invocation fallback for `--kitty-to`.

## Validation

Config loading rejects invalid mirror modes, empty required commands/app ID, negative launch delay, and a non-absolute clipboard temp directory. Runtime validation rejects empty/unsafe SSH destinations, malformed remote snapshots, absent sessions, unsupported mode combinations, and unavailable executables with contextual errors.

## Home Manager / NixOS

All mirror keys above and `restore.onStartup` are typed under `programs.terminal-redeemer`. The NixOS wrapper forwards typed per-user startup policy and other per-user settings to the Home Manager module; it deliberately does not invent a system-level GUI restore service. `extraConfig` remains available for additional raw YAML, but typed options should be preferred.

Example (after disabling all host-local startup restorers):

```nix
programs.terminal-redeemer = {
  enable = true;
  restore.onStartup = true;
};
```

The generated service relies on `NIRI_SOCKET` being imported into the systemd user-manager environment and on Kitty/Zellij being available in the Home Manager or system profile. It retries failed readiness/apply attempts only five times within 30 seconds, then remains failed and journal-visible; there is no persistent retry loop.

Capture-only environment compatibility remains:

- `REDEEM_NIRI_FIXTURE`
- `REDEEM_NIRI_CMD`

The distributed default `niri msg -j windows` and its `workspaces` companion execute directly as argv through `exec.CommandContext`; they do not invoke a shell or login profile. An explicitly configured non-default `capture.niriCommand` remains a compatibility shell command and runs via non-login `sh -c`.
