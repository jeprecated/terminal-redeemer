# Exact mirror attachment (milestone 1)

This is the **source-side prerequisite**, not enabled persistent reconnect.
Public `open`, `new`, `apply`, and `follow` launch planning is unchanged. The
physical-terminal input gate and shared host scheduler are still required before
cutover. In particular, this private helper is not a standalone safe reconnect
loop and does not claim to discard physical input or share thirty host checks.

## Identity and the replacement race

`mirror session-catalog` and additive snapshot IDs now use boot ID, literal
session name, socket device/inode **and socket birth time**. A hermetic real
Zellij test demonstrated immediate inode reuse after socket removal and session
recreation: boot + inode alone was insufficient. `statx` birth time distinguishes
that reuse without changing when a hard link is made. Unsupported/missing birth
time fails closed; there is no inode-only fallback. The historical checkpoint
`Session.ID` is unchanged; reconnect uses the additional `Session.ExactID`.

For a single attachment, the source helper:

1. Opens the source directory components with `O_NOFOLLOW`, then holds directory
   descriptors while checking the expected incarnation.
2. Creates a random, owner-only attempt directory on the same filesystem. It
   hard-links the socket and **rechecks the identity of that link**. A replacement
   before linking is rejected; one after linking cannot redirect the endpoint.
3. Exposes only a private relay socket called `session` in an isolated Zellij
   socket namespace. The alias avoids leading-dash CLI ambiguity and long Unix
   socket paths; it does not rename the real server session. `/proc/<helper>/fd`
   addresses keep the namespace tied to held directory descriptors.
4. Scrubs inherited `ZELLIJ`/`ZELLIJ_*` context and supplies an empty, attempt-local
   resurrection cache. The command is `attach session options --on-force-close
   detach`, with no creation flag.
5. Reaps the client, stops the relay and removes only its own attempt directory.
   Directories are never adopted/reused, and cleanup checks directory identity.
   Uncatchable helper death can leave an inert private directory; nothing scans
   it for reconnect authority. The normal runtime socket base is boot-volatile.

## Positive readiness, not a delay

Redeemer uses the source machine's `zellij` from `PATH`: no release pin,
version probe, or release allowlist. Compatibility is checked through actual
CLI results, socket identity and attach/render messages, not a version string.
The protocol references below were reviewed against Zellij **0.45.1**; that is
validation evidence, not a runtime requirement.

- [IPC framing](https://github.com/zellij-org/zellij/blob/v0.45.1/zellij-utils/src/ipc.rs#L495-L525): little-endian 32-bit length followed by protobuf.
- [Client oneof](https://github.com/zellij-org/zellij/blob/v0.45.1/zellij-utils/src/client_server_contract/client_to_server.proto#L6-L36): `AttachClient` is field 8; `ConnStatus` is field 13; creation is field 7. Terminal capability/focus messages through field 27 are understood; session-list request 25 and unknown fields are refused on the attached connection.
- [Server oneof](https://github.com/zellij-org/zellij/blob/v0.45.1/zellij-utils/src/client_server_contract/server_to_client.proto#L6-L28): `Render` is field 1; `Connected` field 4 can answer a mere status probe.
- [Attachment handling](https://github.com/zellij-org/zellij/blob/v0.45.1/zellij-server/src/lib.rs#L1186-L1250) registers that client and sends `ScreenInstruction::AddClient`.
- [Screen registration](https://github.com/zellij-org/zellij/blob/v0.45.1/zellij-server/src/screen.rs#L10199-L10232) adds it to the screen.
- [Client rendering](https://github.com/zellij-org/zellij/blob/v0.45.1/zellij-client/src/lib.rs#L1418-L1433) writes and flushes the received render to stdout.

The relay forwards bounded opaque frames (4 MiB maximum), inspecting only the
single-oneof envelope and narrow lifecycle messages. A preliminary `ConnStatus`
connection cannot produce readiness. There must be `AttachClient`, followed by a
server `Render`, **on the same pinned connection**. The relay inserts a synthetic
Render containing the attempt marker immediately before the first real render.
The Zellij client itself writes it, so helper/client output cannot interleave the
marker. It appears once, after Zellij processes that connection's render stream.
SSH startup, arbitrary stdout, `Connected`, EOF and elapsed time do not qualify.

Creation, attachment replay, watcher attachment and kill-session protocol
requests are refused. A server `SwitchSession` is converted to detach rather
than allowing Zellij's client loop to switch or create a session. This is a
fixed-incarnation view, not a session switcher. Normal server-directed exit or
detach is distinguished from EOF; successful child/SSH exit alone is insufficient.

Private operations:

```text
redeem mirror session-catalog
redeem mirror session-attach --session NAME --session-id ID --attempt 32_HEX_DIGITS
```

The latter has a default 15-second readiness deadline, not a session lifetime
limit. Markers are `RS REDEEM_ATTACH_V1:<attempt>:<event> US`. The future local
supervisor must validate a fresh current attempt and consume markers before
allowing input; they are not a general authorization protocol against a hostile
same-user process. The outcomes distinguish `missing`, `replaced`, `unverifiable` identity,
`unsupported` capability, `invalid` requests, ordinary `failed` attachments,
`cancelled` attempts and validated `detached` clients. Missing/replaced socket
observations are advisory, **not** authoritative session-end evidence. Only a complete fresh host inventory can
establish end. Missing helpers and unsupported protocol behavior must never fall back to
name-only attachment. Deploy the source capability before future local cutover.

## Hermetic verification

Tests use private socket/config/cache/data/runtime/HOME directories, real PTYs,
and the installed Zellij. They never use the operator's socket namespace.
Real-Zellij tests skip only when `zellij` is absent from `PATH`; an installed
binary with incompatible behavior fails the tests rather than being skipped.
A skip is not evidence of readiness. Tests include:

- Case-sensitive names with spaces and leading dashes, client-rendered readiness,
  fresh shell input, intentional detach, cancellation and survival of the source
  session.
- Actual same-name replacement after socket removal, including inode reuse.
- Replacement while a client is deliberately paused **after pinning**: post-ready
  input reaches the original shell, not the newly created same-name server.
- Status replies and generic startup stdout cannot signal readiness; timeout,
  malformed/oversized frames, creation/replay rejection and EOF are tested.
- Reboot identity, stable birth identity across hard links, symlink rejection,
  distinct attempt cleanup and refusal to remove a replaced attempt directory.
- Existing checkpoint identities, wrapper prefixes and unattended SSH option
  precedence remain covered.

```sh
go test ./...
go test -race ./internal/mirror ./internal/zellijlive ./cmd/redeem
go test -race ./internal/mirror \
  -run '^TestRealZellij' -v -count=3
go vet ./...
nix build path:.#terminal-redeemer --no-link
```

Historical milestone-1 checkpoint (Zellij 0.44.3): all commands above passed. All four real-Zellij test groups
ran (no skips) in each of the three race repetitions, including both literal-name
subtests. The package build was forced local with `--builders '' --max-jobs 1
--cores 2`; the machine's default configuration disables local jobs. Dependency
hash was recomputed as `sha256-hYELoFa+ppt/C6GM+ld+bZS9xSJoBGauwLWEu1UQxnM=`
after adding `github.com/creack/pty v1.1.24` for the real PTY fixtures.

These do not replace milestone 2's physical-input discard/backpressure tests,
milestone 3's cross-process thirty-window checks, or operator-coordinated network
and hibernation smoke tests. No deployment or real-session disruption is part of
this checkpoint.
