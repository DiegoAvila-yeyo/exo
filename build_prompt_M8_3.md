You are building the third piece of M8, spanning `~/nucleo-base` (module
`github.com/yeyoos/nucleo-base`) and `~/exo` (module `github.com/DiegoAvila-yeyo/exo`, depends on
`nucleo-base` via a local `replace`). Real build task — write actual Go code, run the tests you
write, `go test -race` clean in both repos.

## Context

Read `~/exo/M8_INTEGRATION_DESIGN.md` in full first — canonical, closed design (3 rounds of
Claude↔Codex critique). Pieces 1 and 2 are already built and reviewed:
- Piece 1: `sessions.CreateWithOptions`/`realpty.WithCommand` in `exo` (read
  `~/exo/sessions/manager.go`, `~/exo/realpty/realpty.go` as shipped).
- Piece 2: the `tool.TerminalBackend` interface swap in `nucleo-base` (read
  `~/nucleo-base/layer2-runtime-rails/tool/terminal_backend.go` and the five `terminal_*.go` tool
  files) and the new `~/exo/m8adapter` package (read `~/exo/m8adapter/adapter.go` as shipped).

This piece covers **only**: (A) widening the tool-approval callback in `nucleo-base` to carry
structured metadata, and (B) new SSE-based chat/approval HTTP routes in `exo/termserver`. It does
**not** cover wiring a full merged host binary (constructing the agent/runtime/provider stack,
installing the approval callback and `m8adapter` into a running process, loading `launchd`
environment config) — that is a separate, later piece. Where this piece needs something from that
future wiring (e.g. an `AgentRunner` function, an approval-callback installation point), define the
narrowest interface/type needed and let the real construction be supplied later; don't build a
`main.go` here.

Also read before writing code:
- `~/nucleo-base/layer2-runtime-rails/tool/terminalapproval.go` — the callback you're widening.
- `~/nucleo-base/layer2-runtime-rails/tool/terminal_write.go` — the only current caller of the
  approval callback, lines ~61-70 build the `prompt`/`detail` strings.
- `~/nucleo-base/layer1-harness-shell/dashboard/chat.go`, `server.go`, `broadcaster.go` — the
  **pattern** to copy for SSE event shapes and single-flight locking. Do not import this package
  from `termserver`; reimplement the pattern locally so `termserver`'s hardened auth boundary stays
  self-contained (this was explicit in the closed design — reusing the file, not just the pattern,
  was rejected).
- `~/exo/termserver/server.go` — existing routes, `ValidOrigin`/`ValidReadOrigin`/
  `ValidateDoubleSubmit`, how WS routes are registered, existing route table (`routes()` or
  equivalent) so you follow the same conventions for the new HTTP routes.

## Part A — `nucleo-base`: widen the approval callback

Change in `terminalapproval.go`:

```go
var globalApprove func(prompt, detail string, meta map[string]string) bool

func SetGlobalApproveFunc(fn func(prompt, detail string, meta map[string]string) bool)

func RequestToolApproval(prompt, detail string, meta map[string]string) bool
```

(Match the real current names/signatures exactly — read the file first, don't assume the above is
letter-perfect if the real code differs in a small way; the shape is what matters.) If
`globalApprove` is nil, preserve existing behavior (read what "nil callback" currently does —
likely auto-approve — and keep that).

In `terminal_write.go`, update the call site to pass structured metadata:
```go
RequestToolApproval(prompt, detail, map[string]string{
    "tool":       "terminal_write",
    "session_id": in.SessionID,
    "command":    meta.Command,
})
```
(Use the real local variable names at that call site — read the current code around line ~61-70
to get `meta.Command` or whatever the actual field/variable is called.)

This is the only `nucleo-base` change in this piece. Do not touch `agent/`, `runtime/`,
`provider/`, `approval.go` (the *turn-level* approval mechanism — separate from this
tool-call-approval callback, do not confuse the two), Layer 3, Layer 5, or any other tool file.

## Part B — `exo/termserver`: new chat/approval SSE routes

Add three routes to `termserver`, following the existing route-registration and auth conventions
in `server.go`:

### `POST /api/chat`
- Auth: `ValidOrigin` + `ValidateDoubleSubmit` (same as other mutating routes).
- Body: `{"message": "..."}`.
- Single-flight: a `sync.Mutex` with `TryLock()` — if already locked, respond `409 {"error":
  "busy"}` immediately (matches `dashboard/chat.go`'s existing behavior, which you're reproducing
  the *pattern* of, not importing).
- On success: run the turn in a goroutine (the actual agent-invocation function is a dependency
  injected into `termserver` at construction time — define a narrow type for this now, e.g.
  `type AgentRunner func(ctx context.Context, input string) error`, matching
  `dashboard/chat.go`'s existing `AgentRunner` type exactly so a future host-wiring piece can plug
  either dashboard's or termserver's runner in without translation). `termserver` doesn't need to
  know what's inside the runner — that's out of scope for this piece.
- Release the single-flight lock when the turn completes (success or error), and notify SSE
  subscribers.

### `GET /api/chat/stream`
- Auth: `ValidReadOrigin`, no CSRF (matches `GET /api/sessions`'s existing auth level).
- SSE (`Content-Type: text/event-stream`), one event per `data: <json>\n\n` line, plus a `:
  heartbeat\n\n` comment line every 10 seconds (match `dashboard/chat.go`'s heartbeat interval).
- Event shapes — exact, from the closed design doc:
  ```json
  {"type":"idle"}
  {"type":"busy"}
  {"type":"output","text":"..."}
  {"type":"approval","prompt":"...","detail":"...","session_id":"session-0007"}
  {"type":"done"}
  ```
  `idle`/`busy` sent once on connect reflecting current single-flight lock state. `output` events
  come from wherever the turn's output is broadcast (define a small broadcaster type local to
  `termserver`, following `dashboard/broadcaster.go`'s `io.Writer`-based fan-out pattern —
  reimplemented, not imported). `approval` events **must** carry `session_id` populated directly
  from the widened callback's `meta["session_id"]` (Part A) — this is a guaranteed field now, not
  best-effort parsed from a prompt string, per the closed design's round 3 decision. `done` fires
  when a turn completes.

### `POST /api/approve`
- Auth: `ValidOrigin` + `ValidateDoubleSubmit`.
- Body: `{"approved": true/false}`.
- Resolves whatever pending-approval state is currently waiting (a channel-based mechanism,
  matching `dashboard/chat.go`'s `SetPendingApproval`/`respond` pattern — reimplemented locally).
  `409 {"error":"no pending approval"}` if nothing is pending (matches existing behavior).

### Wiring surface `termserver` exposes for later host construction

Define whatever constructor parameters/interfaces the above needs (an `AgentRunner`, a way to
install/receive the approval callback with structured `meta`, from Part A) as explicit, narrow
additions to `termserver`'s existing `New`/config struct — don't hardcode a specific agent
construction inside `termserver` itself. A later piece will supply the real implementations.

## Tests to write

In `exo/termserver` (extend the existing test file or add a new one, following the existing test
style in `termserver/server_test.go`):

1. `TestChatPostReturns409WhenTurnBusy` — with a fake `AgentRunner` that blocks until released,
   a second concurrent `POST /api/chat` gets `409 busy`.
2. `TestChatStreamEmitsIdleBusyOutputApprovalDoneWithExactSchema` — drive a fake turn through a
   fake runner/broadcaster/approval source and assert every event's exact JSON shape, including
   `approval` always carrying `session_id` when the fake approval source provides one.
3. `TestApprovalRoutesResolvePendingApproval` — `POST /api/approve` with `{"approved":true}` and
   `{"approved":false}` both correctly resolve a pending approval; `409` when nothing pending.
4. `TestChatRoutesEnforceOriginAndCSRF` — `POST /api/chat` and `POST /api/approve` reject bad
   Origin / missing CSRF token the same way existing mutating routes do; `GET /api/chat/stream`
   uses the read-only Origin check (accepts a same-origin GET with no `Origin` header, per the
   existing `ValidReadOrigin` behavior `termserver` already relies on — read `security.go` to
   confirm the exact existing semantics you must match).

In `nucleo-base/layer2-runtime-rails/tool`:

5. A test confirming `RequestToolApproval`'s `meta` argument reaches the installed callback intact,
   and that `terminal_write.go` populates `session_id`/`tool`/`command` correctly in the call it
   makes — extend or add to the existing test file(s) for that package, following current style.

Run `go test -race -count=1 ./...` in both `~/exo` and `~/nucleo-base` (or the relevant packages in
`nucleo-base` if running the full suite is slow) and confirm nothing regresses, including
`m8adapter` and the five terminal tools from piece 2.

## What NOT to do

- Do not build a `main.go`/host-process wiring — that's a later piece.
- Do not touch `agent/`, `runtime/`, `provider/`, `agent/approval.go` (the turn-level mechanism),
  Layer 3, Layer 5, or `m8adapter`/`sessions`/`realpty`/`ptyactor` — pieces 1-2 are closed.
- Do not import `nucleo-base/layer1-harness-shell/dashboard` into `termserver` — copy the pattern,
  not the file.
- Do not make `approval` events' `session_id` optional/best-effort — it must be guaranteed now that
  the callback carries structured `meta` (round 3 explicitly rejected prompt-parsing).

## When done

Report: exact files touched in both repos, the exact new route table added to `termserver`, the
exact widened callback signature as written, full `go test -race -count=1` output for both repos,
and explicitly confirm the `approval` SSE event's `session_id` is sourced directly from the
callback's `meta` map (not parsed from any string) with a pointer to where in your code that
happens.
