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
  appAllowlist: {}
  appMode: {}                  # per_window or oneshot
  reconcileWorkspaceMoves: true
  workspaceReconcileDelay: 1200ms
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

All mirror keys above are typed under `programs.terminal-redeemer.mirror`. The NixOS wrapper forwards per-user settings to the Home Manager module. `extraConfig` remains available for additional raw YAML, but typed options should be preferred.

Capture-only environment compatibility remains:

- `REDEEM_NIRI_FIXTURE`
- `REDEEM_NIRI_CMD`
