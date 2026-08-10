# M8 Round 1: design critique and resolved v1

Source-of-truth note: the AGENTS instruction points to `/Users/eltitoyeyo/YEYO/YEYO_ESTADO.md`, but that file is not present on disk from this workspace. I proceeded from the code and closed design docs listed in the prompt.

## Executive verdict

Your 4-point draft is close on topology, but it misses one fatal ownership bug and understates how much contract-shaping the adapter must do.

### Fundamental issue 1: `Session.Write()` would bypass human takeover

The draft says the adapter can call `ptyactor.Session.Write()` and rely on `ErrOwnershipLost` after browser takeover. That is false against the shipped code in [ptyactor/session.go](/Users/eltitoyeyo/exo/ptyactor/session.go): `Write()` captures the **current** lease internally, and stale detection checks **epoch only**, not owner string. After `Takeover("human")`, `session.Write()` would capture the new human epoch and still succeed.

For M8 v1, the adapter must hold an **agent lease snapshot per session** and always call:

- `WriteWithLease(agentLease, ...)`
- `ResizeWithLease(agentLease, ...)`

When the human takes over, the agent lease becomes stale and the next write/resize fails correctly with `ErrOwnershipLost`.

### Fundamental issue 2: adapter-only is not enough for `terminal_open`

`terminal_open` takes a real `command`. `sessions.Manager.Create(workdir, name)` only opens an interactive shell and records the shell path in `SessionInfo.Command` ([sessions/manager.go](/Users/eltitoyeyo/exo/sessions/manager.go), [realpty/realpty.go](/Users/eltitoyeyo/exo/realpty/realpty.go)). If you do not add some `exo` API for command-backed session creation, the adapter either:

- lies about what was launched, or
- writes the command into an already-open shell, which changes lifetime and exit semantics.

That is the main place where the current draft is too optimistic.

## Resolved v1 design

Keep the merged binary. Keep the terminal-tool interface rebind. Keep chat in `termserver`, not in `dashboard/chat.go`.

But for a correct v1, change the design in these ways:

1. `nucleo-base` terminal tools switch from concrete `*terminal.Manager` to a small interface that includes **six** methods, not five:
   - `Open`
   - `Read`
   - `Write`
   - `Kill`
   - `List`
   - `SessionMeta`

2. `exo` gets a terminal adapter that owns per-session agent state:
   - `agentLease`
   - incremental read buffer and cursor
   - requested command/kind/approval metadata
   - local status overlay for kill/ownership-loss cases

3. `exo` also needs a **new command-aware session creation path**. Safest shape:
   - `sessions.Manager.CreateWithOptions(CreateOptions{Workdir, Name, Command, InitialOwner})`
   - or equivalent lower-level `realpty.NewCommand(...)`

4. `terminal_read` cursor semantics stay in the adapter, not in `ptyactor`.
   - The adapter subscribes once at session open, accumulates bytes, and serves cursor-based reads itself.
   - No new `ptyactor` read API is required for v1 if the adapter only supports agent-opened sessions.

5. The adapter should expose **only agent-opened sessions** through `terminal_list/read/write/kill`.
   - Browser `/api/sessions` still lists all `exo` sessions.
   - Agent tools do not attach to arbitrary pre-existing human sessions in v1.

That last point is important because it collapses a lot of ambiguity around cursors, missing historical output, ownership on pre-existing sessions, and metadata gaps.

## Gap-by-gap resolutions

### 1. Read semantics mismatch

#### Resolution

Do **not** add a new `ptyactor` cursor API for M8 v1.

Use an adapter-owned collector per agent-opened session:

- On `Open`, adapter subscribes to the backing `ptyactor.Session`.
- Collector appends every output chunk into a bounded byte buffer.
- Adapter tracks a monotonic `cursor int64` measured in bytes appended.
- `Read(since, maxBytes, wait)` is served from that buffer plus a condition variable / channel wakeup.

When a takeover force-closes the collector subscription, the collector immediately resubscribes with `session.Lease()` and continues buffering. Read access is observational; it does not imply write ownership.

#### Why this is the simplest correct v1

- `ptyactor` already replays only a ring snapshot, not a true cursor stream.
- `terminal_read` needs incremental semantics tied to an agent session, not to all websocket viewers.
- Keeping the cursor in the adapter avoids changing already-shipped `ptyactor` behavior.

#### Buffer lifetime and bounds

Per adapter session:

- Keep a bounded byte history, for example `max(terminal.maxReadBytes * 4, 64 KiB)`.
- Keep `baseCursor` for the first retained byte.
- If old bytes are evicted, a later `Read(since)` below `baseCursor` returns the retained tail and `truncated=true`.

Lifetime:

- Created on `terminal_open`
- Destroyed on `terminal_kill`
- Destroyed when the backing session exits and the adapter session is pruned

#### Blocking or deferrable?

Blocking for M8. Without this, `terminal_read` is not faithful.

#### Dependencies

Depends on gap 2: this works cleanly only if the adapter owns session open and session lifetime.

### 2. Session lifecycle / ID mapping

#### Resolution

For v1, `terminal_open` **always creates a brand-new `exo` session**. No attach-to-existing-session path.

Use the same string ID for both layers:

- adapter terminal session id = `sessions.SessionInfo.ID`

No translation table is needed if agent tools only see sessions the adapter created.

#### Why no attach in v1

Attaching the agent to an already-open human session creates four unresolved problems at once:

- no full read history, only last ring snapshot
- unknown command metadata
- unknown intended ownership baseline
- ambiguous UI expectations when the agent starts driving a human-owned shell

That is exactly the kind of undefined shared-control state the `tesla` rounds rejected.

#### Field mapping for `OpenOptions`

Current mismatch:

| `terminal.OpenOptions` | `sessions.Manager.Create` today | v1 answer |
|---|---|---|
| `Command` | no slot | add command-aware create path in `exo` |
| `Workdir` | yes | map directly |
| `Name` | yes | map directly |
| `StartupWait` | no slot | adapter-only wait after create |

#### Command creation decision

This is a real blocking design decision. The clean v1 answer is:

- add a command-aware create path in `exo`
- default owner for agent-opened sessions is `"agent"`
- browser can still take over later through existing `Takeover("human")`

Trying to fake this by opening a shell and writing the command into it is not a faithful replacement for `terminal_open`.

#### Browser listing behavior

Agent-created sessions appear in `GET /api/sessions` like any other `exo` session. UI labeling such as "created by agent" is safe to defer.

### 3. `SessionMeta` / `ToolEnvelope` field mapping

#### Resolution

Map what can be mapped directly, synthesize what the adapter already knows, and explicitly omit what `exo` does not know today.

#### Mapping table

| `terminal.SessionMeta` field | Source in M8 v1 | Notes |
|---|---|---|
| `ID` | `sessions.SessionInfo.ID` | direct |
| `Name` | `sessions.SessionInfo.Name` | direct |
| `Command` | adapter open spec | **not** `SessionInfo.Command` if `exo` keeps storing shell path |
| `Workdir` | `sessions.SessionInfo.Workdir` | direct |
| `RunID` | omit / zero value | not needed by tools today |
| `OwnerPID` | omit / zero value | not available from `sessions` |
| `PID` | optional new `SessionInfo.PID`; otherwise omit | useful, not required for tool flow |
| `Kind` | adapter-classified from requested command | same classifier as old terminal manager |
| `ApprovalMode` | adapter-synthesized from kind | `"prompt_on_write"` for shell-like |
| `Status` | adapter overlay + `SessionInfo.Status` | see below |
| `StartedAt` | `SessionInfo.CreatedAt` | direct |
| `LastOutputAt` | adapter collector timestamp | synthesized |
| `ExitedAt` | adapter-detected close time if seen, else nil | synthesized best-effort |
| `ExitCode` | nil in v1 unless `exo` is extended | safe omission |

#### Status mapping

| `sessions.SessionInfo.Status` | adapter `terminal.SessionStatus` |
|---|---|
| `running` | `running` |
| `exited` | `exited` |
| `closed` after `terminal_kill` | `killed` |
| `closed` from unknown external close | `killed` in v1 |

#### Does this require shipped `exo` API changes?

For correctness of tool flow: no.

For fidelity and observability: likely yes, minimally:

- add `PID` to `sessions.SessionInfo`
- optionally add `ExitCode`

Those are quality improvements, not blockers, because the current tool code does not make control decisions based on PID or exit code.

#### Blocking or deferrable?

The mapping itself is blocking. Adding PID/ExitCode is safe to defer.

### 4. Startup/reconnect ownership handshake

#### Resolution

This gap is mostly based on a false premise.

M5 sessionstore recovery in `exo` does **not** recreate live PTY sessions. It only reconciles stale metadata and kills leftover process groups ([sessionstore/reconcile.go](/Users/eltitoyeyo/exo/sessionstore/reconcile.go)). After a crash:

- there is no recovered `ptyactor.Session`
- there is no preserved owner to negotiate
- the new backend starts with an empty live session map

So M8 v1 needs no ownership reset logic on recovery.

For any newly opened agent terminal:

- initial owner is `"agent"`
- if the human takes over, adapter-held agent lease goes stale
- next agent write/resize fails with `ErrOwnershipLost`

That is enough.

#### Blocking or deferrable?

Resolved now. No new work needed beyond documenting that crash recovery is cleanup-only, not session restore.

### 5. Provider credentials/config in the merged binary

#### Resolution

The merged backend must load the same provider env/config that the current TUI process expects, from the **host process environment** given by `launchd`.

Concretely for v1:

- `launchd` plist env vars become the source of truth for provider keys/model settings
- the merged `exo` backend constructs provider/runtime/agent from those env vars at startup
- no new config file format for M8

Why:

- `launchd` already owns process startup
- introducing a second config source is a deployment footgun
- provider config is a hard blocker; keeping it in env avoids a new migration

#### Required explicit rule

M8 should fail fast on startup if required provider config is missing, with a clear backend log message. Silent partial startup would be worse than a boot failure.

#### Blocking or deferrable?

Blocking. This is a real deployment dependency.

### 6. Chat/approval event delivery transport in `termserver`

#### Resolution

Use **SSE for chat and approval events**, separate from the existing per-terminal websocket.

Add to `termserver`:

- `POST /api/chat`
- `GET /api/chat/stream`
- `POST /api/approve`

Keep terminal bytes on the existing session websocket only.

#### Why SSE, not terminal WS multiplexing

- chat/approval are not scoped to one PTY session
- current agent runner already maps naturally to a broadcast text/event stream
- approval is a turn-global event, not terminal-frame data
- mixing chat with PTY frames would recreate the kind of ownership ambiguity M1-M7 avoided

#### Auth model

Apply the existing hardened `termserver` rules:

- `POST /api/chat`: `ValidOrigin` + `ValidateDoubleSubmit`
- `POST /api/approve`: `ValidOrigin` + `ValidateDoubleSubmit`
- `GET /api/chat/stream`: `ValidReadOrigin`, no CSRF, same as `GET /api/sessions`

No websocket subprotocol token is needed for SSE because this stays in the HTTP read-path model already used by `GET /api/sessions`. It does not widen trust relative to current read endpoints.

#### Reuse choice

Copy the **pattern**, not the file, from `dashboard/broadcaster.go` and `dashboard/chat.go`:

- keep it in `exo/termserver` so the hardened auth boundary remains local
- do not import the old dashboard transport package as a dependency of the hardened server

#### Blocking or deferrable?

Blocking. Transport choice affects auth, UI, and failure semantics.

### 7. Single-flight vs multi-session concurrency

#### Resolution

Keep the existing global single-flight agent turn lock for M8 v1.

Meaning:

- one agent turn at a time, globally
- within that turn, the agent may open and drive multiple terminal sessions
- second chat message during an active turn still returns `409 busy`

#### Why keep it

- matches existing verified behavior
- avoids re-architecting approval routing
- avoids overlapping agent turns contending for the same terminal adapter state

This is not in conflict with multi-session terminal usage. One turn can orchestrate many sessions; the lock is about **agent turns**, not about PTY count.

#### Blocking or deferrable?

Resolved now. Safe to keep.

### 8. Failure/restart interaction

#### Resolution

Accept the v1 rule:

- if the merged binary crashes or is restarted, the in-flight agent turn is lost
- no turn resume
- no PTY reattach

This matches the already-closed `tesla` v1 stance and the current M5 cleanup-only recovery model.

#### What the chat layer must do

It does not need resume logic, but it **does** need clean user-visible failure semantics:

- SSE stream disconnect before `done` means "backend/turn interrupted"
- browser reconnects and sees idle once backend is back
- pending approval state is lost with the process

That should be documented explicitly in the UI copy and in M8 notes.

#### Blocking or deferrable?

Resolved now. Resume is explicitly deferred beyond v1.

### 9. TUI backward compatibility

#### Resolution

Yes, the interface change can remain TUI-compatible, but only if the new interface includes every method the tools actually use.

Required interface method set:

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

Why six methods: `terminal_write.go` calls `SessionMeta(...)` before it calls `Write(...)`.

`*terminal.Manager` already satisfies this structurally today, so the TUI construction path does not need logic changes.

#### Blocking or deferrable?

Blocking to get the interface right. TUI-side code changes are otherwise avoidable.

### 10. Test strategy

#### Minimum deterministic unit/integration set

Use real `ptyactor.Session` with fake PTY or fake session wrapper. No live browser needed.

1. Adapter open/read cursor progression
   - open session
   - emit output chunks
   - `Read(0)` returns initial output and cursor
   - `Read(prevCursor)` returns only appended bytes

2. Adapter read wait behavior
   - `Read(wait=...)` blocks until new output or timeout
   - deterministic wakeup via fake PTY emit

3. Ownership-loss on next agent write
   - adapter opens session and stores agent lease
   - external `Takeover("human")`
   - next `Write` returns tool error mapped from `ErrOwnershipLost`

4. Collector survives takeover for read continuity
   - after takeover closes stale subscription, collector resubscribes
   - subsequent human-side PTY output is still observable via `terminal_read`

5. Kill/status mapping
   - `terminal_kill` marks local status `killed`
   - subsequent read/write report non-running / already-exited semantics

6. List only exposes adapter-owned sessions
   - sessions existing in `sessions.Manager` but not opened through adapter are omitted from adapter `List`

7. SessionMeta synthesis
   - `Command`, `Kind`, `ApprovalMode`, `StartedAt`, `LastOutputAt` map as designed
   - omitted fields stay zero/nil without breaking JSON rendering

8. SSE transport tests in `termserver`
   - chat stream gets `busy`, `idle`, `approval`, `done`
   - POST routes enforce origin + CSRF

#### One thing only manual browser verification will catch

The cross-transport takeover UX:

- agent turn running
- browser terminal websocket receives lease change and lock banner state
- chat SSE remains connected
- human clicks `Take control`
- agent’s next terminal write fails
- browser sees terminal stay human-owned while chat stream shows the agent reacting to the tool error

That is exactly the kind of real timing/UI integration bug M6 exposed and unit tests will not reliably cover.

## Dependencies between gaps

- Gap 2 constrains gap 1: adapter-owned cursor buffering is clean only if M8 v1 restricts agent tools to agent-opened sessions.
- Gap 2 also constrains gap 3: command metadata is only trustworthy if `terminal_open` creates the session.
- Gap 6 depends on gap 7: approval routing stays simple only if turns remain single-flight.
- Gap 8 depends on gap 4: because crash recovery is cleanup-only, no ownership handshake is needed after restart.
- Gap 9 depends on gap 3: the new interface must include `SessionMeta`, not just the five obvious methods.

## Concrete v1 punch list

### Blocking decisions

1. Fix the ownership model in the adapter: hold agent lease, never call bare `Session.Write()`.
2. Add command-aware session creation in `exo`; do not fake `terminal_open` by stuffing commands into a pre-opened shell.
3. Keep adapter terminal namespace limited to adapter-opened sessions for M8 v1.
4. Put chat + approval in `termserver` over SSE with existing origin/CSRF rules.
5. Load provider config from `launchd` environment in the merged binary.

### Safe deferrals

1. Browser badge for "created by agent".
2. Exporting PID and exit code from `sessions.SessionInfo`.
3. Agent attach-to-existing-human-session workflow.
4. Any post-crash resume or PTY reattach semantics.

## Final recommendation

Do the merged binary.

Do the tool-interface rebind.

Do the hardened chat transport inside `termserver`.

But do **not** ship the adapter as "just call `sessions.Manager` and let lease errors fall out." That is the exact kind of false ownership boundary the earlier `tesla` rounds were meant to kill. The correct M8 v1 is still small, but it needs:

- adapter-held agent lease
- adapter-held read cursor buffer
- command-aware session creation in `exo`
- adapter-owned session namespace

Without those four, the design compiles on paper and disagrees with itself at runtime.
