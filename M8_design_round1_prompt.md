This is round 1 of a new design-critique conversation for **M8**, continuing the same Claude↔Codex
adversarial-review pattern used for `~/tesla/DASHBOARD_TERMINAL_DESIGN.md` (rounds 1-10, closed)
and for `exo`'s M0-M7 build prompts. Planning only — nothing gets built or merged yet. Your job is
to attack this draft as hard as you attacked the `tesla` rounds: find the races, the undefined
states, the places where two already-closed subsystems disagree about who's in charge.

## Recap: what's closed and must not be re-litigated

- **`~/tesla/DASHBOARD_TERMINAL_DESIGN.md`** (10 rounds, closed): single actor-goroutine per PTY
  session, `owner` (string) + monotonic `epoch uint64`, all ops (write/read-subscribe/resize/
  takeover) serialized as messages through that goroutine, stale-epoch write/resize returns
  `ErrOwnershipLost`, stale-epoch subscriber channel is force-closed on takeover (not left
  hanging), WS auth via `Sec-WebSocket-Protocol` token + double-submit cookie for HTTP + strict
  `Origin` allow-list (no wildcard CORS), one backend process hosts multiple named PTY sessions,
  `launchd` socket-activated single instance (no separate supervisor), crash recovery via process
  groups + env markers, no PTY reattach in v1 (session lost on crash, not reattachable).
- **`exo` M0-M7`** (built, reviewed, `go test -race` clean, manually verified in-browser): this
  design is now real code, not just paper. Confirmed API surface:
  ```go
  // ptyactor.Session — pure Go, no HTTP/WS dependency, directly importable
  func (s *Session) Lease() Lease                              // {Owner string; Epoch uint64}
  func (s *Session) Write(data []byte) error                   // captures current lease internally
  func (s *Session) WriteWithLease(lease Lease, data []byte) error
  func (s *Session) Subscribe() (<-chan []byte, func(), error) // live stream, no cursor/replay at this layer
  func (s *Session) Resize(cols, rows int) error
  func (s *Session) Takeover(newOwner string) (Lease, error)   // owner++epoch, force-closes old subscribers
  func (s *Session) Close() error
  // defaultOwner = "agent" — "agent"/"human" are just string labels, not an enum

  // sessions.Manager — wraps ptyactor.Session + realpty.Terminal + sessionstore
  func (m *Manager) Create(workdir, name string) (SessionInfo, error)
  func (m *Manager) Get(id string) (*ptyactor.Session, SessionInfo, bool)
  func (m *Manager) List() []SessionInfo
  func (m *Manager) Close(id string) error
  func (m *Manager) Touch(id string) bool
  ```
  `termserver` is one consumer of `sessions`/`ptyactor` via a narrow interface — CSRF
  (`ValidateDoubleSubmit`), strict Origin (`ValidOrigin`/`ValidReadOrigin`), WS subprotocol token.
  Frontend is vendored xterm.js with a "Agent has control" lock banner and explicit
  "Take control" action (no auto-takeover on keystroke — that was explicitly rejected in `tesla`
  round 10).
- **`nucleo-base`'s existing agent stack** (`layer2-runtime-rails/{agent,runtime,provider,tool}`)
  is a verified faithful port of `pacta-harness`, confirmed by direct diff, byte-for-byte except
  import-path rewrites. It has its own, unrelated, weaker terminal backend:
  ```go
  // terminal.Manager — polling-based, no epoch, no takeover, no resize
  func (m *Manager) Open(ctx context.Context, opts OpenOptions) (ToolEnvelope, error)
  func (m *Manager) Read(sessionID string, opts ReadOptions) (ToolEnvelope, error) // polls, time.Sleep(100ms) loop
  func (m *Manager) Write(sessionID, input string, appendNewline bool, wait time.Duration, maxBytes int) (ToolEnvelope, error)
  func (m *Manager) Kill(sessionID, signal string) (ToolEnvelope, error)
  func (m *Manager) List(includeExited bool) ([]SessionMeta, error)
  ```
  The five `terminal_*` tool structs (`terminal_open.go` etc.) hold `Manager *terminal.Manager` as
  a **concrete pointer field**, not an interface — `Execute()` calls straight through to it. There
  is no `init()`/`Register()` for these five tools in the checked-out repo, and no `main.go` — the
  actual construction/injection point (`terminal.NewManager(harnessRoot)` →
  `&tool.TerminalOpenTool{Manager: ...}`) lives outside this snapshot, confirmed by a comment in
  `terminalapproval.go:14-15` ("The parent agent's Confirm callback is wired to this in main.go").
  Approval-for-shell-writes is a **separate global**: `tool.SetGlobalApproveFunc` /
  `tool.RequestToolApproval(prompt, detail string) bool`, called directly inside `terminal_write.go`
  when `meta.Kind == terminal.SessionKindShellLike` — independent of `agent/approval.go`'s
  `needsApproval()` gate (which already excludes `terminal_list/read/write/kill` from the
  agent-loop approval gate, and routes `terminal_open` through `bashNeedsApproval`).
- **`nucleo-base`'s existing chat wiring** (`layer1-harness-shell/dashboard/chat.go`,
  `server.go`, `broadcaster.go`) — `POST /api/chat` single-flights via `agentMu.TryLock()` (409
  `busy` if already running), runs `s.runner(ctx, msg)` in a goroutine where
  `type AgentRunner func(ctx, string) error` is injected once at startup (same runner the TUI
  uses — dashboard and TUI drive the literal same agent turn loop, no per-request tool registry).
  Approval is channel-based: `SetPendingApproval(ch chan bool, prompt, detail string)` stores the
  channel; `POST /api/approve` sends on it. `GET /api/stream` is SSE, polling `pendingApproval`
  every 250ms and re-emitting on change, plus `output`/`done`/`idle`/`busy` events and a 10s
  heartbeat comment. `broadcaster.go` is SSE fan-out over `io.Writer`, ANSI-stripped, 200-line
  replay ring. **This chat/SSE layer has none of `termserver`'s auth model** (no CSRF, no Origin
  allow-list, no WS subprotocol token) — it was built for a local single-user TUI-adjacent
  dashboard, not for the hardened remote-access model `tesla` designed.

## My draft proposal (attack this)

1. **Process topology**: one merged Go binary. `exo`'s existing `backend`/`launchdsocket`/
   `singleton`/`lifecycle` process (the one `launchd` socket-activates) is the host process. It
   already depends on `nucleo-base` via a `replace` in `go.mod`. At startup this binary now also
   constructs the full agent stack (`provider`, `tool` registry, `runtime.Coordinator`,
   `agent.Agent`) in-process, alongside `sessions.Manager`. No RPC, no second process, no IPC
   protocol to design/secure.
2. **Terminal tool rebind via interface, not fork**: introduce a small interface in
   `nucleo-base/layer2-runtime-rails/tool` (or a new file there) matching `terminal.Manager`'s
   existing method set. Change the five `terminal_*` tool structs' field type from
   `*terminal.Manager` to that interface — mechanical, no logic change, and non-breaking: the TUI
   binary (wherever it lives) keeps constructing the real `terminal.Manager` and still satisfies
   the interface unchanged. `exo`'s new host binary instead constructs an **adapter** (lives in
   `exo`, not `nucleo-base`) that implements the same interface backed by `sessions.Manager`.
3. **Ownership vs. approval are two independent gates, no shared state**: tool-call approval
   (`agent/approval.go` channel mechanism) is reused untouched for "can the agent run this
   command." Terminal write/session ownership is `exo`'s existing `Lease{Owner,Epoch}` — the
   adapter's `Write` call goes through `ptyactor.Session.Write` (which auto-captures the current
   lease), so if a human has called `Takeover("human")` from the browser, the agent's next
   `terminal_write` tool call fails with `ErrOwnershipLost` with no new coordination code needed;
   that error surfaces back through `ToolEnvelope` as a normal tool error the agent sees in its
   context.
4. **Chat endpoint lives in `termserver`, not `dashboard/chat.go`**: extend `termserver` with a new
   chat route reusing its existing CSRF/Origin/auth machinery, internally calling the same
   `AgentRunner` function type `chat.go` already uses (so the runner itself — and everything
   downstream of it, approval included — is reused verbatim, only its HTTP transport changes).
   `dashboard/chat.go`/`server.go`/`broadcaster.go` are not modified; they simply stop being the
   thing `exo` calls into for the web case (TUI-adjacent local usage, if any, is unaffected).

Nothing above touches: `provider/*`, `agent/agent.go`, `runtime/*`, `approval.go`'s mechanism,
Layer 3 (Flamen — stays an empty placeholder), Layer 5 (stays an empty placeholder). The two real
code changes are (a) the interface-typed field on the five terminal tools in `nucleo-base`, and (b)
new code added in `exo` (adapter + chat route) — nothing existing in `nucleo-base` gets rewritten.

## Known gaps in the draft — you must resolve or explicitly defer each of these, don't skip any

1. **Read semantics mismatch.** `terminal_read`'s contract is poll-until-`wait`-or-`maxBytes`,
   return accumulated text since some implicit "last read" point (`ReadOptions`, cursor-like).
   `ptyactor.Session.Subscribe()` is a live channel with **no cursor/replay at that layer** — a
   subscriber that attaches late has already missed everything before it subscribed. Does the
   adapter need its own per-session accumulation buffer (subscribe once at session-open time,
   buffer everything, let `Read` calls drain/peek from that buffer with a cursor), and if so, who
   owns that buffer's lifetime and bound (memory growth if the agent never reads)? Does `exo`'s own
   ring buffer (already built in `ptyactor`/`sessions` for browser replay-on-reconnect) already
   solve this and just need an exposed read-since-offset API, or is that buffer not reachable
   outside the WS handler today? Say plainly whether this needs new API surface on
   `ptyactor.Session`/`sessions.Manager` (a real code change to already-shipped M1-M7 packages) or
   can be built entirely in the new adapter without touching them.
2. **Session lifecycle / ID mapping.** `terminal_open`'s `OpenOptions` vs. `sessions.Manager.Create
   (workdir, name)` — map every field explicitly (command, shell kind, env, cwd, whatever else
   `OpenOptions` carries that `Create` doesn't accept). Does the agent's `terminal_open` always
   create a brand-new `exo` session, or can/should it attach to an already-open session (e.g. one
   the human already has a browser tab on, so the human sees the agent land in the terminal they're
   already looking at, or vice versa)? If sessions are keyed differently in each system
   (`terminal.Manager`'s `sessionID` vs. `sessions.SessionInfo.ID`), does the adapter need a
   translation table, and what happens to an agent-created session's ID when the browser calls
   `GET /api/sessions` — is it just listed like any other, indistinguishable from a human-created
   one, or does the UI need to show "created by agent"?
3. **`SessionMeta`/`ToolEnvelope` field mapping.** `terminal.Manager.List`/`SessionMeta` and
   `sessions.SessionInfo` likely don't have identical fields (exit code, status enum, owner PID,
   command line, kind). Produce the actual mapping table, and call out anything `ToolEnvelope`
   needs that `sessions.SessionInfo` doesn't currently expose (again: new field on already-shipped
   code, or synthesizable in the adapter alone?).
4. **Startup/reconnect ownership handshake.** `defaultOwner = "agent"` is fine for a session the
   agent itself creates. What about a session recovered by `sessionstore` after a crash (M5) that
   previously had `owner="human"` — does the recovered session preserve that owner, and if so does
   the agent's first `terminal_write` on a session it didn't create just correctly fail with
   `ErrOwnershipLost` (probably fine, but say so explicitly), or does something need to reset
   ownership to a known state on recovery?
5. **Provider credentials/config in the merged binary.** `nucleo-base`'s `provider` package needs
   API keys/model config that previously lived in whatever config the TUI/dashboard process loaded
   at startup. Now that `exo`'s `launchd`-activated backend is the host process, where does that
   config come from — same `~/Library/LaunchAgents` plist env vars, a config file path `exo`
   already reads, something new? Don't leave this undefined; it's a real deployment blocker.
6. **Chat/approval event delivery transport in `termserver`.** `termserver` today is WS-for-
   terminal-bytes plus a handful of plain HTTP routes (`/api/sessions`, `/close`) — it has no SSE
   or chat-message-stream mechanism yet. Does the new chat route reuse `broadcaster.go`'s
   `io.Writer`-based SSE fan-out pattern verbatim (copied into `termserver`, or imported if
   `nucleo-base` exports it cleanly), or does it ride the existing terminal WS connection as a
   second message type, or open its own WS? State a concrete choice and defend it against the
   existing auth model (CSRF applies to which of these transports, Origin allow-list applies to
   all of them equally today).
7. **Single-flight vs. multi-session concurrency.** `chat.go`'s `agentMu.TryLock()` means one agent
   turn at a time, globally. Does that still hold when the agent can now open/drive multiple `exo`
   terminal sessions within one turn? Is that a real constraint worth keeping (simplicity, matches
   current behavior) or does M8 need to relax it, and if kept, what's the UX when a second chat
   message arrives mid-turn (already returns 409 `busy` today — is that still right for the new
   transport)?
8. **Failure/restart interaction.** If the merged binary crashes and `launchd` respawns it
   (or the idle-shutdown/on-demand cycle from M4 kicks in), what happens to an in-flight agent
   turn — is "agent turn lost, same as any crash, no resume" an acceptable v1 answer (matching
   `tesla`'s existing "no PTY reattach in v1" stance), or does the chat layer need its own
   awareness of this that it doesn't have today?
9. **TUI backward compatibility.** Confirm explicitly: does introducing the terminal-tool interface
   break the existing TUI's construction path at all (it should not, since it keeps supplying the
   concrete `*terminal.Manager`, which already implements the new interface's method set
   structurally) — call out if you find any signature mismatch that would force a TUI-side change,
   since the user does not want existing logic touched unless truly necessary.
10. **Test strategy**, matching the M1-M7 pattern (real `go test -race`, no live PTY needed at the
    unit level): what's the minimum deterministic test set for the adapter (fake `ptyactor.Session`
    or real one with a fake `PTY`?), and what's the one thing that can only be caught by actual
    manual browser verification (per the M6 lesson — three real bugs, zero caught by tests) for
    this milestone specifically?

## What I want from you

Same format as `tesla` round 9: don't just poke holes, propose concrete resolutions for each of the
10 gaps above, scoped to "what does a real, shippable v1 of M8 need" — not a maximal design. For
each gap: give the simplest design that's actually correct, flag anything that's a real blocking
decision vs. safe to defer, and call out dependencies between gaps explicitly (e.g. gap 1's answer
likely constrains gap 2's session-ID-mapping design). If you think my 4-point draft proposal itself
is wrong in some more fundamental way (wrong process topology, wrong place for the chat endpoint,
etc.) — say so directly and argue for the alternative, the same way earlier `tesla` rounds
overturned earlier drafts (e.g. round 2's shared-PTY rejection). Do not touch or modify any files in
`~/exo`, `~/nucleo-base`, `~/pacta-harness`, or `~/forge` — this is a planning/critique round only.

## Diagrams (new for this round)

Separately from the critique above, produce **draft** architecture diagrams (SVG, self-contained,
similar visual style/complexity to the existing ones at `~/forge/docs/diagrams/*.svg` — look at
those first for style reference) showing your resolved version of the M8 integration:
1. A component diagram: where the merged binary's pieces sit (`ptyactor`/`sessions`/`termserver`
   from `exo`; `agent`/`runtime`/`provider`/`tool` from `nucleo-base`; the new adapter and chat
   route), and how a chat message flows from browser → agent turn → tool call → `ptyactor.Session`
   → PTY → back to browser (both as terminal bytes and as chat/approval events).
2. A sequence diagram for the "human takes over mid-agent-turn" case: agent holds lease, human
   clicks "Take control," agent's next write fails, what the agent and the UI each do next.
Write these SVGs to `~/exo/diagrams_draft/` (new directory, not `~/forge/docs/diagrams/` — those
are the real published diagrams and get updated later, after this design is actually settled and
built, not now). These are working sketches to help reason about the design during critique, not
the final artifact.

Write your full response (critique + resolutions + punch list + the two SVG files) to
`~/exo/M8_design_round1_response.md` (text) and `~/exo/diagrams_draft/` (SVGs).