# Persistent terminal isolation (milestone 2)

`RunSessionSupervisor` owns one physical terminal and replaces only its separate
SSH PTY. This is an internal core, **not a public launch cutover**. The next
milestone supplies the real shared coordinator and private CLI adapter. Tests use
a controlled admission adapter; there is no per-window host retry implementation
or restored slice controller.

## Input boundary

The states are offline, checking, connecting and ready. Only positive evidence
from the current, unexpired attachment can open the input gate. SSH startup,
arbitrary output, successful exit and stale nonce markers cannot do so. Transport
loss closes the gate immediately; a later transport has a fresh attempt nonce,
a different PTY and a monotonically increasing local input generation.

One nonblocking reader owns physical input. Reads, stream classification and the
readiness drain share a lock; network calls and queue delivery never hold it.
Queued bytes retain their read generation. Input and output queues are bounded.
At readiness, the helper parses and discards the kernel backlog until an empty
nonblocking read, then opens the new generation. **Blind `TCIFLUSH` is not safe
here:** it could remove an unread paste-start delimiter and admit its later tail.
The readiness drain is bounded; continuous offline input does not freeze control.

Waiting screens enable bracketed paste. A paste keeps its originating generation
through its closing delimiter, including a delimiter split across reads. An
unfinished offline paste/partial delimiter delays opening the gate; expiry still
cancels the pending attachment. Paste contents do not act as retry commands.
Fresh ready-state paste delimiters and contents are forwarded unchanged. An
unframed byte arriving after the readiness boundary is new terminal input; the
asynchronous `paste-image` completion path still needs explicit origin-generation
binding in milestone 4. This checkpoint does not claim that integration is done.

Retry Enter is consumed locally and acknowledged without awaiting control. The
control exchange is asynchronous, single-flight and has a one-second context;
late successful replies are rejected after that context expires. Its adapter
must honour cancellation. Retry intent survives a failed exchange. Only grants
for the helper's current proposed nonce can start a child; acknowledged loss
rotates that nonce. Events retain their attempt identity until acknowledged.
A reset cancels pending work but does not tear down an established ready child.

Before readiness, remote output is retained up to 4 MiB instead of overwriting
the connecting screen. Overflow or stalled output fails the attempt closed. The
buffer is released on readiness or loss. Ordinary prompts are not held waiting
for another read merely because the marker decoder retains partial prefixes.

## Terminal and process lifecycle

- Raw mode and original descriptor flags are saved and restored. Physical input
  is flushed on final exit, after stopping the reader.
- Child PTYs have their own controlling session (`Setsid`); no incompatible
  `procrun.Setpgid` is applied. Initial size and `SIGWINCH` updates are forwarded.
- Input/output writes use nonblocking syscalls with a 100 ms bound. A full paste,
  stalled SSH reader or blocked terminal cannot indefinitely hold the event loop.
- SSH stdout/stderr use an owned pipe, not `Cmd.StdoutPipe`. Output drainage is
  bounded to one second even if an escaped descendant retains that descriptor.
- Linux `waitid(WNOWAIT)` observes child exit before reaping. Cleanup kills the
  owned process group while its leader PID is still reserved, then calls `Wait`;
  a delayed cleanup cannot signal a recycled process group.
- Only a validated ready-then-detached outcome ends the helper intentionally.
  Exit status zero alone reports loss and awaits shared admission. Signals and
  terminal closure cancel/reap the child without touching any real session.

## Hermetic tests

Real PTYs and isolated test processes cover:

- Offline and connecting text, large paste, retry Enter, and fresh ready input.
- Paste crossing readiness and every split of its delimiters; an unread start
  still in the kernel queue when readiness arrives.
- Repeated transport replacement using the same physical terminal; old readiness
  and expired/replayed grants cannot enable a new attachment.
- Resize, a saturated child-input buffer and subsequent successful recovery.
- Blocked/late control responses, immediate local retry feedback, and ready
  transport survival across a control reset.
- Descriptor-holding descendants, bounded writes, deliberate detach versus exit
  zero, signal/physical-terminal closure, child reaping, termios/flag restoration.

```sh
go test ./...
go test -race ./internal/mirror ./internal/zellijlive ./cmd/redeem
go test -race ./internal/mirror \
  -run '^Test(SessionSupervisor|SessionTerminalWrite|SessionTransportBounds|SessionInputPaste|SessionGate|AttachmentOutput|RealPinnedZellij)' \
  -v -count=3
go vet ./...
nix build .#terminal-redeemer --no-link --builders '' --max-jobs 1 --cores 2
```

Checkpoint results: all commands above passed. All listed terminal tests and all
four real-Zellij source-side groups ran in each of the three race repetitions,
including signal/terminal-close and literal-name subtests; there were no skips.
The local Nix build passed with the existing dependency hash unchanged:
`charmbracelet/x/term` became a direct import but was already vendored. Source-side
proof remains documented in [exact attachment](exact-mirror-attachment.md).
Thirty-window sharing, production ownership/clipboard integration and physical
Kitty/Niri/network/hibernate tests are not proven by this isolated core. Public
launches remain unchanged, and no deployment or real-session disruption is
performed.
