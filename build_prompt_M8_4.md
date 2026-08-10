You are building the fourth and final piece of M8, in `~/exo` (module
`github.com/DiegoAvila-yeyo/exo`), which already depends on `~/nucleo-base` (module
`github.com/yeyoos/nucleo-base`) via a local `replace`. Real build task — write actual Go code, run
the tests you write, `go test -race` clean.

## Context

Read `~/exo/M8_INTEGRATION_DESIGN.md` in full first — canonical, closed design (3 rounds of
Claude↔Codex critique). Pieces 1-3 are built and reviewed:
- Piece 1: `sessions.CreateWithOptions`/`realpty.WithCommand` (`~/exo/sessions`, `~/exo/realpty`).
- Piece 2: `tool.TerminalBackend` interface in `nucleo-base`
  (`~/nucleo-base/layer2-runtime-rails/tool/terminal_backend.go`) and `~/exo/m8adapter` (read
  `adapter.go` as shipped — `NewWithManager(manager sessionManager, ...)` and `New(manager
  *sessions.Manager, ...)` constructors).
- Piece 3: widened approval callback (`tool.SetGlobalApproveFunc(func(prompt, detail string, meta
  map[string]string) bool)`) and `termserver`'s new chat/approval routes (read `~/exo/termserver/
  chat.go` as shipped — `AgentRunner` type, `WithAgentRunner` option, `Server.RequestApproval`,
  `Server.ChatOutputWriter()`).

This piece wires everything together into `exo`'s existing host process — the same
`launchd`-activated backend that already runs the terminal infrastructure. Read
`~/exo/backend/backend.go` in full first — this is the file you're extending. It currently
constructs `sessions.Manager` and `termserver.New(config.Port, manager, opts...)`; you're adding
construction of the full `nucleo-base` agent stack and wiring it into both.

Also read, for the real constructor signatures you'll call (do not assume the summary below is
letter-perfect — confirm against the actual current source):
- `~/nucleo-base/layer2-runtime-rails/provider/*.go` — `NewAnthropicProvider`,
  `NewOpenAIProvider`, `NewLiteLLMProvider`, `NewFallbackProvider`, and how each currently reads
  its own config from environment variables directly (`ANTHROPIC_API_KEY` via the Anthropic SDK,
  `OPENAI_API_KEY`, `LITELLM_API_KEY`/`LITELLM_BASE_URL`) — there is no existing "build provider
  from env" helper, you're writing the first one.
- `~/nucleo-base/layer2-runtime-rails/agent/agent.go` — `agent.New(p provider.Provider, system
  string, tools *tool.Registry) *Agent`.
- `~/nucleo-base/layer2-runtime-rails/runtime/coordinator.go` — `runtime.NewCoordinator(a
  *agent.Agent, rootPath string) *Coordinator`, and `(c *Coordinator) Run(ctx, input)
  (TurnResult, error)` — this is what `AgentRunner` will call.
- `~/nucleo-base/layer2-runtime-rails/tool/registry.go` — `tool.NewRegistry()`, `var tool.Default`,
  `(r *Registry) Register(t Tool)`. Note: several tools (`bash`, `edit`, `writefile`, etc.)
  self-register into `tool.Default` via `init()`. The five terminal tools do **not** — confirmed by
  grepping for `init()` in `terminal_open.go`/`terminal_read.go`/`terminal_write.go`/
  `terminal_kill.go`/`terminal_list.go`, none exists. You must construct and register them
  explicitly with the `m8adapter.Adapter` as their `Manager` field.

## What to build

### 1. Provider construction from environment, fail-fast

New file, e.g. `~/exo/agenthost/provider.go` (new package `agenthost` in `exo`, or fold into
`backend` if that reads more naturally given `backend.go`'s existing structure — your call, note
which you picked and why). A function like:

```go
func buildProviderFromEnv() (provider.Provider, error)
```

Pick a simple, defensible selection order (there is no existing convention in `nucleo-base` to
match here, so make a pragmatic choice and state it clearly in your report): e.g. if
`ANTHROPIC_API_KEY` is set, use `NewAnthropicProvider`; else if `LITELLM_API_KEY` is set, use
`NewLiteLLMProvider`; else if `OPENAI_API_KEY` is set, use `NewOpenAIProvider`; else return a clear
error (`"no provider configured: set ANTHROPIC_API_KEY, LITELLM_API_KEY, or OPENAI_API_KEY"`) — do
not fall back to `provider.NewMockProvider` in production wiring, that's for tests only. Whatever
model/system-prompt parameters those constructors need, source sensible defaults or additional env
vars (document exactly which env vars you end up requiring).

**Fail-fast requirement (blocking, per the closed design)**: if provider construction fails,
`backend.Run` must return the error immediately, before doing anything else expensive (opening the
session store, starting the HTTP listener) — the process should not come up in a half-working
state. Log a clear, actionable message (not just the raw Go error) since this will be read from
`launchd`'s logs, not an interactive terminal.

### 2. Tool registry + `m8adapter` wiring

In the same construction path:
```go
adapter := m8adapter.New(manager) // manager is the *sessions.Manager backend.Run already builds
registry := tool.Default // or tool.NewRegistry() if you decide isolation is safer — state which and why
registry.Register(&tool.TerminalOpenTool{Manager: adapter})
registry.Register(&tool.TerminalReadTool{Manager: adapter})
registry.Register(&tool.TerminalWriteTool{Manager: adapter})
registry.Register(&tool.TerminalKillTool{Manager: adapter})
registry.Register(&tool.TerminalListTool{Manager: adapter})
```
(Confirm the exact five struct names against the real files — piece 2's report should have them,
but verify against source, not the report.) If you use `tool.Default`, be aware other tools
(`bash`, `edit`, etc.) already self-registered into it via `init()` — that's intended, the agent
should have those too, not just terminal tools.

### 3. Agent/runtime construction and `AgentRunner` wiring

```go
agent := agent.New(provider, systemPrompt, registry)
coordinator := runtime.NewCoordinator(agent, rootPath)
```
`rootPath` should be a sensible default (e.g. the user's home directory, or read from an env var —
your call, state it). Wrap `coordinator.Run` to match `termserver.AgentRunner`'s signature
(`func(ctx context.Context, input string) error` — discard or log `TurnResult` on success, return
the error on failure), and pass it to `termserver.New(...)` via the existing `WithAgentRunner`
option from piece 3.

### 4. Approval callback installation

```go
tool.SetGlobalApproveFunc(server.RequestApproval)
```
where `server` is the `*termserver.Server` `backend.Run` already constructs. Order matters: the
approval callback must be installed before the agent can possibly run a turn (i.e., before the HTTP
listener starts accepting `/api/chat` requests) — don't leave a window where a chat message could
trigger a `terminal_write` approval with no callback installed (falls back to auto-approve today,
which would be a real behavior regression from what the design intends).

### 5. Chat output wiring

Whatever mechanism the coordinator/agent uses to emit turn output (read `runtime/coordinator.go`
and `runtime/coordinator_render.go` to find it — likely a callback, writer, or similar hook point)
should be connected so that output ends up written to `server.ChatOutputWriter()`. If the
`Coordinator`'s current construction doesn't expose an obvious output hook, don't invent a large
new mechanism — find the smallest real seam (read the code, don't guess) and use it, then report
exactly what you found and used.

### 6. `backend.Run` integration

Extend `backend.Run` in `~/exo/backend/backend.go` (or a new file in that package) to call the
above construction after `sessions.Manager` is built and before `termserver.New` is called (since
`termserver.New` needs the `AgentRunner` via `WithAgentRunner`). Preserve every existing behavior
of `backend.Run` exactly — this is an additive change to an already-shipped, tested function; if
provider/agent construction fails, the existing lease/cleanup logic must still run correctly (study
the existing `cleanup()`/error-return pattern in the function and match it, don't bypass it).

## Tests to write

Full end-to-end wiring is inherently hard to unit test (it needs a real or mocked LLM provider) —
focus tests on the parts that don't require a real network call:

1. `TestBuildProviderFromEnvFailsFastWithClearErrorWhenUnconfigured` — with no relevant env vars
   set (use `t.Setenv` to clear them), assert a clear, specific error, not a panic or a silent
   fallback.
2. `TestBuildProviderFromEnvSelectsAnthropicWhenConfigured` (and similar for LiteLLM/OpenAI) —
   using `t.Setenv`, assert the right provider type is constructed (don't make a real API call).
3. `TestTerminalToolsRegisteredWithAdapterBackend` — construct the registry-wiring step in
   isolation and assert the five terminal tools' `Manager` field is the `m8adapter.Adapter`
   instance (not nil, not a `*terminal.Manager`).
4. `TestBackendRunFailsFastOnProviderConstructionError` — run `backend.Run` (or the relevant
   extracted construction function, if you factor it out for testability — likely cleaner) with a
   config that can't build a provider, assert it returns before touching the HTTP listener/session
   store in a half-initialized way, matching existing `backend_test.go`'s test style.

Do not write a test that requires a real LLM API call or a real end-to-end chat turn — that's
exactly the kind of thing that needs real manual browser verification instead (see below), not a
unit test with a fake API key.

Run `go test -race -count=1 ./...` in `~/exo` and confirm nothing regresses.

## Manual verification (required, this is the payoff milestone — do not skip)

Once this builds and tests pass, the full stack should be end-to-end runnable for the first time in
this whole multi-session effort. Per the M8 handoff brief's explicit requirement (carried from the
M0-M7 pattern, which caught 3 real product-breaking bugs exactly this way): actually run `exo serve`
(or the equivalent dev-mode entry point) with a real provider API key configured, open the
dashboard in a real browser, and:
1. Type a chat message that should cause the agent to open a terminal and run a real command.
2. Confirm the terminal panel shows the agent's session and its real output live.
3. Click "Take control" mid-turn and confirm the agent's next write visibly fails (surfaced in the
   chat stream) while the terminal keeps showing live output.
4. Trigger a shell-like `terminal_write` and confirm the approval prompt appears in the chat UI
   with the correct session correlation, and that approving/denying it does the right thing.

Report exactly what you observed for each of these 4 steps — if you cannot run a real browser
verification in your environment, say so explicitly and describe exactly what's untested, don't
claim success you didn't actually observe.

## What NOT to do

- Do not modify `m8adapter`, `sessions`, `realpty`, `ptyactor`, or `termserver`'s existing chat/SSE
  code from pieces 1-3 — this piece only wires them together plus new construction code.
- Do not fall back to a mock/no-op provider in the real `backend.Run` path — fail fast instead.
- Do not silently swallow provider or agent construction errors.

## When done

Report: files touched, the exact env vars required/read, the provider-selection order you chose and
why, the exact seam you used to route agent turn output into `ChatOutputWriter()`, full
`go test -race -count=1` output, and the manual verification results (or an explicit statement of
what you could not verify and why).
