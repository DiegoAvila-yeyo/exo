# M8 Round 2: closing the exact shapes

Source-of-truth note first: `AGENTS.md` says to read `/Users/eltitoyeyo/YEYO/YEYO_ESTADO.md`, but that file is not present on disk from this workspace as of **August 1, 2026**. I still followed the requested source order by reading `M8_INTEGRATION_DESIGN.md` first, then the live `exo` and `nucleo-base` code.

## Executive verdict

Round 1's structural decisions still hold. Round 2 does **not** uncover a new architecture-level disagreement like the bare-`Write()` bug from round 1. The remaining work is now mostly contract-shaping.

I see **zero new fundamental problems** if you adopt the concrete resolutions below.

I do see **one place that becomes a build-time trap if left vague**:

1. `exo` must gain a real command-aware creation path. `sessions.Manager.Create(workdir, name)` still only opens an interactive shell and stores `pty.Command()` into `SessionInfo.Command` (`/Users/eltitoyeyo/exo/sessions/manager.go:80-155`). `realpty.New` still only resolves a shell and starts `shell -i` (`/Users/eltitoyeyo/exo/realpty/realpty.go:45-63`, `183-231`). If M8 tries to fake `terminal_open` by writing the command into an already-open shell, it regresses lifetime and exit semantics relative to `terminal.Manager.Open` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:61-173`).

Everything else below is now concrete enough to turn into build prompts.

## 1. Exact command-aware session creation shape in `exo`

### What the current code actually supports

- `sessions.Manager.Create(workdir, name)` validates the workdir, allocates an id, calls `realpty.New(...)`, wraps it in `ptyactor.NewSession(pty)`, and records `SessionInfo.Command = pty.Command()` (`/Users/eltitoyeyo/exo/sessions/manager.go:80-155`).
- `realpty.New` does not accept a command. It only tries shell candidates and calls `startShell(shell, workdir, cfg)` (`/Users/eltitoyeyo/exo/realpty/realpty.go:45-63`).
- `startShell` hardcodes `exec.Command(shell, "-i")` and sets `term.command = shell` (`/Users/eltitoyeyo/exo/realpty/realpty.go:183-205`).
- `ptyactor` already supports non-default initial ownership today via `WithInitialOwner` on `NewSession` (`/Users/eltitoyeyo/exo/ptyactor/session.go:164-202`, `444-448`).

### Concrete resolution

Make this **additive**, not replacing the existing browser-facing API:

```go
type CreateOptions struct {
    Workdir      string
    Name         string
    Command      string
    InitialOwner string
}

func (m *Manager) Create(workdir, name string) (SessionInfo, error) {
    return m.CreateWithOptions(CreateOptions{
        Workdir: workdir,
        Name:    name,
    })
}

func (m *Manager) CreateWithOptions(opts CreateOptions) (SessionInfo, error)
```

Why additive:

- `termserver` already depends on the simple `Create(workdir, name)` shape through its `sessionStore` interface (`/Users/eltitoyeyo/exo/termserver/server.go:29-35`, `181-217`).
- Replacing `Create` would force unrelated M3/M6 surface churn with no design upside.
- A wrapper preserves all existing browser behavior and keeps M8 scoped.

### Lower-level `realpty` shape

I would keep the public change at `sessions.Manager` and add a small, also-additive `realpty` extension:

```go
func WithCommand(command string) Option
```

Behavior:

- `WithCommand("")` or omitted: keep today's interactive-shell path.
- `WithCommand(non-empty)`: launch `/bin/sh -lc <command>` in the PTY, but record the **logical requested command string** in `Terminal.command`, not `/bin/sh`.

That matches what `terminal.Manager.Open` does today in `nucleo-base` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:89-100`) and avoids inventing an argv-style API when the tool contract is already string-based.

### `SessionInfo.Command`: confirm or correct round 1?

Round 1's assumption should be **corrected**.

`SessionInfo.Command` should now store the **logical launched command** for command-backed sessions, not the shell executable path. Reasons:

- The field is named `Command`, and both browser and adapter consumers want the human-meaningful command.
- `termserver` exposes `SessionInfo` directly from `GET /api/sessions` (`/Users/eltitoyeyo/exo/termserver/server.go:181-217`), so keeping `/bin/zsh` there for agent-created `npm run dev` sessions would be worse than the current round-1 workaround.
- Existing plain `Create` can still store the shell path, because for the interactive-shell case the shell is the real command.

So the new rule should be:

- `Create(...)` interactive shell: `SessionInfo.Command = resolved shell path`
- `CreateWithOptions(... Command: "npm run dev" ...)`: `SessionInfo.Command = "npm run dev"`

That means the adapter no longer needs a separate command field just to paper over `SessionInfo.Command`.

### `InitialOwner`: does it need plumbing, and is the support already there?

Yes, and yes.

- It **does** need to be plumbed from `CreateWithOptions` to `ptyactor.NewSession(pty, ptyactor.WithInitialOwner(...))`, otherwise M8 cannot explicitly start agent-owned sessions.
- The support already exists in shipped `ptyactor`; nothing new is needed there (`/Users/eltitoyeyo/exo/ptyactor/session.go:164-202`, `444-448`).

For minimal churn:

- Keep `Create()`'s default behavior as-is by omitting `InitialOwner`.
- Have the M8 adapter call `CreateWithOptions(... InitialOwner: "agent")`.

I would **not** take this round to relabel all browser-created sessions as `"human"`; that is a separate semantics cleanup, not required for M8 correctness.

### Verdict on point 1

Straightforward detail to fill in, not a new fundamental problem.

## 2. The `Kind` / `ApprovalMode` classifier the adapter needs

### What the current code actually does

The classifier is unexported and tiny:

- `classifyCommand(command string) SessionKind` lives in `terminal/manager.go` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:491-507`)
- `approvalMode` is then synthesized as `"prompt_on_write"` only for `SessionKindShellLike`, otherwise `"default"` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:83-87`)

The shell-like list today is:

- `bash`, `zsh`, `sh`, `fish`
- `python`, `python3`, `node`, `irb`, `psql`, `mysql`, `sqlite3`
- `rails console`, `rails c`
- `python -i`, `python3 -i`
- `node ` prefix when it is not a `.js` file

### Concrete resolution

Do **not** export it from `nucleo-base` for M8 v1.

Duplicate the classifier logic inside the `exo` adapter and add an explicit comment that it is intentionally mirrored from:

- `/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:491-507`

Why duplication is the right v1 tradeoff:

- Round 1 explicitly closed `nucleo-base` changes to the six-method interface swap in the five terminal tool structs. Exporting classifier helpers would widen the agreed change surface.
- The logic is only 17 lines and stable.
- The adapter only needs exactly two outputs: `kind` and `approval_mode`.

### Exact adapter rule

Mirror the classifier byte-for-byte, then synthesize:

- `shell_like` -> `approval_mode = "prompt_on_write"`
- `command` -> `approval_mode = "default"`

### Residual risk

The only real risk is classifier drift if `nucleo-base` changes its list later. That is acceptable in v1 because:

- this is planning for a merged binary pinned to one repo state, not a long-lived remote API contract
- the duplicated logic is small enough to keep in sync deliberately

### Verdict on point 2

Straightforward detail. No round 3 needed here.

## 3. Error mapping: `ptyactor` / `exo` errors -> `ToolEnvelope`

### What the agent actually consumes

The terminal tools marshal `terminal.ToolEnvelope` to JSON and mark the tool call as an error whenever `err != nil` or `env.OK == false` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminal_common.go:9-25`).

So the agent does **not** see Go error types directly. It sees JSON like:

```json
{"ok":false,"session_id":"...","error":"..."}
```

plus any extra status fields you preserve.

This is why vague strings would be a real agent-quality regression, not just ugly logs.

### Current baseline behaviors from `terminal.Manager`

- read timeout with no new output is a **success**, not an error (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:184-199`)
- write to a non-running session returns:
  - `ok:false`
  - `status`
  - `session_kind`
  - `approval_mode`
  - `exit_code`
  - `already_exited:true`
  - `error:"session is not running"`
  (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:212-241`)
- kill on an already-exited session is **idempotent success** with `already_exited:true` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:270-285`)

### Concrete mapping table

#### `terminal_open`

1. Invalid or missing `command`
   - Match current `terminal.Manager.Open`
   - Return: `{"ok":false,"error":"command is required"}`
   - No `session_id`
   - Error result: yes

2. Invalid `workdir`, session cap reached, PTY launch failure, no usable shell, sessionstore persistence failure
   - Return: `{"ok":false,"error":"<real error string>"}`
   - No `session_id` unless the id has already been durably committed and you explicitly choose to expose it
   - Error result: yes

3. Command process exits during startup wait
   - This is **not** a launch failure if the PTY/session itself was created successfully
   - Return success envelope from current metadata:
     - `ok:true`
     - `session_id`
     - `status:"exited"` or `status:"failed"`
     - `cursor`
     - `output`
     - optional `exit_code` if available
   - Error result: no

That exactly mirrors the current `terminal.Manager.Open` design where the PTY can start and then the command can immediately finish before the initial snapshot (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:151-173`).

#### `terminal_read`

4. Read timeout with no new data
   - Return success
   - Shape:
     - `ok:true`
     - `session_id`
     - `status:<current>`
     - `cursor:<unchanged or advanced if EOF/exit bookkeeping advanced it>`
     - `output:""`
     - `truncated:false`
   - Error result: no

This must match current behavior (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:184-199`).

5. Read on unknown session / missing metadata
   - Return:
     - `ok:false`
     - `session_id`
     - `error:"<real not found error>"`
   - Error result: yes

6. Collector buffer had to evict older bytes before `since_cursor`
   - Return success
   - Shape:
     - `ok:true`
     - `session_id`
     - `cursor:<new cursor>`
     - `output:<retained suffix>`
     - `truncated:true`
   - Error result: no

7. Collector subscription was force-closed by takeover and immediately resubscribed
   - Invisible to the tool caller
   - Return ordinary success or timeout result
   - Error result: no

#### `terminal_write`

8. `ptyactor.ErrOwnershipLost`
   - This is the important new M8-specific case.
   - Return:
     - `ok:false`
     - `session_id`
     - `status:"running"`
     - `session_kind`
     - `approval_mode`
     - `already_exited:false`
     - `error:"terminal ownership lost (session taken over by another client)"`
   - Error result: yes

Why this exact wording:

- "session is not running" would be false and would teach the agent the wrong remedy
- "ownership lost" tells the model it should stop trying to write and explain that a human took control

9. Write to exited / killed session
   - Preserve current shape:
     - `ok:false`
     - `session_id`
     - `status:<exited|failed|killed>`
     - `session_kind`
     - `approval_mode`
     - `exit_code`
     - `already_exited:true`
     - `error:"session is not running"`
   - Error result: yes

10. PTY write I/O failure while session is nominally live
   - Return:
     - `ok:false`
     - `session_id`
     - `status:"running"` if that is still the last known meta, otherwise latest known status
     - `session_kind`
     - `approval_mode`
     - `error:"<real write error>"`
   - Error result: yes

11. `wait <= 0`
   - Preserve current semantics
   - Return success envelope immediately, with no output, cursor set to current tail (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/manager.go:256-267`)

#### `terminal_kill`

12. Kill on already-exited session
   - Preserve idempotent success:
     - `ok:true`
     - `session_id`
     - `status:<exited|failed|killed>`
     - `already_exited:true`
   - Error result: no

13. Kill of live adapter-owned session succeeds
   - Return success:
     - `ok:true`
     - `session_id`
     - `status:"killed"`
     - `session_kind`
     - `approval_mode`
     - optional `exit_code`
     - `already_exited:true`
   - Error result: no

14. Kill on unknown session
   - Return:
     - `ok:false`
     - `session_id`
     - `error:"<not found>"`
   - Error result: yes

### Verdict on point 3

This is now concrete enough for a build prompt. The one non-negotiable addition is the explicit `"terminal ownership lost ..."` error string for stale agent leases.

## 4. Exact SSE schema for `GET /api/chat/stream`

### What `dashboard` actually emits today

`dashboard` writes unnamed SSE `data:` events with JSON payloads, plus heartbeat comment lines:

- `{"type":"idle"}` or `{"type":"busy"}` on connect (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:78-84`)
- `{"type":"approval","prompt":"...","detail":"..."}` when an approval is pending (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:85-89`, `110-114`)
- `{"type":"output","text":"..."}` for broadcast text (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:101-106`)
- `{"type":"done"}` when the turn ends (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:107-109`)
- `: heartbeat` every 10 seconds (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:91-121`)

### What `termserver` should do

Reuse that wire pattern exactly for transport simplicity, but make **one deliberate extension** to approval events.

Why only approval gets extended:

- `output` events are agent/chat text, not raw terminal bytes, so they are still turn-global.
- Multi-session-per-turn does create a correlation problem for approvals.
- `tool.RequestToolApproval` only gives `prompt` and `detail` strings to the host callback today (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminalapproval.go:18-47`), and `dashboard.SetPendingApproval` stores only those strings (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/server.go:172-176`).

So the clean v1 answer is:

- keep the approval callback signature unchanged
- in `termserver`, opportunistically parse `session_id` from the known `terminal_write` prompt shape:
  - prompt format today is `approve write to interactive terminal <sessionID>?`
  - emitted by `TerminalWriteTool` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminal_write.go:65-70`)
- if parse succeeds, include `session_id`; otherwise omit it

### Exact event payloads

All events are sent as:

```text
data: <json>

```

Heartbeat remains:

```text
: heartbeat

```

#### Idle

```json
{"type":"idle"}
```

Meaning:

- no turn is currently running

#### Busy

```json
{"type":"busy"}
```

Meaning:

- a turn is currently running or the single-flight lock is held

#### Output

```json
{"type":"output","text":"..."}
```

Notes:

- `text` is agent/chat output, not terminal PTY bytes
- no `session_id` field

#### Approval

```json
{"type":"approval","prompt":"...","detail":"...","session_id":"session-0007"}
```

`session_id` is optional:

```json
{"type":"approval","prompt":"...","detail":"..."}
```

Rules:

- include `session_id` only when `termserver` can parse it from the known `terminal_write` prompt format
- do not block the design on changing `approval.go` to structured metadata

#### Done

```json
{"type":"done"}
```

Meaning:

- the current turn finished and the client can go back to idle polling state
- any actual failure text will already have been emitted as an `output` event, matching `dashboard.handleChat` (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:155-173`)

### HTTP/auth shape

This should follow existing `termserver` conventions exactly:

- `POST /api/chat`
  - `ValidOrigin`
  - `ValidateDoubleSubmit`
- `GET /api/chat/stream`
  - `ValidReadOrigin`
  - no CSRF check
- `POST /api/approve`
  - `ValidOrigin`
  - `ValidateDoubleSubmit`

That matches how `termserver` already treats mutating vs read routes (`/Users/eltitoyeyo/exo/termserver/server.go:156-217`, `222-250`) and matches round 1's auth conclusion.

### Verdict on point 4

Concrete. No round 3 required unless you insist on a guaranteed structured `session_id` for **every** approval event without relying on prompt parsing; that would require widening the approval callback contract beyond the already-closed round-1 scope.

## 5. Concrete test plan by package

The right default test strategy is:

- for adapter behavior: use the **real `ptyactor.Session` with a fake PTY at the bottom**
- for `sessions` command-creation behavior: add a constructor seam or test helper around `realpty`, because this is about creation metadata and option plumbing, not takeover races
- for `termserver` SSE chat: use fake runner / fake approval / fake adapter backend; no real PTY needed unless the test is intentionally WS + takeover related

Reason:

- `WriteWithLease`, `SubscribeWithLease`, and takeover epoch behavior are the heart of M8, and those semantics already live in real `ptyactor` (`/Users/eltitoyeyo/exo/ptyactor/session.go:222-287`, `376-418`)
- `ptyactor` already has strong tests for stale writes, stale subscriptions, slow subscribers, and replay (`/Users/eltitoyeyo/exo/ptyactor/session_test.go:11-216`)
- `termserver` already has good WS takeover tests to build on (`/Users/eltitoyeyo/exo/termserver/server_test.go:275-343`, `424-464`)

### Proposed package split

1. `exo/sessions`
   - `TestCreateWithOptionsStartsCommandBackedSession`
   - Purpose: `CreateWithOptions` stores `SessionInfo.Command` as the logical command string, not shell path
   - Test double: fake `realpty` factory seam

2. `exo/sessions`
   - `TestCreateWrapperStillCreatesInteractiveShell`
   - Purpose: old `Create(workdir, name)` remains behavior-compatible
   - Test double: fake `realpty` factory seam

3. `exo/m8adapter` or whatever adapter package you introduce
   - `TestOpenCapturesLeaseAndClassifierMetadata`
   - Purpose: `Open` stores agent lease snapshot, `kind`, `approval_mode`, and returns current cursor/output snapshot
   - Test double: real `ptyactor.Session` + fake PTY

4. `exo/m8adapter`
   - `TestWriteReturnsOwnershipLostAfterHumanTakeover`
   - Purpose: stale agent lease becomes `ToolEnvelope{ok:false,error:"terminal ownership lost ..."}` after `Takeover("human")`
   - Test double: real `ptyactor.Session` + fake PTY

5. `exo/m8adapter`
   - `TestReadTimeoutReturnsSuccessWithEmptyOutput`
   - Purpose: long-poll deadline with no output is success, not error
   - Test double: real `ptyactor.Session` + fake PTY

6. `exo/m8adapter`
   - `TestCollectorResubscribesAfterTakeoverAndCursorContinues`
   - Purpose: read collector survives forced subscription close on takeover and keeps monotonic cursor semantics
   - Test double: real `ptyactor.Session` + fake PTY

7. `exo/m8adapter`
   - `TestWriteToExitedSessionMatchesTerminalManagerEnvelope`
   - Purpose: preserve `already_exited:true`, status, exit-code, and `"session is not running"` shape
   - Test double: real `ptyactor.Session` + fake PTY or adapter-local fake session metadata, whichever gives cleaner exit control

8. `exo/m8adapter`
   - `TestKillIsIdempotentForAlreadyExitedSession`
   - Purpose: preserve current `terminal.Manager.Kill` semantics: success with `already_exited:true`
   - Test double: adapter-local fake metadata/session or real `ptyactor.Session` if easy

9. `exo/termserver`
   - `TestChatPostReturns409WhenTurnBusy`
   - Purpose: preserve single-flight lock behavior from `dashboard.handleChat` (`/Users/eltitoyeyo/nucleo-base/layer1-harness-shell/dashboard/chat.go:140-145`)
   - Test double: fake runner

10. `exo/termserver`
    - `TestChatStreamReplaysBusyIdleApprovalDoneHeartbeatSchema`
    - Purpose: assert exact SSE event JSON shapes and heartbeat comments
    - Test double: fake broadcaster + fake approval state + fake runner

11. `exo/termserver`
    - `TestApprovalEventIncludesParsedSessionIDForTerminalWritePrompt`
    - Purpose: approval correlation for shell-like terminal writes without changing approval callback shape
    - Test double: fake approval state

12. `exo/termserver`
    - `TestChatStreamUsesValidReadOriginAndApproveUsesCSRF`
    - Purpose: route auth matches existing `termserver` pattern
    - Test double: plain HTTP tests

### The 8-item minimum build-prompt version

If you want the minimal list exactly sized for a build prompt, use these 8:

1. `exo/sessions`: `CreateWithOptions` launches command-backed sessions and records logical command metadata correctly.
2. `exo/m8adapter`: `Open` returns correct `kind` and `approval_mode` for shell-like vs command sessions.
3. `exo/m8adapter`: stale agent lease after browser takeover returns the explicit ownership-lost envelope on write.
4. `exo/m8adapter`: read timeout with no new output returns success with empty output, not an error.
5. `exo/m8adapter`: collector resubscribes after takeover and continues cursor-based reads.
6. `exo/m8adapter`: write/kill against exited sessions preserve `already_exited` semantics compatible with `terminal.Manager`.
7. `exo/termserver`: `/api/chat` preserves single-flight `409 busy`, and `/api/chat/stream` emits `idle|busy|output|approval|done` plus heartbeat with the exact JSON shapes above.
8. `exo/termserver`: approval events include parsed `session_id` for interactive-terminal approvals, and route auth matches `ValidOrigin` / `ValidReadOrigin` / CSRF expectations.

### Manual-only test that should stay manual

One thing still deserves manual browser verification:

- cross-transport takeover UX during an active agent turn

What to verify manually:

- browser opens chat stream and one terminal WS
- agent opens or drives a shell-like session
- approval modal appears
- user takes over the terminal from another browser tab or terminal view
- next agent write fails cleanly as ownership-lost in chat
- terminal viewer keeps streaming output after takeover
- approval UI still maps to the correct session

This is the same class of UX race that `termserver` already had to fix for WS takeover fanout (`/Users/eltitoyeyo/exo/termserver/server.go:372-389`, `508-521`; `/Users/eltitoyeyo/exo/termserver/server_test.go:275-343`).

## Updated punch list

### What is now fully concrete enough for build prompts

- additive `CreateWithOptions(CreateOptions{Workdir, Name, Command, InitialOwner})`
- `Create()` stays as a thin wrapper
- `InitialOwner` is plumbed into existing `ptyactor.WithInitialOwner`
- `SessionInfo.Command` stores the logical command for command-backed sessions
- classifier is duplicated inside `exo` adapter, not exported from `nucleo-base`
- exact `ToolEnvelope` error mapping is defined, especially ownership-lost vs already-exited
- exact SSE payload shapes are defined
- package-by-package test plan is defined

### Is round 3 needed?

**Not for build-prompt readiness.**

I would only schedule a round 3 if you want to debate one of these optional refinements before implementation:

1. whether approval SSE events should have a guaranteed structured `session_id` field via a widened approval callback instead of prompt parsing
2. whether browser-created non-agent sessions should also start with `InitialOwner:"human"` for cleaner lease labels
3. whether `exo` should expose `PID` and `ExitCode` in `SessionInfo` during the same milestone or defer them

Those are refinements, not blockers.

## Bottom line

As of **August 1, 2026**, I think M8 is now concrete enough to write real build prompts **without** a round 3, provided the prompt explicitly locks in:

- additive `CreateWithOptions`
- `SessionInfo.Command` semantic correction
- duplicated classifier in `exo`
- explicit ownership-lost `ToolEnvelope` string
- approval-event `session_id` as optional parsed metadata, not a required new `nucleo-base` API
