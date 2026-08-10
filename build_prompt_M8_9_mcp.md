You are wiring MCP server support into `exo`'s merged agent binary (`~/exo`, module
`github.com/DiegoAvila-yeyo/exo`). Real build task, run tests, `go test -race` clean.

## Context

`nucleo-base` (module `github.com/yeyoos/nucleo-base`) already has a working MCP client package —
read `~/nucleo-base/layer2-runtime-rails/mcp/register.go` and `client.go` in full before writing
any code. Key facts already confirmed by reading the source (don't re-derive, just use them):
- `mcp.LoadConfig(path string) (*Config, error)` reads a JSON file shaped
  `{"servers": [{"name":"...","transport":"stdio"|"http","command":"...","args":[...],
  "url":"...","headers":{...}}]}`. **Missing file returns `(nil, nil)` — not an error, MCP is
  opt-in by design.**
- `mcp.Register(ctx, cfg, registry *tool.Registry, progress mcp.ProgressFunc) []*mcp.Client`
  connects every configured server, registers each remote tool into the given `tool.Registry` as
  `"<server>_<toolname>"`, and **already handles per-server failure gracefully** — a bad/unreachable
  server is logged to stderr and skipped, it does not fail the whole call or return an error. It
  returns the successfully-connected clients so the caller can `Close()` them later.
- `mcp.ProgressFunc(server string, status mcp.ProgressStatus, total int)` — optional, fires
  `ProgressBegin`/`ProgressConnecting`/`ProgressConnected`/`ProgressFailed`/`ProgressDone` per
  server. Pass `nil` if you don't need it, or a simple one that logs via `log.Printf` (this
  process's stdout is captured per-turn by `agenthost.redirectStdout` during agent turns — use
  `log.Printf`, which goes to stderr by default in Go, so MCP bring-up logging at startup doesn't
  get mixed into a chat turn's captured output).

This build prompt wires that existing package into `agenthost` and `backend`, the same way piece 4
wired the terminal tools and provider. Read `~/exo/agenthost/host.go` (current `New`,
`buildToolRegistry`) and `~/exo/backend/backend.go` (current `Run`, especially the `cleanup()`
closure and where `newAgentHost`/`host` are used) before writing code — you're extending both, not
replacing them.

## What to build

### 1. `appconfig.MCPConfigPath()`

In `~/exo/appconfig/config.go`, following the exact existing pattern of `EnvFilePath()`:
```go
func MCPConfigPath() (string, error) {
    dir, err := AppSupportDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(dir, "mcp.json"), nil
}
```
No template bootstrap needed (unlike `agent.env` in piece 7) — JSON has no comment syntax to make a
helpful annotated template, and `mcp.LoadConfig`'s missing-file-is-fine behavior means there's
nothing to bootstrap. Don't add one.

### 2. Wire `mcp.Register` into `agenthost.New`

In `~/exo/agenthost/host.go`:
- After `registry := buildToolRegistry(adapter)` (so MCP tools join the same registry as the
  built-in + terminal tools), resolve `appconfig.MCPConfigPath()`, call `mcp.LoadConfig(path)`. If
  it returns an error (malformed JSON — distinct from "file missing", which returns `nil, nil` per
  the source above), **fail fast** on `agenthost.New`, matching the existing fail-fast convention
  for broken persisted config (piece 7's `agent.env` fail-fast precedent).
- Call `mcp.Register(ctx, cfg, registry, progressFn)` with a bounded context — wrap
  `context.Background()` in a `context.WithTimeout` (e.g. 30 seconds total for MCP bring-up across
  all configured servers) so one hung server can't block the whole backend from starting
  indefinitely. `agenthost.New` doesn't currently take a `context.Context` parameter — add one
  (`func New(ctx context.Context, manager *sessions.Manager) (*Host, error)`), and update every
  call site (`backend.Run`, any tests) to pass one through. This is a small, deliberate signature
  change — state explicitly in your report that you made it and why.
- Store the returned `[]*mcp.Client` on the `Host` struct (new field, e.g. `mcpClients
  []*mcp.Client`).
- Add `func (h *Host) Close() error` that closes every stored MCP client (collect and return the
  first non-nil error, but attempt closing all of them regardless — don't stop at the first
  failure, matching the "one bad thing shouldn't cascade" principle already used in `mcp.Register`
  itself). If `Host` has no MCP clients (empty config or MCP not configured), `Close()` should be a
  safe no-op.

### 3. Wire `Host.Close()` into `backend.Run`'s shutdown path

In `~/exo/backend/backend.go`, the existing `cleanup()` closure already calls `idle.Close()`,
`httpServer.Shutdown(...)`, and `lease.Release()` in that order. Add `host.Close()` to this same
closure (pick the position that makes sense given the existing order — read the existing code and
match its style, e.g. probably after `idle.Close()` and before/alongside `httpServer.Shutdown`,
since MCP clients are a backend-level resource like the HTTP server). Update the `newAgentHost`
call site to pass a context (per the signature change above — a `context.Background()` is fine
here, this isn't the request-scoped context, it's process-lifetime).

## Tests to write

1. `appconfig`: `TestMCPConfigPathUnderAppSupportDir` — matches the existing style of whatever test
   covers `EnvFilePath`/`LockPath`, just asserting the path shape.
2. `agenthost`: `TestNewSucceedsWithNoMCPConfigFile` — no `mcp.json` present, `New` still succeeds
   (proves the opt-in behavior isn't accidentally made mandatory).
3. `agenthost`: `TestNewFailsFastOnMalformedMCPConfig` — write a syntactically invalid JSON file at
   the resolved `MCPConfigPath()` (use a temp `HOME`/app-support-dir override matching whatever
   seam the existing `agenthost` tests already use for isolating `appconfig.AppSupportDir()` in
   tests — check `host_test.go` for the pattern, e.g. does it override `$HOME` via `t.Setenv`?),
   assert `New` returns an error.
4. `agenthost`: `TestHostCloseClosesMCPClientsWithoutError` — construct a `Host` with no MCP
   servers configured, call `Close()`, assert no error (the no-op case; a full integration test
   with a real MCP server dial is out of scope/flaky for unit tests — don't attempt to spin up a
   real stdio/http MCP server in this test).

Run `go test -race -count=1 ./...` in `~/exo` and confirm nothing else regresses — pay particular
attention to anything that called `agenthost.New(manager)` before this change and now needs a
`ctx` argument.

## What NOT to do

- Do not touch `nucleo-base` — the `mcp` package is already complete and correct, this is pure
  wiring in `exo`.
- Do not add retry/reconnect logic for failed MCP servers beyond what `mcp.Register` already does
  — it already logs-and-skips per server, that's the intended v1 behavior.
- Do not build any UI for configuring MCP servers — that's a plain JSON file the user edits by
  hand, matching how `agent.env` works today.
- Do not create an `mcp.json` template file on `exo install` — explicitly out of scope, see above.

## When done

Report: exact files touched, the exact new `agenthost.New` signature and every call site you had
to update, where `Host.Close()` landed in `backend.Run`'s cleanup order and why, full
`go test -race -count=1` output, and confirm the 30-second (or whatever you chose) MCP bring-up
timeout doesn't block `backend.Run` from returning promptly when `mcp.json` is absent (the common
case) — this should be near-instant, not waiting out any timeout when there's nothing to connect
to.
