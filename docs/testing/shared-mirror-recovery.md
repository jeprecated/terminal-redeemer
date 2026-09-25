# Shared mirror recovery (milestone 3)

The private `mirror session-supervisor` / `mirror session-recovery` commands now
connect the isolated terminal core to runtime-only host recovery. Public launch
planning remains unchanged until workflow and asynchronous clipboard integration.

## Ownership and scheduling

- One owner-only runtime directory per exact configured transport (destination,
  SSH executable/options and source helper prefix). Aliases are not merged.
- Stable, inode-checked `flock` election; an inherited lock and startup receipt
  prevent racing starters from launching multiple daemons. The lock survives
  until handlers and the single inventory worker stop. A small probe watchdog
  retains that same lock across coordinator death, watches its parent pidfd and
  cancellation pipe, and cancels/reaps the SSH group before releasing election.
- Directory-FD-relative Unix socket access avoids pathname length limits and
  rejects symlink substitution. Frames are versioned, bounded to 16 KiB and
  deadline/cancellation bound; both ends authenticate the peer UID.
- Kernel `SO_PEERPIDFD` (Linux 6.5+) binds membership to a live process, not just
  a reusable PID or token. Unsupported kernels fail closed. An inherited socket
  cannot preserve authority after the original process dies.
- One complete headless inventory check supplies a shared retry deadline:
  1, 2, 4, 8, 16, then 30 seconds indefinitely. Enter requests coalesce with
  in-flight checks and have a one-second minimum start interval.
- Two pending attachment slots, fair rotation, session-specific failure delay,
  immutable client identity and attempt-bound grants. Older connecting/status
  messages cannot revoke a replacement, and expiry is sticky across clock
  rollback. Expiry requests exact
  attempt cancellation but retains capacity until reap acknowledgement. Two
  helper-held kernel slot locks additionally enforce the physical pending limit
  across coordinator restart; release follows actual readiness or child reap.
- Control EOF is not reap evidence: an unready grant retains its process-bound
  slot across connection loss. Only that live process can resume it. Orderly
  client close follows transport cleanup and explicitly releases membership.
- Coordinator restart preserves ready transports; unacknowledged connecting
  attempts reset. No registry or desired-window state is written.
- Failed/incomplete/non-increasing observations are unknown, never session end.
  Negative evidence only applies to members present when that probe began.
  Wall deadlines also reject buffered pre-suspend replies when a monotonic
  context timer has not yet fired.
- Native discovery, including follow, queries existing shared outage status
  without starting a daemon, creating runtime directories, or issuing competing
  SSH discovery calls. Healthy discovery and custom-runner tests retain their
  existing paths.
- Catalog process output is capped during acquisition (1 MiB stdout, 64 KiB
  stderr), not merely after allocation. Displayed diagnostics remove controls.

## Hermetic evidence

`session_recovery*_test.go` covers the pure scheduling model and real processes:

- Thirty client processes racing startup produce one daemon and one blocked
  inventory process. Retry storms and discovery add no competing SSH work.
- Status/retry requests remain responsive during the blocked check; admission
  never exceeds two pending grants.
- A hard daemon kill through its verified pidfd triggers one replacement, with
  thirty ready registrations restored and no re-grants of healthy transports.
- Unrelated processes cannot adopt another client's identity; a socket retained
  by a descendant does not outlive its original peer's authority.
- Killing a coordinator during a blocked check drains the old SSH process before
  a replacement starts its probe, without forgetting other helpers' slot locks.
- Pending control disconnection does not free capacity; same-process reconnect
  resumes the grant, and explicit release admits the next member.
- Stale sockets recover, transport identities remain isolated, and the last
  departing member lets the daemon stop and cancel/reap outstanding probes.

All fixture executables, catalogs, runtime directories and source sessions are
isolated. No real SSH host, Kitty/Niri window, network outage, suspend or operator
session is used. Real pinned-Zellij tests use a fixture-only single-key Detach
binding: batching Ctrl-o and d races its asynchronous mode switch and does not
reliably request a detach. The readiness/incarnation assertions are unchanged.

Full Go tests, targeted races, three combined supervisor/source-Zellij/recovery
repetitions (no skips), vet, and ten extra thirty-process race repetitions passed.
Fixtures reconnect transiently dropped idle status connections rather than
mistaking an unknown reply for a zero-member observation. PID liveness polling
retries EINTR and rejects closed descriptors; neither is process-death evidence.
A local Nix package build passed; final build status is in the implementation plan.

Validation commands:

```sh
go test ./...
go test -race ./internal/mirror ./cmd/redeem
go test -race ./internal/mirror -run '^Test(SessionRecovery(ThirtyProcessesShareBlockedProbe|StaleSocketAndIdentityIsolation|RejectsDeadPeerWithRetainedSocket|ControlDisconnectRetainsPendingCapacity|RestartPreservesProbeAndPendingBounds)|SessionSupervisor|RealPinnedZellij)' -v -count=3
go vet ./...
```

Physical window identity/placement, real suspend/network recovery and deployment
remain operator-only acceptance checks; these tests do not claim those results.
