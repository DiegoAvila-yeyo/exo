You are building the second piece of M8, spanning two repos: `~/nucleo-base` (module
`github.com/yeyoos/nucleo-base`) and `~/exo` (module `github.com/DiegoAvila-yeyo/exo`, already
depends on `nucleo-base` via a `replace` in `go.mod` pointing at `../nucleo-base`). Real build
task — write actual Go code, run the tests you write, `go test -race` clean in both repos.

## Context

Read `~/exo/M8_INTEGRATION_DESIGN.md` in full first — it is the canonical, closed design (3 rounds
of Claude↔Codex critique) for all of M8. This build prompt implements piece 2 of 3: the terminal
tool interface swap in `nucleo-base`, and the new terminal adapter in `exo` that implements it.
Piece 1 (already built and reviewed) added `sessions.CreateWithOptions`/`realpty.WithCommand` to
`exo` — read `~/exo/sessions/manager.go` and `~/exo/realpty/realpty.go` now, as shipped, to see
what you're building on. Piece 3 (new SSE chat/approval routes in `termserver`, plus widening the
approval callback) is a separate build prompt that comes after this one.

Also read before writing code:
- `~/nucleo-base/layer2-runtime-rails/terminal/manager.go` — the existing `terminal.Manager` you
  are NOT modifying, but whose behavior (classifier, error shapes, `ToolEnvelope` semantics) the
  new adapter must faithfully match for every case in the mapping table below.
- `~/nucleo-base/layer2-runtime-rails/terminal/types.go` — `ToolEnvelope`, `SessionMeta`,
  `OpenOptions`, `ReadOptions` struct definitions.
- `~/nucleo-base/layer2-runtime-rails/tool/terminal_open.go`, `terminal_read.go`,
  `terminal_write.go`, `terminal_kill.go`, `terminal_list.go`, `terminal_common.go` — the five
  tool structs whose `Manager *terminal.Manager` field you're retyping to an interface.
- `~/exo/ptyactor/session.go` — `Lease`, `Write`/`WriteWithLease`, `Subscribe`,
  `Resize`/`ResizeWithLease`, `Takeover`, `ErrOwnershipLost`. You are not modifying this file.
- `~/exo/sessions/manager.go` (as shipped after piece 1) — `CreateWithOptions`, `Get`, `Close`.

## Part A — `nucleo-base`: the interface swap (small, mechanical, per round 1)

In `layer2-runtime-rails/tool` (new file, e.g. `terminal_backend.go`), define:

```go
type TerminalBackend interface {
    Open(ctx context.Context, opts terminal.OpenOptions) (terminal.ToolEnvelope, error)
    Read(sessionID string, opts terminal.ReadOptions) (terminal.ToolEnvelope, error)
    Write(sessionID, input string, appendNewline bool, wait time.Duration, maxBytes int) (terminal.ToolEnvelope, error)
    Kill(sessionID, signal string) (terminal.ToolEnvelope, error)
    List(includeExited bool) ([]terminal.SessionMeta, error)
    SessionMeta(sessionID string) (terminal.SessionMeta, error)
}
```

Change the `Manager` field type on `TerminalOpenTool`, `TerminalReadTool`, `TerminalWriteTool`,
`TerminalKillTool`, `TerminalListTool` (whatever the five structs are actually named — confirm
against the real file names) from `*terminal.Manager` to `TerminalBackend`. Do not change any
method bodies, any other field, or any other file in `nucleo-base`. `*terminal.Manager` must
continue to satisfy `TerminalBackend` with zero changes to `terminal.Manager` itself — if it
doesn't compile as-is, you've defined the interface wrong, fix the interface, not
`terminal.Manager`. Confirm whatever constructs these tool structs today (the TUI's wiring,
wherever that lives) still compiles unchanged.

Do not touch `agent/`, `runtime/`, `provider/`, `approval.go`, Layer 3, Layer 5, or anything else
in `nucleo-base`.

## Part B — `exo`: the new adapter package (`m8adapter`)

Create a new package `~/exo/m8adapter`. It implements `tool.TerminalBackend` (import
`nucleo-base`'s `tool` and `terminal` packages via the existing `replace`), backed by
`*sessions.Manager`.

### Core state per adapter-opened session

The adapter must track, per session it created (not sessions it didn't create — see "scope"
below):
- **agent lease snapshot**: captured once via `session.Lease()` right after
  `sessions.Manager.CreateWithOptions(..., InitialOwner: "agent")` returns. All subsequent writes
  and resizes on this session go through `session.WriteWithLease(snapshot, ...)` /
  `session.ResizeWithLease(snapshot, ...)` — **never call bare `Write`/`Resize`**. This is the
  round-1 fatal-bug fix: bare `Write` captures the *current* lease on every call, so it would
  silently survive a human takeover and keep writing as the agent. The snapshot must not be
  refreshed automatically — a stale snapshot after takeover is supposed to make writes fail with
  `ptyactor.ErrOwnershipLost`, that's the whole point.
- **read collector**: on `Open`, subscribe once via `session.Subscribe()` (unqualified — reading
  doesn't require holding write ownership) and accumulate every received chunk into a bounded byte
  buffer with a monotonic cursor (bytes appended so far). If the subscription channel closes (a
  takeover force-closed it), immediately resubscribe with the session's *current* lease (call
  `session.Lease()` fresh at resubscribe time, not the stored agent snapshot) and keep
  accumulating — read access must survive a takeover even though writes won't. Bound the buffer
  (e.g. `max(4 * expected max read size, 64 KiB)`); when older bytes are evicted, a `Read` request
  for a cursor position before the retained window returns the retained tail with `truncated:true`
  in the resulting envelope rather than erroring.
- **classification metadata**: `kind` (`shell_like` or `command`) and `approval_mode`
  (`"prompt_on_write"` or `"default"`), computed once at `Open` time from the requested command.

### Classifier — duplicated intentionally, not imported

Read `classifyCommand` in `~/nucleo-base/layer2-runtime-rails/terminal/manager.go` (currently
unexported) and reproduce its logic in `m8adapter` (a private function, e.g.
`classifyCommand(command string) sessionKind`). Add a comment at the duplication site naming the
exact source file/function you copied it from, and why it's duplicated rather than exported
(round 1 closed `nucleo-base`'s change surface to the six-method interface; exporting a helper for
this would widen that unnecessarily for ~17 lines of stable logic). Keep the shell-like command
list behaviorally identical to the source (`bash`, `zsh`, `sh`, `fish`, `python`, `python3`,
`node`, `irb`, `psql`, `mysql`, `sqlite3`, `rails console`/`rails c`, `python -i`/`python3 -i`, bare
`node` prefix when not invoking a `.js` file — verify the exact list against the real source, don't
trust this summary as exhaustive).

### Session scope for v1

`Open` always creates a brand-new `exo` session via `sessions.Manager.CreateWithOptions` — never
attaches to an existing one. `List`/`Read`/`Write`/`Kill`/`SessionMeta` only ever operate on
sessions this adapter instance created and is tracking internally (its own map from
`terminal`-tool-facing session ID to adapter session state) — if a session ID exists in
`sessions.Manager` but wasn't opened through this adapter, treat it as not-found from the adapter's
perspective. This is what makes the read-cursor design tractable without any new `ptyactor` API.

### `Open` implementation

```go
func (a *Adapter) Open(ctx context.Context, opts terminal.OpenOptions) (terminal.ToolEnvelope, error)
```
- Missing/empty `opts.Command` → `{ok:false, error:"command is required"}`, no session created.
- Otherwise call `sessions.Manager.CreateWithOptions(sessions.CreateOptions{Workdir: ..., Command:
  opts.Command, InitialOwner: "agent"})`. Map `OpenOptions`' other fields (workdir, name/whatever
  else it carries — check the real struct) onto `CreateOptions` explicitly; don't silently drop
  fields.
- On any creation failure (invalid workdir, session cap, PTY launch failure): `{ok:false,
  error:"<real underlying error>"}`.
- On success: capture the agent lease snapshot, start the read collector, classify the command,
  register the adapter session, and return a success envelope with `session_id`, `status`
  (`"running"`, or `"exited"`/`"failed"` if the command already finished by the time you check —
  this can legitimately happen for fast-exiting commands, it is not a launch failure), initial
  `cursor`, and any immediately-available `output`.

### `Read` implementation

```go
func (a *Adapter) Read(sessionID string, opts terminal.ReadOptions) (terminal.ToolEnvelope, error)
```
- Unknown session (not adapter-owned) → `{ok:false, session_id, error:"<not found>"}`.
- Serve from the collector's buffer starting at `opts`'s cursor field (check the real
  `ReadOptions` field name). If `opts.Wait > 0` and there's no new data yet, block (with a
  timer/context, not `time.Sleep` polling — use a condition variable or a channel-based wakeup
  fed by the collector) until either new data arrives or the wait deadline passes.
- Timeout with no new data → **success**, `{ok:true, output:"", truncated:false, cursor:<unchanged>}`
  — this must match `terminal.Manager`'s existing behavior exactly (read `manager.go`'s
  `Read`/timeout handling to confirm the exact envelope shape you must reproduce).
- Requested cursor before the retained buffer window → success, retained tail as `output`,
  `truncated:true`.

### `Write` implementation

```go
func (a *Adapter) Write(sessionID, input string, appendNewline bool, wait time.Duration, maxBytes int) (terminal.ToolEnvelope, error)
```
- Unknown session → not-found error envelope.
- Call `session.WriteWithLease(agentLeaseSnapshot, data)` where `data` is `input` plus `"\n"` if
  `appendNewline`.
- If that returns `ptyactor.ErrOwnershipLost`: `{ok:false, session_id, status:"running",
  session_kind, approval_mode, already_exited:false, error:"terminal ownership lost (session taken
  over by another client)"}` — this exact string, it's load-bearing for the agent's reasoning, per
  the design doc's error-mapping table.
- If the session is already exited/killed: `{ok:false, session_id, status:<exited|failed|killed>,
  session_kind, approval_mode, exit_code (if known), already_exited:true, error:"session is not
  running"}` — matches existing `terminal.Manager` shape, must not diverge.
- Other write I/O failure while nominally live: `{ok:false, session_id, status:"running" (or
  latest known), session_kind, approval_mode, error:"<real write error>"}`.
- `wait <= 0`: existing semantics — return immediately with no output, cursor at current tail.
- If `wait > 0` after a successful write: behave like `Read` with that wait budget (this matches
  `terminal.Manager.Write`'s existing "write then optionally wait for response" contract — confirm
  the exact shape against the real source).

### `Kill` implementation

```go
func (a *Adapter) Kill(sessionID, signal string) (terminal.ToolEnvelope, error)
```
- Already-exited session → idempotent success, `{ok:true, session_id, status:<...>,
  already_exited:true}`.
- Live adapter-owned session → call `sessions.Manager.Close(sessionID)` (or the more targeted
  signal-based kill if `sessions.Manager` exposes one — check; if it only exposes `Close`, that's
  fine for v1, note it explicitly in your report), mark local adapter status `"killed"`, return
  `{ok:true, session_id, status:"killed", session_kind, approval_mode, already_exited:true}`.
- Unknown session → not-found error envelope.

### `List` / `SessionMeta` implementation

Only list/describe adapter-owned sessions. Map fields per the design doc's table:
`ID`/`Name`/`Workdir` direct from `sessions.SessionInfo`; `Command` direct from
`sessions.SessionInfo.Command` (piece 1 already made this the logical command, not a shell path —
confirm this by reading piece 1's shipped code, don't assume); `Kind`/`ApprovalMode` from the
adapter's own classification; `Status` mapped from `SessionInfo.Status` plus the adapter's kill
overlay (`running`→`running`, `exited`→`exited`, killed-via-adapter or externally-closed→`killed`);
`StartedAt` from `SessionInfo.CreatedAt`; `LastOutputAt` from the collector's last-write timestamp;
`RunID`/`OwnerPID`/`PID`/`ExitedAt`/`ExitCode` omitted/zero-value per the design doc (explicitly
deferred, not a gap).

## Tests to write (in `exo`, package `m8adapter`, real `ptyactor.Session` + a fake `PTY`)

Match the design doc's 8-item list, scoped to this piece (items 7-8 about `termserver` SSE are
piece 3's tests, skip them here):

1. `TestOpenCapturesLeaseAndClassifierMetadata` — `Open` stores the agent lease snapshot and
   correct `kind`/`approval_mode` for a shell-like command vs. a plain command.
2. `TestWriteReturnsOwnershipLostAfterHumanTakeover` — open a session, call the underlying
   `ptyactor.Session.Takeover("human")` directly (simulating the browser), then call the adapter's
   `Write` — assert the exact ownership-lost envelope, including the exact error string.
3. `TestReadTimeoutReturnsSuccessWithEmptyOutput` — no new output within `wait`, assert success
   envelope, not an error.
4. `TestCollectorResubscribesAfterTakeoverAndCursorContinues` — after a takeover force-closes the
   subscription, drive more output through the fake PTY from the "human" side and assert a
   subsequent `Read` still sees it (proves the collector resubscribed rather than going silent).
5. `TestWriteAndKillAgainstExitedSessionPreserveAlreadyExitedSemantics` — close the underlying fake
   PTY (simulate command exit), then call `Write` and `Kill`, assert both return
   `already_exited:true` with the right status/error shapes.
6. `TestListOnlyExposesAdapterOwnedSessions` — create a session directly via
   `sessions.Manager.Create` (bypassing the adapter, simulating a browser-created session) and one
   via the adapter's `Open`; assert the adapter's `List` only returns the latter.
7. `TestSessionMetaFieldMapping` — assert every field in the mapping table above is populated (or
   correctly zero/omitted) as specified.
8. `TestReadCursorTruncationOnBufferEviction` — push enough output through the fake PTY to evict
   the collector's earliest bytes, then `Read` from a cursor before the retained window, assert
   `truncated:true` and the retained tail.

Run `go test -race -count=1 ./m8adapter/...` in `exo` and `go build ./...` / relevant `go test
-race -count=1 ./...` in both `nucleo-base` and `exo` to confirm nothing else regressed.

## What NOT to do

- Do not build the `termserver` SSE chat/approval routes or widen the approval callback — piece 3.
- Do not wire this adapter into any `main.go`/host binary construction yet — that's part of a
  later piece (or piece 3, if it makes sense to bundle) — this build prompt is the adapter package
  itself plus the `nucleo-base` interface swap, not the merged-binary wiring.
- Do not modify `ptyactor/session.go`, `sessions/manager.go`, or `realpty/realpty.go` — piece 1 is
  done and closed, build on top of it, don't change it (if you find you truly need to, stop and
  report why instead of changing it silently).
- Do not export the classifier from `nucleo-base` — duplicate it in `exo` per the design doc.

## When done

Report: files touched in each repo, the exact `TerminalBackend` interface as written, confirmation
`*terminal.Manager` still satisfies it unchanged, full `go test -race -count=1` output for both
repos, and explicitly flag anything where the real `OpenOptions`/`ReadOptions`/`SessionMeta`
field names or `terminal.Manager`'s actual error-envelope shapes differed from what this prompt
assumed — describe what you found and how you resolved the discrepancy.
