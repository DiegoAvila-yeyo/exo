This is round 2 of the M8 design-critique conversation (round 1 closed, see
`~/exo/M8_INTEGRATION_DESIGN.md` for the full consolidated state — read that file first, it is the
canonical source of truth, not the round 1 prompt/response files). Still planning only — nothing
gets built yet. Same adversarial-review pattern as `tesla` and `exo`'s M0-M7.

## Recap: what round 1 already closed (do not re-litigate)

- Merged Go binary: `exo`'s `launchd`-activated backend hosts the full `nucleo-base` agent stack
  in-process (via the existing `replace` in `go.mod`). No RPC, no MCP, no second process — this is
  a deliberate, explicit choice: the integration between `exo` and `nucleo-base` is a **direct Go
  import**, not a service boundary. Do not propose MCP or any other RPC/IPC mechanism as an
  alternative; that door is closed.
- Only change inside `nucleo-base`: the five `terminal_*` tool structs' field type moves from
  concrete `*terminal.Manager` to a new 6-method interface (`Open/Read/Write/Kill/List/
  SessionMeta`). Nothing else in `nucleo-base` — `provider`, `agent`, `runtime`, `approval.go`,
  Layer 3, Layer 5 — gets touched.
- All new code lives in `exo`: a terminal adapter (holds an agent lease snapshot per session, uses
  `WriteWithLease`/`ResizeWithLease` — never bare `Write`/`Resize`, since bare `Write` captures the
  *current* lease and would silently survive a human takeover — this was a real bug caught in
  round 1), an incremental read-cursor buffer per adapter session (subscribes once at open,
  accumulates, resubscribes after a takeover force-close), and new SSE-based chat/approval routes
  in `termserver` (`POST /api/chat`, `GET /api/chat/stream`, `POST /api/approve`), separate from
  the existing per-session terminal WebSocket.
- `terminal_open` always creates a brand-new `exo` session (no attach-to-existing-session in v1);
  adapter-facing tools (`terminal_list/read/write/kill`) only ever see adapter-opened sessions.
- Crash recovery (M5) is cleanup-only, no live session restore — so no ownership-handshake logic
  needed after a crash/restart.
- Provider credentials load from the `launchd` plist environment in the merged binary, fail-fast on
  missing config.
- Global single-flight agent-turn lock (`TryLock`, 409 `busy`) is kept as-is.

## What round 1 left open — this round's scope

1. **Exact shape of command-aware session creation in `exo`.** `sessions.Manager.Create(workdir,
   name)` only opens an interactive shell; `terminal_open` needs to launch a specific command.
   Round 1 said "add `CreateWithOptions(CreateOptions{Workdir, Name, Command, InitialOwner})` or an
   equivalent lower-level `realpty` constructor" but did not nail down the actual signature. Read
   `~/exo/sessions/manager.go` and `~/exo/realpty/realpty.go` for real before proposing this. Open
   questions to resolve: is this additive (`Create` stays, `CreateWithOptions` is new) or does
   `Create` become a thin wrapper calling `CreateWithOptions` with a default shell command? What
   happens to `SessionInfo.Command` for a command-backed session — does it now store the real
   launched command instead of the shell path (round 1's field-mapping table assumed `SessionInfo.
   Command` is *not* reused for this, adapter tracks command separately — confirm that's still
   right or correct it)? Does `InitialOwner` need to be plumbed all the way through to
   `ptyactor.NewSession`'s owner-init path, and does that path support a non-default initial owner
   today (check `ptyactor/session.go`'s actual constructor, don't assume)?

2. **The `Kind`/`ApprovalMode` classifier the adapter needs.** Round 1 said the adapter should
   "reuse the same classifier logic" the old `terminal.Manager` uses to decide `SessionKindShellLike`
   etc. Read `~/nucleo-base/layer2-runtime-rails/terminal/manager.go` (and any classifier
   helper file near it) for the actual logic. Can it be called directly by the adapter as an
   exported function (if so, name it), or does it need to be duplicated/reimplemented in `exo`
   because it's unexported or too tightly coupled to `terminal.Manager`'s internals? If
   duplication is required, is that an acceptable v1 tradeoff (small, stable classifier logic) or
   does it need to be extracted into a shared, exported helper in `nucleo-base` first (a small,
   safe refactor, not a logic change — say explicitly which).

3. **Error-mapping table: `ptyactor`/`exo` errors → `ToolEnvelope` error shape.** The agent already
   knows how to interpret `ToolEnvelope` errors from the *old* `terminal.Manager` (some existing
   error shape/convention — find and document it from `~/nucleo-base/layer2-runtime-rails/
   terminal/manager.go` and however `ToolEnvelope` represents failure, e.g. an `Error` field, a
   `Status` field, etc.). Produce the actual mapping: `ptyactor.ErrOwnershipLost` → ?, a
   command-launch failure in the new `CreateWithOptions` → ?, a `Read` timeout with no new data →
   ? (is that success with empty output, or an error?), writing to an exited/killed session → ?.
   Be exhaustive — this table is what the agent's prompt/reasoning will actually see when things go
   wrong, so vague error strings will produce a worse agent, not just an ugly log line.

4. **Exact SSE event schema for `/api/chat/stream`.** Round 1 said "reuse the pattern" of
   `dashboard/broadcaster.go`'s events (`idle`/`busy`/`output`/`approval`/`done` + heartbeat) but
   didn't confirm field-for-field parity is actually right for the new termserver-hosted version,
   or whether M8 needs something dashboard's version didn't have (e.g. does the browser now also
   need a `session_id` field on `output`/`approval` events so the UI can correlate a chat approval
   prompt with which terminal session it's about, given the agent may now be driving multiple
   sessions in one turn per round 1's decision to keep multi-session-per-turn allowed)? Write the
   actual JSON shape for every event type, not just names.

5. **Test plan, concretely.** Round 1 gave an 8-item minimum test list and named "cross-transport
   takeover UX during an active turn" as the one thing only manual browser verification will catch.
   Turn that into something a build prompt can hand to Codex directly: for each of the 8 items,
   what package does the test live in (adapter package in `exo`, or `termserver`), and does it need
   a fake `ptyactor.Session`/fake `PTY`, or the real `ptyactor.Session` with a fake `PTY` at the
   bottom (round 1 said "real `ptyactor.Session` with fake PTY", confirm that's sufficient given
   `WriteWithLease` is now central to the design, not just `Write`).

## What I want from you

Same as round 1: don't just critique, propose concrete resolutions for all 5 points above, citing
real file/line references from `~/exo` and `~/nucleo-base` (read the actual current code, don't
assume round 1's descriptions are exhaustive — they were accurate but not exhaustive quotes). Flag
anything that turns out to be a new fundamental problem (like round 1 found two) versus a
straightforward detail to fill in. End with an updated punch list: is everything now concrete
enough to write real build prompts, or is there a round 3 needed and on what specifically?

Do not touch or modify any files in `~/exo`, `~/nucleo-base`, `~/pacta-harness`, or `~/forge` —
planning/critique only. Do not produce diagrams this round (round 1's `~/exo/diagrams_draft/*.svg`
are still the current drafts, no changes needed until the design is fully closed).

Write your full response to `~/exo/M8_design_round2_response.md`.