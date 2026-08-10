You are building milestone M1 of `exo`, a new Go repo at `~/exo` (module `github.com/DiegoAvila-yeyo/exo`). This is a real build task, not planning — write actual Go code, run the tests you write, and make sure they pass.

## Context

`exo` depends on an existing repo, `github.com/yeyoos/nucleo-base` (referenced via a `replace` directive in `go.mod` pointing at `../nucleo-base`, both directories are siblings). `nucleo-base` already has: an agent runtime (`layer2-runtime-rails/agent`), a tool registry including `bash`/`edit`/`writefile`/`terminal_open`/`terminal_write`/`terminal_read`/`terminal_list`/`terminal_kill` (`layer2-runtime-rails/tool`), and a PTY session manager (`layer2-runtime-rails/terminal`, specifically `manager.go`, `process_unix.go`, `history.go`). Read those files first to understand what already exists — you are not reimplementing PTY spawning from scratch, you're building a new ownership/concurrency layer on top of (or alongside) the existing `terminal.Manager`.

This is the result of a 10-round design critique process (already closed, not open for re-litigation) captured in `~/tesla/DASHBOARD_TERMINAL_DESIGN.md` — read that file for full context/rationale before building. The relevant decisions for this milestone specifically:

1. **Single actor-goroutine per PTY session** owns all session-mutable state: `owner` (`"agent"` or `"human"`), a monotonic `epoch uint64`, and subscriber/output-channel state. All operations — write, read/subscribe, resize, takeover — are messages sent to that goroutine and processed one at a time from its own `select` loop. This serialization is what makes the epoch-based ownership model race-free: there is no separate lock spanning a check-then-act sequence, because only one goroutine ever touches the session's mutable state or the underlying PTY file descriptor.
2. **Epoch semantics**: every write and every read-subscription captures the epoch active at the time of the call. A takeover (switching `owner` and bumping `epoch`) is itself just another message processed by the same loop, so it can never race with an in-flight write/read from the old epoch — by the time the loop gets to the takeover message, all earlier messages (from the old epoch) have already been fully processed, and every message after it sees the new epoch. A write attempted under a stale epoch must return a typed error (call it `ErrOwnershipLost`) rather than silently applying or hanging. A read subscription tied to a stale epoch must be woken/closed (not left blocked forever) when the epoch bumps — do this by having the subscribe call return a `<-chan []byte` (or similar) that the owning goroutine closes when it rotates epochs, so the caller's blocking receive on that channel unblocks naturally when it closes.
3. **Bounded ring buffer + non-blocking fanout**: each session has a fixed-size in-memory ring buffer that already-scrubbed bytes get written into (a `scrub(data []byte) []byte` function — a stub/simple regex-based redactor is fine for this milestone, it doesn't need to be the full production secret-scanning logic yet, just wire the call site correctly so it's a single choke point). The owning goroutine must never block trying to push output to a slow or absent subscriber: give each subscriber a bounded channel; if it's full, drop that subscriber's connection (mark it disconnected) rather than blocking the actor loop. A reconnecting subscriber replays from the ring buffer before attaching to live output.
4. **PTY I/O must be abstracted behind a small interface** so this package's tests don't need a real PTY or terminal. Something like:
   ```go
   type PTY interface {
       Write(p []byte) (int, error)
       Read(p []byte) (int, error)
       Resize(cols, rows int) error
       Close() error
   }
   ```
   The actor should accept anything satisfying this interface (the real implementation, wrapping `nucleo-base`'s `terminal.Manager`, can come in a later milestone — for M1, a fake/mock implementation used only in tests is sufficient, plus the interface definition itself).

## What to build

A new Go package in `exo` (suggest `session/` or `ptyactor/` — pick a clear name and say what you picked) implementing:

- The `PTY` interface above.
- A `Session` type: one actor-goroutine per instance, started via a constructor (e.g. `NewSession(pty PTY) *Session`), with methods like `Write(data []byte) error`, `Subscribe() (<-chan []byte, func())` (returns a channel and an unsubscribe func), `Resize(cols, rows int) error`, `Takeover(newOwner string) error`, `Close() error`. Use whatever exact method signatures make sense given Go idioms — the point is the behavior, not matching these names exactly.
- `ErrOwnershipLost` as a typed/sentinel error.
- The bounded ring buffer + non-blocking fanout behavior described above.
- A minimal `scrub()` stub wired into the single point where bytes get persisted/delivered.

## Tests to write (this is the important part — these are the correctness-critical cases from the design)

Using a fake `PTY` implementation (in-memory, controllable from the test):

1. A write under the current epoch succeeds and reaches the fake PTY.
2. After a takeover bumps the epoch, a write attempted with the old (captured) epoch returns `ErrOwnershipLost` and does NOT reach the PTY.
3. A read subscription's channel is closed when the epoch bumps (simulating takeover happening while something is subscribed) — verify the subscriber observes channel closure, not a hang.
4. If a takeover happens while a read/write is genuinely in-flight (use goroutines + channels/sync primitives to construct this race deliberately, not `time.Sleep()` — the design doc explicitly calls out `Sleep`-driven race tests as something to avoid), the in-flight caller must end up with `ErrOwnershipLost`, never with mixed/incorrect output attributed to the wrong epoch.
5. A subscriber that doesn't drain its channel (simulate a slow/absent consumer) gets disconnected rather than blocking the actor loop — verify with a bounded-size channel that other operations (e.g., a write, or another subscriber's delivery) continue to succeed even while the slow subscriber is stalled.
6. Ring buffer replay: after N bytes are written (more than fit in the ring buffer), a new subscriber's replay only contains the most recent bytes up to the ring buffer's capacity, not the full history.

Run `go build ./...` and `go test ./...` in `exo` before reporting done, and paste the actual test output in your report, not a claim that it passed.

## What I want back

A report covering: what package/file layout you created, the exact public API you settled on (paste the type/method signatures), confirmation that `go build ./...` and `go test ./...` both succeed (with real output pasted in), and any design judgment calls you had to make that weren't fully specified above (there will likely be a few — e.g., exact channel buffer sizes, exact ring buffer size default — just pick reasonable values and say what you picked and why).
