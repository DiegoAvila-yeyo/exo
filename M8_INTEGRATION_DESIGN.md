# M8 Integration Design — exo ⇄ nucleo-base

Canonical, running source of truth for M8 (connecting nucleo-base's AI agent to exo's terminal
infrastructure). Consolidates resolved decisions round-by-round, same pattern as
`~/tesla/DASHBOARD_TERMINAL_DESIGN.md` for M0-M7. Individual round transcripts
(`M8_design_roundN_prompt.md` / `M8_design_roundN_response.md`) stay in this directory as history;
this file is what to read for "what's actually decided" without re-reading all of them.

Status: **BUILT, REVIEWED, AND MANUALLY VERIFIED END-TO-END IN A REAL BROWSER (2026-08-01).** M8 is
done — the original multi-session goal ("program from a web dashboard as if it were Claude
CLI/Desktop") is real and working. Design: rounds 1-3 closed (see History). Build: 5 pieces built
by Codex, each reviewed line-by-line and independently re-tested (`go test -race -count=1`, not
just trusting Codex's pasted output) before moving to the next:
1. `sessions.CreateWithOptions`/`realpty.WithCommand` + the `InitialOwner:"human"` bug fix.
2. `tool.TerminalBackend` interface + `m8adapter` (agent lease, read cursor, classifier).
3. SSE chat/approval routes in `termserver` + widened approval callback.
4. `agenthost` — the merged-binary wiring (provider from env, tool registry, agent/coordinator,
   `backend.Run` integration).
5. The browser chat UI itself (`termserver/assets`) — **missing from the original piece
   breakdown**, added after piece 4 revealed there was no way to actually type a chat message.

Two real bugs were caught only by actually reading the code and running the system, not by
Codex's own tests (same pattern as M6's three bugs in the original `exo` build):
- **Piece 4**: agent output is only reachable via raw `os.Stdout` (no injectable writer exists
  anywhere in `nucleo-base`'s `agent`/`runtime`), so the merged binary captures process-wide stdout
  during a turn. Real ANSI color codes in that output (`agent.go`'s `assistantColor`/`toolLogColor`)
  were not stripped before reaching the browser SSE stream — `termserver/chat.go`'s broadcaster was
  missing the `stripANSI` step that the original `dashboard/broadcaster.go` pattern it was supposed
  to mirror already had. Fixed in `build_prompt_M8_4_fix.md` — single point of scrub, matching the
  same principle as `ptyactor`'s secrets scrubber.
- **Missing chat UI**: pieces 3-4 built the complete backend chat contract but no build prompt ever
  asked for the actual `<input>`/message-log/approval-banner in the browser — an oversight in
  scoping, not a code bug, caught immediately when opening the browser for verification.
- **`EXO_AGENT_ROOT_PATH` was decorative, not a real sandbox** (found during a real end-to-end
  file-creation test against a real project, `~/Pacta-dashboard-AI`): `nucleo-base`'s `write_file`
  tool calls `os.WriteFile(in.Path, ...)` directly with no root anchoring, and `bash`'s `cmd.Dir`
  defaults to the process's OS-level cwd when the model omits `Workdir` — neither resolves against
  `coordinator.rootPath`. `agenthost.Host` never made the process cwd match the configured
  `rootPath`, so asking the agent to create `prueba/suma.js` (relative path) actually wrote it
  wherever `exo serve` happened to be launched from (`~/exo` in testing), not the configured
  project root. Confirmed by direct filesystem inspection — the chat log claimed success while the
  file was nowhere near the intended location. Not a `nucleo-base` bug (out of scope to fix there;
  the TUI has the same characteristic and relies on being launched from the right directory by a
  human). Fixed entirely in `exo`: `agenthost.New` now calls `os.Chdir(rootPath)` right after
  resolving it, fail-fast on error (`build_prompt_M8_6_rootpath_fix.md`). Re-verified after the fix:
  same test against `~/Pacta-dashboard-AI` correctly created `prueba/suma.js` in the right place on
  the first attempt (content-writing was cut short by an unrelated LiteLLM gateway quota/429 issue,
  not a location bug — the location was already correct before the gateway error).

## Post-verification hardening pieces (beyond the original 5)

- **Piece 6 — `rootPath` sandbox fix**: see above, `build_prompt_M8_6_rootpath_fix.md`.
- **Piece 7 — persistent agent config** (`build_prompt_M8_7_env_persistence.md`): before this,
  `agenthost` only read provider credentials/config from ambient process environment variables,
  fine for manual testing but useless for the real `launchd`-activated deployment — a reboot or
  `launchctl bootstrap` would start the backend with none of it configured. Added
  `appconfig.EnvFilePath()` → `~/Library/Application Support/exo/agent.env`, loaded by
  `appconfig.LoadEnvFile` as the very first thing in `backend.Run` (before `validateAgentHostEnv`,
  before `singleton.Acquire`/`sessionstore.New` — fail fast before creating any state if the file
  is present but unreadable). Deliberately **not** baked into the `LaunchAgent` plist (secrets
  don't belong in `~/Library/LaunchAgents/*.plist`) and deliberately does **not** override ambient
  env vars already set (so manual testing with exported shell vars still works exactly as before).
  `exo install` bootstraps a commented `0600` template on first install and never touches it again
  if it already exists. Reviewed and tests re-run — clean.
- **Piece 8 — `launchd` `PATH` fix** (`build_prompt_M8_8_launchd_path_fix.md`, found during real
  persistence verification below): `launchd`-spawned processes don't inherit the interactive
  shell's `PATH`, so the coordinator's `npm run build` preflight gate failed with `command not
  found: npm` once `exo` ran as an installed service (npm is nvm-managed, not on `launchd`'s
  default `PATH`). Fixed by capturing `os.Getenv("PATH")` at `exo install` time (when the CLI runs
  interactively, with the user's real `PATH`) and baking it into the plist's new
  `EnvironmentVariables` block (`launchagent.Config.EnvironmentVariables`, only emitted when
  non-empty). Documented, deliberate limitation: captured once at install time, doesn't auto-update
  if the user's `PATH` changes later (e.g. an `nvm` version switch) — re-run `exo install` when
  that happens. Reviewed and tests re-run — clean.
- **Piece 9 — MCP server support** (`build_prompt_M8_9_mcp.md`): `nucleo-base` already had a
  complete, working MCP client package (`layer2-runtime-rails/mcp` — `LoadConfig`/`Register`,
  connects stdio or HTTP MCP servers and registers their tools into a `tool.Registry`, with
  per-server failures already logged-and-skipped rather than fatal) that `agenthost` simply never
  called. Wired in: new `appconfig.MCPConfigPath()` → `~/Library/Application Support/exo/mcp.json`
  (optional — a missing file is not an error, by `mcp.LoadConfig`'s own existing design; malformed
  JSON *is* a fail-fast error). `agenthost.New` gained a `context.Context` parameter (signature
  change, all call sites updated) to bound MCP server bring-up with a 30s total timeout so one
  hung server can't block backend startup — confirmed the common case (no `mcp.json`) returns
  near-instantly, the timeout only applies when there are servers to actually dial. `Host.Close()`
  added to close MCP clients cleanly, wired into `backend.Run`'s existing `cleanup()` closure
  (which was also hardened with a small `recordErr` helper so multiple cleanup steps' errors don't
  clobber each other). Reviewed and tests re-run — clean. **Not yet configured with any real MCP
  server** — `mcp.json` is user-authored, no server is connected by default.
- **Piece 10 — persistent agent memory** (`build_prompt_M8_10_memory.md`): `nucleo-base`'s
  `runtime.Coordinator` was **already internally calling into memory on every turn**
  (`prepareOrientation` reads the package-level `tool.LocalMemoryService` var) — it just silently
  no-op'd because nothing ever set it, and `memoryservice.Service.Enabled()` is nil-safe by design
  specifically to support that. Wired in: `appconfig.MemoryDBPath()` →
  `~/Library/Application Support/exo/memory.db`, opened via `localstore.OpenSQLite` (self-contained
  — creates its own parent dir and schema) in `agenthost.New`, wrapping it in
  `memoryservice.New(...)` and calling `tool.SetLocalMemoryService(...)` before any turn can run.
  **Deliberately best-effort, not fail-fast** — the one piece in M8 where that's the *correct*
  choice rather than a shortcut: a memory-DB-open failure means "no memory this session," not "the
  agent can't function," unlike piece 7's provider config or piece 9's malformed MCP JSON. On
  failure, logs via `log.Printf` and leaves `tool.LocalMemoryService` `nil`, exactly matching
  today's (memory-less) behavior. `Host.Close()` extended to close the store and reset the global.
  Test isolation for the shared global handled with a save/nil/restore `t.Cleanup` helper.

### Real MCP verification (2026-08-01)

Copied the user's existing `github`/`jira-pacta` MCP credentials (already used by Claude Code
itself, from `~/.claude.json`) into `agent.env` — `GITHUB_PERSONAL_ACCESS_TOKEN`, `JIRA_URL`,
`JIRA_USERNAME`, `JIRA_API_TOKEN`, `PYTHONWARNINGS` — since `mcp.Register`'s stdio dial
(`exec.Command` with no explicit `Env`) inherits the process environment, exactly the same
mechanism already built for provider keys. Used the *agent itself*, over the real chat API, to
author `~/Library/Application Support/exo/mcp.json` (no secrets in that file, just server/command
definitions — the point was to prove the write-a-config-file loop works end-to-end, not just to
get the file written). Restarted the service; MCP bring-up connected both real servers. Verified
by asking the agent to list its own tools matching `github_*`/`jira-pacta_*` — got back all 25
GitHub tools and 50+ Jira tools, matching Claude Code's own tool list exactly. Confirms piece 9
works with real, non-trivial subprocess-based MCP servers, not just the unit-test mocks.
  Reviewed and tests re-run — clean.

### Real persistence verification (2026-08-01) — installed as an actual `launchd` service, not simulated

Ran `exo install` for real (`~/bin/exo install`), producing a real plist at
`~/Library/LaunchAgents/com.diegoavila.exo.plist` and a real `0600` template at `~/Library/
Application Support/exo/agent.env`. Filled the template with real `LITELLM_API_KEY`/
`LITELLM_BASE_URL`/`EXO_AGENT_ROOT_PATH`. Triggered socket activation with a plain HTTP request (no
manually-exported env vars in the shell — `launchd` doesn't inherit the interactive shell's
environment anyway) — the backend came up healthy on the first real connection. Ran `exo restart`
(`launchctl bootout` + `bootstrap`, a real process kill/respawn, confirmed via a changed PID) and
verified the **new** process still had the correct provider config and root path with zero manual
intervention — proof the config genuinely persists across a real service restart, not just within
one long-lived process. After adding piece 8, reinstalled to regenerate the plist with `PATH`
baked in and confirmed **at the OS level** (`ps eww <pid> | grep PATH`) that the real
`launchd`-spawned process has the correct `PATH` including the `nvm`-managed Node install — the
most reliable verification available, independent of the LiteLLM gateway's flakiness (hit two
unrelated quota/rate-limit errors from the shared gateway during this round of testing — a
gateway-side infrastructure issue, not an M8/exo bug).

**Manual verification results** (real browser, real LiteLLM-gateway-backed agent, real bash/command
sessions, no mocks): sent 5 chat messages, each correctly caused the agent to open a real
`exo` terminal session and run the requested command with real output round-tripped back into the
chat log, ANSI-clean. Confirmed live terminal streaming (compared tick counters across
screenshots against wall-clock timestamps). Clicked "Take control" mid-turn on an agent-owned
session — UI correctly flipped to "You have control", and the agent's next `terminal_write`
against that session failed with the exact designed string (*"terminal ownership lost (session
taken over by another client)"*), visible in the chat log, while the terminal kept streaming live
output uninterrupted. Triggered a real `terminal_write` approval on a `bash` (shell-like) session —
banner showed the correct `session_id`/command/bytes sourced directly from the widened callback's
`meta` (not parsed), approved it, and the write + read-back completed correctly.

## Goal (unchanged, do not re-litigate)

Chat message typed in the browser → AI agent (already built in `nucleo-base`) drives a real,
secure, ownership-aware PTY terminal (already built in `exo`) → human can watch and take over at
any point. `exo` currently has zero AI in it; `nucleo-base`'s agent currently drives a much weaker
terminal backend of its own. M8 connects them without rewriting either's already-shipped core.

## Closed decisions (round 1)

### Process topology
One merged Go binary. `exo`'s existing `launchd`-activated backend process is the host. It
constructs, at startup, the full agent stack (`provider`, `tool` registry, `runtime.Coordinator`,
`agent.Agent`) from `nucleo-base` (via the existing `replace` in `go.mod`) alongside
`sessions.Manager`. No RPC, no second process.

### What gets touched in `nucleo-base` — and only this
A new interface, **six methods** (not five — `terminal_write.go` calls `SessionMeta` before
`Write`, this was missed in the first draft and caught in round 1):
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
The five `terminal_*` tool structs' field type changes from concrete `*terminal.Manager` to this
interface. `*terminal.Manager` already satisfies it structurally, so the TUI's construction path
needs zero changes. Nothing else in `nucleo-base` is touched: `provider/*`, `agent/agent.go`,
`runtime/*`, `approval.go`'s mechanism, Layer 3 (Flamen), Layer 5 all stay exactly as they are.

### What gets built new — entirely inside `exo`
1. **Terminal adapter** implementing the interface above, backed by `sessions.Manager` +
   `ptyactor.Session`. Must hold, per agent-opened session:
   - an **agent lease snapshot** captured at session-open time — writes/resizes go through
     `WriteWithLease(agentLease, ...)` / `ResizeWithLease(...)`, never bare `Write()`/`Resize()`.
     (**Round 1 fatal-bug catch**: `Session.Write()` captures the *current* lease on every call,
     not the caller's own — calling it bare would silently survive a human `Takeover()` and keep
     writing as the agent. This was in the original draft and is wrong.)
   - an **incremental read buffer with a monotonic cursor**, built by subscribing once at open and
     accumulating output. `Read(since, maxBytes, wait)` is served from this buffer. On takeover
     force-closing the subscription, the collector immediately resubscribes with the new lease and
     keeps buffering (read access ≠ write ownership, so this keeps working after a human takes
     over). No new cursor/replay API needed on `ptyactor` itself for v1.
   - kill/status overlay state for `terminal_kill` and status mapping (see mapping table below).
2. **Command-aware session creation path** — `sessions.Manager` today only opens an interactive
   shell (`Create(workdir, name)`); it has no way to launch a specific command, which
   `terminal_open` requires. Round 1 verdict: do **not** fake this by writing the command into an
   already-open shell (wrong lifetime/exit semantics). **Round 2 closed the exact shape**: additive,
   non-breaking change to already-shipped `exo` code:
   ```go
   type CreateOptions struct {
       Workdir      string
       Name         string
       Command      string // empty = today's interactive-shell path
       InitialOwner string // empty = today's default ("agent")
   }
   func (m *Manager) Create(workdir, name string) (SessionInfo, error) {
       return m.CreateWithOptions(CreateOptions{Workdir: workdir, Name: name})
   }
   func (m *Manager) CreateWithOptions(opts CreateOptions) (SessionInfo, error)
   ```
   Plus a matching additive `realpty` option, `WithCommand(command string) Option` — when set,
   launches `/bin/sh -lc <command>` in the PTY but records the **logical requested command
   string** (not `/bin/sh`) as `Terminal.command`, matching what `terminal.Manager.Open` already
   does in `nucleo-base`. `InitialOwner` plumbs straight into the already-shipped
   `ptyactor.WithInitialOwner` — no new `ptyactor` work needed, just wiring. `termserver`'s
   existing `sessionStore` interface (which only needs `Create(workdir, name)`) is untouched by
   this change — `Create` stays a thin wrapper, so M3/M6 surface is not disturbed.

   **Round 2 correction to round 1's field-mapping table**: `SessionInfo.Command` should store the
   *logical launched command* for command-backed sessions (e.g. `"npm run dev"`), not the shell
   path — round 1 assumed the adapter would track command separately to "paper over"
   `SessionInfo.Command`; that's no longer needed. Plain `Create()` (interactive shell) keeps
   storing the resolved shell path, since for that case the shell *is* the real command.
3. **New chat/approval routes in `termserver`**, SSE-based, separate from the per-terminal
   WebSocket:
   - `POST /api/chat`, `GET /api/chat/stream`, `POST /api/approve`
   - Terminal bytes stay on the existing session WebSocket only — chat/approval are turn-global,
     not scoped to one PTY session, so mixing them into the terminal WS was rejected.
   - Auth: `POST /api/chat` and `POST /api/approve` get `ValidOrigin` + `ValidateDoubleSubmit`
     (same as other mutating routes); `GET /api/chat/stream` gets `ValidReadOrigin`, no CSRF (same
     as `GET /api/sessions`). No new WS-subprotocol token needed — SSE stays in the existing
     HTTP read-path trust model.
   - Reuse the **pattern** of `nucleo-base/dashboard/chat.go` + `broadcaster.go` (single-flight via
     `TryLock`, same `AgentRunner` function type, same event shapes: `idle`/`busy`/`output`/
     `approval`/`done` + heartbeat) — do not import the old dashboard package as a dependency of
     the hardened server. Reimplemented locally in `termserver` so the hardened auth boundary
     stays self-contained.

### Ownership vs. approval — confirmed orthogonal, no shared state
Tool-call approval (`agent/approval.go` channel mechanism, reused verbatim) answers "can the agent
run this command." Terminal write ownership (`exo`'s `Lease{Owner,Epoch}`, now correctly enforced
via the adapter's held lease) answers "who's allowed to write to this PTY right now." They don't
interact; a lost lease just surfaces as a normal tool error in the agent's context.

### Latent bug fixed as part of this milestone (round 3)
**`sessions.Manager.Create` (browser session creation, shipped in M3) never sets an explicit
owner, so it inherits `ptyactor`'s `defaultOwner = "agent"`** (`ptyactor/session.go:18-23`).
`termserver` sends that owner in the initial `ready` message
(`termserver/server.go:294-306`), and the frontend gates write access and shows the lock
banner/"Take control" button off that value *before any write or takeover happens*
(`termserver/assets/app.js:298-305, 338-340, 421-431, 459-462`). Net effect already shipped today:
**a human opening a brand-new browser session immediately sees "Agent has control" / read-only
input, with no agent involved at all** — a real semantic inversion, not a cosmetic label issue.
Fixed as part of the same `CreateWithOptions` change M8 already needs (not a separate patch):
```go
func (m *Manager) Create(workdir, name string) (SessionInfo, error) {
    return m.CreateWithOptions(CreateOptions{Workdir: workdir, Name: name, InitialOwner: "human"})
}
```
Browser/`termserver` sessions → `InitialOwner:"human"`. M8 adapter's agent-opened sessions →
`InitialOwner:"agent"` (explicit, matching the existing default, but now stated rather than
implicit).

### Approval callback is widened (round 3 — supersedes round 2's prompt-parsing plan)
Round 2 proposed leaving `tool.RequestToolApproval(prompt, detail string) bool` unchanged and
having `termserver` best-effort parse `session_id` out of the `terminal_write` prompt string.
**Round 3 rejects that as the v1 answer** — round 1 kept multi-session-per-turn allowed, so a
failed parse means an approval banner with no reliable way for the human to know which terminal
it's about; that's a real ambiguity, not just a missing label, and cheaper to fix now than after
tools are built against the old shape. The callback becomes structured:
```go
var globalApprove func(prompt, detail string, meta map[string]string) bool
func SetGlobalApproveFunc(fn func(prompt, detail string, meta map[string]string) bool)
func RequestToolApproval(prompt, detail string, meta map[string]string) bool
```
`TerminalWriteTool` passes `meta: {"tool":"terminal_write","session_id":in.SessionID,"command":
meta.Command}`. Files touched: `nucleo-base/layer2-runtime-rails/tool/terminalapproval.go`,
`.../tool/terminal_write.go`, plus the merged-host wiring in `exo` that installs the callback —
this is a small, explicit widening of round 1's "only touch the terminal tools" scope, justified
by the multi-session ambiguity above. Approval SSE events in `/api/chat/stream` now carry a
**guaranteed** `session_id` for terminal-write approvals (not best-effort parsed):
```json
{"type":"approval","prompt":"...","detail":"...","session_id":"session-0007"}
```

### Session ID / lifecycle model (v1 scope decision)
- `terminal_open` **always creates a brand-new `exo` session** — no attach-to-an-existing
  (e.g. human-opened) session in v1. Rejected explicitly: attaching would leave read history,
  command metadata, and ownership baseline all undefined, exactly the kind of shared-control
  ambiguity the `tesla` rounds were built to eliminate.
- Same ID used at both layers: adapter session id = `sessions.SessionInfo.ID`. No translation
  table needed as a result of the no-attach rule.
- Agent-created sessions are listed in `GET /api/sessions` indistinguishably from human-created
  ones for v1 (a "created by agent" UI badge is a safe deferral).
- **`terminal_list`/`read`/`write`/`kill` only ever see adapter-opened (i.e. agent-opened)
  sessions** — this is what makes the cursor-buffer design in point 1 above tractable without
  touching `ptyactor`.

### Field mapping — `terminal.SessionMeta` ⇄ `exo`
| `SessionMeta` field | v1 source | Notes |
|---|---|---|
| `ID` | `sessions.SessionInfo.ID` | direct |
| `Name` | `sessions.SessionInfo.Name` | direct |
| `Command` | `sessions.SessionInfo.Command` | **round 2 correction**: `CreateWithOptions` now stores the logical command there directly, so this is a direct read, not an adapter workaround |
| `Workdir` | `sessions.SessionInfo.Workdir` | direct |
| `RunID` | omitted / zero | unused by tools today |
| `OwnerPID` | omitted / zero | not available from `sessions` |
| `PID` | omitted in v1 | optional future field on `SessionInfo`, not required for tool flow |
| `Kind` | adapter-classified from requested command | **round 2**: classifier logic (`classifyCommand`, `terminal/manager.go:491-507`) is duplicated inside the `exo` adapter, not exported from `nucleo-base` — it's unexported, ~17 lines, stable; exporting it would widen round 1's closed "only touch the 6-method interface" scope. Comment the duplication site with its source line reference so drift is visible. |
| `ApprovalMode` | adapter-synthesized from `Kind` | `"prompt_on_write"` for shell-like |
| `Status` | adapter overlay + `SessionInfo.Status` (`running`→`running`, `exited`→`exited`, closed-via-kill or closed-externally→`killed`) | |
| `StartedAt` | `SessionInfo.CreatedAt` | direct |
| `LastOutputAt` | adapter collector timestamp | synthesized |
| `ExitedAt` / `ExitCode` | best-effort / nil in v1 | safe omission, tools don't branch on these today |

### Crash recovery interaction (resolved, was based on a false premise)
M5's `sessionstore` reconciliation is cleanup-only — it kills leftover process groups and marks
stale metadata, it does **not** recreate live `ptyactor.Session`s. So after a backend crash/restart
there is no owner to negotiate and nothing to hand back: new sessions just start at
`InitialOwner="agent"` as normal. No new ownership-handshake logic needed anywhere.

### Provider credentials in the merged binary
Sourced from the `launchd` plist environment (same mechanism that already starts the process) —
no new config file format. Backend must fail fast at startup with a clear log message if required
provider config is missing; silent partial startup is explicitly rejected.

### Concurrency
Global single-flight agent-turn lock is kept exactly as `chat.go` has it today (`TryLock`, 409
`busy` on a second concurrent chat message). One turn may still open/drive multiple terminal
sessions — the lock is per-turn, not per-session.

### Failure/restart semantics
If the merged binary crashes or restarts mid-turn: turn is lost, no resume, no PTY reattach —
matches `tesla`'s already-closed v1 stance. Chat layer's job is just clean user-visible failure
(SSE disconnect before `done` = "turn interrupted"; pending approval is lost with the process),
not recovery logic.

## Closed decisions (round 2)

### `ToolEnvelope` error mapping — exact strings, not vague ones
The agent never sees Go error types, only marshalled `ToolEnvelope` JSON (`{"ok":false,"error":
"..."}` plus status fields) via `tool/terminal_common.go`. A vague error string is a real
agent-quality regression, not just an ugly log line, so the mapping is explicit:

| Case | `ok` | Key fields | `error` |
|---|---|---|---|
| `terminal_open`: missing command | false | — | `"command is required"` |
| `terminal_open`: workdir/cap/launch/store failure | false | — | real underlying error string |
| `terminal_open`: command exits during startup wait | **true** | `status:"exited"/"failed"`, `exit_code?` | — (not an error — PTY/session was created fine) |
| `terminal_read`: timeout, no new data | **true** | `output:""`, `truncated:false`, `cursor` unchanged | — (matches existing `terminal.Manager` behavior) |
| `terminal_read`: unknown session | false | `session_id` | real not-found error |
| `terminal_read`: buffer evicted bytes before `since_cursor` | **true** | `truncated:true`, retained-tail `output` | — |
| `terminal_write`: **`ptyactor.ErrOwnershipLost`** | false | `status:"running"`, `already_exited:false` | **`"terminal ownership lost (session taken over by another client)"`** — new, M8-specific, non-negotiable exact wording so the agent doesn't confuse it with "not running" and try the wrong remedy |
| `terminal_write`: exited/killed session | false | `status`, `exit_code`, `already_exited:true` | `"session is not running"` (existing shape, preserved) |
| `terminal_write`: PTY I/O failure while nominally live | false | latest known `status` | real write error |
| `terminal_write`: `wait <= 0` | **true** | immediate return, cursor at current tail | — (existing semantics preserved) |
| `terminal_kill`: already exited | **true** | `already_exited:true` | — (idempotent, existing shape) |
| `terminal_kill`: live session | **true** | `status:"killed"` | — |
| `terminal_kill`: unknown session | false | `session_id` | not-found error |

### SSE schema for `GET /api/chat/stream` — exact payloads
Reuses `dashboard/chat.go`'s wire pattern (`data: <json>\n\n`, `: heartbeat` every 10s) verbatim
for `idle`/`busy`/`output`/`done`. `approval` events carry a **guaranteed** `session_id` for
terminal-write approvals — see "Approval callback is widened (round 3)" above; the callback itself
is structured (`meta map[string]string`), not prompt-parsed:
```json
{"type":"idle"}
{"type":"busy"}
{"type":"output","text":"..."}
{"type":"approval","prompt":"...","detail":"...","session_id":"session-0007"}
{"type":"done"}
```
Auth: `POST /api/chat` and `POST /api/approve` get `ValidOrigin` + `ValidateDoubleSubmit`;
`GET /api/chat/stream` gets `ValidReadOrigin`, no CSRF — matches existing `termserver` route
conventions exactly (mutating vs. read-only).

### Test plan — 8-item minimum, mapped to packages
1. `exo/sessions` — `CreateWithOptions` launches command-backed sessions, records logical command.
2. `exo/m8adapter` (new package) — `Open` returns correct `kind`/`approval_mode`.
3. `exo/m8adapter` — stale agent lease after browser takeover → explicit ownership-lost envelope.
4. `exo/m8adapter` — read timeout with no new output → success, empty output, not an error.
5. `exo/m8adapter` — collector resubscribes after takeover, cursor semantics continue.
6. `exo/m8adapter` — write/kill against exited sessions preserve `already_exited` semantics.
7. `exo/termserver` — `/api/chat` preserves `409 busy`; `/api/chat/stream` emits the exact event
   shapes above plus heartbeat.
8. `exo/termserver` — approval events include parsed `session_id` where applicable; route auth
   matches `ValidOrigin`/`ValidReadOrigin`/CSRF expectations.

Test doubles: real `ptyactor.Session` + fake `PTY` for adapter tests (this is where
`WriteWithLease`/takeover semantics actually live and are already well-tested at the `ptyactor`
layer); fake runner/approval/adapter for `termserver` SSE tests — no real PTY needed there.

**Manual-only** (per the M6 lesson — three real bugs, zero caught by unit tests): cross-transport
takeover UX during an active agent turn — chat stream stays connected, terminal WS shows the lock
banner change, human takes over from another tab, agent's next write fails and is visible in the
chat stream as a tool error, approval UI still maps to the right session throughout.

## Blocking punch list before build prompts (final — rounds 1-3)

1. Adapter must hold agent lease snapshot, always use `WriteWithLease`/`ResizeWithLease` — never
   bare `Write`/`Resize`.
2. Add `CreateWithOptions`/`WithCommand` to `exo` exactly as specified above (additive,
   non-breaking) — **and fix `Create()` to pass `InitialOwner:"human"`** (round 3 latent-bug fix).
3. Adapter terminal namespace stays limited to adapter-opened sessions for v1.
4. Chat + approval go in `termserver` over SSE with the exact routes/auth/event-shapes above.
5. Provider config sourced from `launchd` environment, fail-fast on missing config.
6. Classifier duplicated (not exported) inside the `exo` adapter, commented with its source line.
7. `ToolEnvelope` error mapping implemented exactly as the table above, especially the
   ownership-lost string.
8. Widen `tool.RequestToolApproval`/`SetGlobalApproveFunc` to carry `meta map[string]string`;
   `TerminalWriteTool` passes `session_id`/`tool`/`command`; approval SSE events carry a
   guaranteed (not parsed) `session_id`.

## Safe deferrals (explicitly not v1 scope)

- "Created by agent" badge in the browser session list.
- Exporting `PID` / `ExitCode` on `sessions.SessionInfo`.
- Agent attaching to a pre-existing human-opened session.
- Any post-crash turn resume or PTY reattach.

## Open questions

None. Rounds 1-3 closed every structural and contract-shape question. `PID`/`ExitCode` on
`SessionInfo` are explicitly, permanently deferred (round 3 confirmed no row in the error-mapping
table needs them for correctness — diagnostic-only value, not a blocker). Ready for build prompts.

## History

- **Round 1** (2026-07-31): `M8_design_round1_prompt.md` → `M8_design_round1_response.md` +
  `diagrams_draft/{m8_component_diagram,m8_takeover_sequence}.svg`. Found and fixed 2 fundamental
  issues in the initial draft (bare `Write()` bypasses takeover; `terminal_open` needs command-aware
  creation `exo` doesn't have yet). Resolved all 10 identified gaps with concrete v1 answers.
- **Round 2** (2026-08-01): `M8_design_round2_prompt.md` → `M8_design_round2_response.md`. Zero new
  fundamental problems found. Closed the exact shapes left open by round 1: `CreateWithOptions`/
  `WithCommand` signatures (additive), corrected round 1's assumption about `SessionInfo.Command`
  (now stores the logical command directly, no adapter workaround needed), classifier duplication
  decision, full `ToolEnvelope` error-mapping table, exact SSE event JSON, and an 8-item test plan
  mapped to concrete packages. Codex's own verdict: buildable now, round 3 optional.
- **Round 3** (2026-08-01): `M8_design_round3_prompt.md` → `M8_design_round3_response.md`. Closed
  the 3 remaining optional questions with definitive (not hedged) answers: (1) approval callback
  widened to carry structured `meta`, guaranteed `session_id` on approval SSE events — prompt-
  parsing rejected as too fragile under multi-session turns; (2) **found and fixed a latent
  correctness bug in already-shipped M3/M6 code** — browser-created sessions inherited
  `ptyactor`'s `defaultOwner="agent"` with no override, so a human opening a new session saw
  "Agent has control"/read-only input with no agent involved; fixed by making `Create()` pass
  `InitialOwner:"human"` explicitly, bundled into the same `CreateWithOptions` change M8 already
  needed; (3) `PID`/`ExitCode` deferral reconfirmed, no scope change. Codex's own verdict: zero
  remaining open questions, ready for build prompts, no round 4 needed.
