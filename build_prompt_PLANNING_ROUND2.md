You are building Round 2 of `exo`'s Planning section: letting the chat docked at the bottom of a
Planning Board actually create Boards and Knowledge (Notes/Research/Questions) through real tools,
instead of being a generic chat that happens to sit in that spot. Go + frontend, real build task.

## Context — read before writing any code

1. `~/exo/PLANNING_MANIFESTO.md` — the frozen philosophy. The rule that matters most this round:
   **"El humano siempre dirige. La IA organiza, sugiere y conecta. Nunca es dueña del proyecto."**
   Nothing the AI creates this round becomes canonical silently — see the Tier policy below.
2. `~/exo/planningstore/model.go` — already has `AuthorState` (`AuthorHuman`, `AuthorAISuggested`,
   `AuthorAccepted`, `AuthorRejected`, `AuthorArchived`). This round is largely about finally using
   that field for real instead of it only being exercised by tests.
3. `~/exo/build_prompt_PLANNING_ROUND1.md` and its result — the current state: `/api/plannings*`
   HTTP API, the Planning list/Board screens in `termserver/assets/`, and the chat panel re-parented
   into `#planning-chat-slot` when a board is open (`dockChatIntoBoard`/`undockChatToHome` in
   `app.js`). None of that is redesigned this round — you're adding a capability, not touching layout.
4. How tools are already wired, so you follow the existing pattern instead of inventing a new one:
   - `agenthost/atom_tool.go` — shape of a tool: `Definition() api.ToolDef` + `Execute(ctx,
     rawInput) (string, bool)`, registered on an `*nbtool.Registry`.
   - `agenthost/host.go`'s `buildToolRegistry` (around line 221) — where the fixed tool set is
     assembled once at `agenthost.New`. `Host.SetRootPath` (line 156) is the existing precedent for
     "per-turn context the tools need to read" — Planning context follows the same shape.
   - `termserver/chat.go` — `AgentRunner` (line 25) takes `projectPath string` today; `handleChat`
     reads `project_path` from the request body (line 215) and threads it through
     `sess.ProjectPath` into the runner call (line 262). `project_path` is the template for adding
     `planning_id`/`board_id`.
   - `backend/backend.go`'s `runner` closure (around line 172) — calls `host.SetRootPath(projectPath)`
     before `host.Run`. `SetPlanningContext` (new) slots in right next to it.

## The Tier policy for this round (do not build a general "AI review" system beyond this)

- **Boards are structural containers, not canonical knowledge.** `planningstore.Board` has no
  `Author` field and this round does not add one. AI-created Boards are created directly, with no
  acceptance step and no "AI suggested" marker — this is a deliberate exception, not an oversight:
  a Board is just a named space to put things in, it carries no claim about the project the way a
  Note/Decision/Principle does. Put this sentence in the code comment above `planning_create_board`
  verbatim, so the exception reads as a decision, not a gap: *"Boards are structural containers, not
  canonical knowledge. AI-created Boards may be created directly without Author metadata or
  acceptance. Only Knowledge is governed by AuthorState in Round 2."*
- **Knowledge of type note/research/question**: AI creates them directly via tool call, but the
  created object's `Author` is `AuthorAISuggested`, never `AuthorHuman`. They are visible
  immediately (Tier 1 — additive, non-blocking) with a visual "AI suggested" marker. The human can
  accept (→ `AuthorAccepted`) or reject (→ `AuthorRejected`, hidden from the default view, same as
  Round 1's rejected/archived rule) from the Notes panel. No confirmation dialog interrupts the chat
  to create these.
- **Knowledge of type decision/principle**: **out of scope this round.** The AI must not be given a
  tool that can create or edit these. If asked to make a decision, the agent should say so in chat
  and suggest the human create it directly on the board — do not build a workaround.
- **Deleting or editing anything that already exists**: out of scope. Tools this round only create
  new Boards/Notes/Research/Questions — no update, no delete, no supersede.

## Scope for this round, explicitly

**In scope:**
- Planning context (`planning_id`, `board_id`) flows from the open Board screen → chat request →
  session → `AgentRunner` → `Host`, the same way `project_path` already does, **except for how it
  clears** — see "Context lifecycle" below, this is not a plain copy of the `project_path` behavior.
- Three new tools, always present in the registry, execution-gated on Planning context — see
  "Tool availability" below, this is a firm decision, not left to Codex to interpret:
  `planning_create_board`, `planning_create_note` (covers note/research/question via a `type`
  param), and `planning_list_board` (read-only: lets the agent see what's already on the board
  before adding more, so it doesn't duplicate). All three call `planningstore.Store` directly — no
  duplicated domain logic, no going through HTTP from inside the agent process.
- New Knowledge created this way gets `Author: planningstore.AuthorAISuggested`. New Boards created
  this way carry no Author (see Tier policy above — this is intentional, not an inconsistency).
- Notes panel (`#planning-notes-panel`, already built in Round 1) shows an "AI suggested" badge on
  such entries, with inline Accept/Reject controls that call `PATCH`-style endpoints you add
  (`POST /api/plannings/{id}/knowledge/{knowledge_id}/accept` and `.../reject`).
- Boards created by the AI are indistinguishable from human-created ones in the UI — no badge, no
  marker (Tier policy above). Do not add one.
- Refreshing the UI after the agent acts — see "Post-turn refresh" below, this is a concrete
  requirement, not a suggestion to figure out later.

### Context lifecycle: omitted vs. explicitly cleared

`project_path`'s existing behavior — "send it once, the session remembers it even on turns that
omit it" — is **not** safe to copy verbatim for `planning_id`/`board_id`. A session that keeps
believing it's inside `Planning: Exo / Board: Auth` after the user has navigated back to Home (or
started a new chat) would let planning tools keep acting on that board with no visible context on
screen — exactly the "agent writes to the wrong board without the user noticing" failure mode.

**Planning context is atomic and the state table below is exhaustive — do not infer intent from a
half-complete pair.** `planning_id` and `board_id` are always evaluated as a pair:

| `planning_id`      | `board_id`         | Result                                              |
|---------------------|---------------------|------------------------------------------------------|
| omitted             | omitted             | preserve existing session context, unchanged         |
| non-empty           | non-empty           | set Planning context to that Planning+Board          |
| explicitly `""`     | explicitly `""`     | clear Planning context (both fields cleared)          |
| omitted              | present (any)        | **400** — reject the request; session context unchanged |
| present (any)        | omitted               | **400** — reject the request; session context unchanged |
| non-empty            | explicitly `""`       | **400** — reject the request; session context unchanged |
| explicitly `""`       | non-empty             | **400** — reject the request; session context unchanged |

Whenever both IDs are non-empty, also verify that the referenced Board actually belongs to the
referenced Planning (`board.PlanningID == planningID`) before writing session context or executing
any Planning tool — a client sending a real `planning_id` paired with a `board_id` that belongs to a
*different* Planning must not be able to make the agent write into that other Planning's board.
Treat a mismatch the same as any other invalid pair: 400, session context unchanged.

Only the exact pair `("", "")` means "clear." Every other partial/mismatched combination is a 400,
and — this is the part that matters — **a rejected request must leave whatever context the session
already had completely untouched.** A malformed request is not an implicit clear and not an implicit
update; it's a no-op on state, with an error response.

`app.js`'s chat submit handler sends whatever `state.activePlanning`/`state.activeBoardId` currently
hold — both non-empty while docked in a Board, both `""` everywhere else (Home, or Planning-list).
Never send just one of the two.

**`undockChatToHome()` cannot itself clear the backend session** — it runs on navigation, not
necessarily around a `POST /api/chat` call, so there's nothing to attach a `planning_id: ""` to at
that moment. What it does and must do: clear the *frontend* state (`state.activePlanning =
null; state.activeBoardId = null`) immediately, so that the very next chat submission — from Home,
in whatever session is active — sends the explicit `("", "")` pair per the table above. This also
covers "New chat": even though a fresh session starts with no persisted context at all, its first
message still explicitly sends `("", "")` rather than omitting the fields — defense in depth, not
load-bearing on its own.

Add these tests, exercising the state table directly:
1. Session has Planning A / Board B context set. Send another message in the *same* session with
   `planning_id: ""`, `board_id: ""` → verify the persisted session now has both fields empty, the
   runner receives empty Planning context, and a `planning_create_note` call in that same turn fails
   with "not currently inside a Planning board."
2. Same starting context (Planning A / Board B). Send `{"planning_id":"abc","board_id":""}` → 400,
   **and** verify the session's persisted context is still Planning A / Board B, unchanged (this is
   the important assertion — a bad request must not clear or corrupt existing context as a side
   effect).
3. Same starting context. Send `{"board_id":"xyz"}` with `planning_id` omitted → 400, same
   unchanged-context assertion as above.

### Tool availability: registry stays fixed, execution is gated — not two competing designs

Given how `Host`/`buildToolRegistry` already works (one fixed registry built once at construction,
see `agenthost/host.go` line 221), Round 2 does **not** attempt per-turn dynamic registries. Decision
for this round, final: **the three planning tools are always present in the registry, but their
`Execute` MUST fail with a clear "not currently inside a Planning board" error whenever Host's
Planning context is unset** — this means the model can technically see these tools while in the Home
chat, and that's accepted for this round, not a bug. To make that acceptable in practice: their
`Definition().Description` MUST say outright that they only work inside an active Planning Board, so
a model reading its own tool list understands why a call might fail before it even tries. Do not
build per-turn tool availability (copying `atoms_decision_tool.go`'s registry-swap trick) this
round — that's solving a different problem and is out of scope here. If a future round decides Home
chat must never even see these tools, that's a deliberate scope change for that round, not something
to sneak into this one.

### Post-turn refresh: when the frontend re-fetches after the agent acts

There is no SSE/event for "a board was created" this round, and you should not add one — a plain
refetch after the turn completes is enough. Concrete requirement: whenever a chat turn finishes
(the existing "done" SSE event — see `handleChatEvent`'s `"done"` case in `app.js`) **while the chat
is currently docked in a Planning Board** (`state.view === "planning-board"`):
- Re-fetch the current Planning's Boards (`GET /api/plannings/{id}`) and repopulate the board
  switcher, preserving the currently-selected board if it still exists (reuse the same logic
  `openPlanning`'s `populateBoardSelect` call already uses for "stay on the same board after
  refresh").
- If the Notes panel is currently expanded (`aria-expanded === "true"`), also re-run
  `loadBoardKnowledge()` so a newly created Note/Research/Question shows up without the user having
  to manually toggle Notes closed and open again.
- If the chat is docked but on Home, or the Notes panel is collapsed, skip the corresponding refetch
  — don't turn every "done" event into two unconditional requests regardless of what's visible.

**Out of scope — do not build this round:**
- Decision/Principle creation or editing by the AI (see Tier policy above).
- Editing or deleting existing Boards/Knowledge via chat, by the AI or via the new endpoints beyond
  accept/reject.
- The general infinite canvas, drag/connect, positioning of AI-created objects on it (Round 3+).
- Any change to how Tier 2 (blocking) actions might look — there are none this round, since nothing
  in scope is destructive or touches Decision/Principle.
- Multi-turn "plan mode" or the AI proactively deciding to open/switch boards — it only acts on the
  board the human currently has open.

## What to build

### 1. Thread Planning context through the chat request → runner → Host

**Before writing `Host.SetPlanningContext`, verify concurrency safety first — this is the one
genuinely risky part of Round 2.** `SetRootPath` already mutates shared `Host` state the same way
(`agenthost/host.go` line 156) and `backend.go`'s `runner` closure comment (~line 160) claims this is
safe because "termserver only ever calls the runner while holding its own agentMu, so turns never
overlap." Don't take that on faith for this new field — confirm it by reading how `agentMu` is
actually used in `termserver/chat.go`/`server.go` (is it held for the *entire* turn, including the
whole `host.Run` call, or just around dispatching it?) before adding `planningID`/`boardID` as
mutable fields on `Host`. If turns can genuinely never overlap on one `Host`, mutable fields are
fine and match the existing `SetRootPath` pattern — use it. If you find turns *can* overlap (or the
serialization is weaker than the comment claims), do **not** add shared mutable planning/board
fields — that would let Chat A's tool call read Chat B's board mid-flight, which is the worst
possible bug for this feature (writing AI-suggested knowledge into the wrong Planning). In that case,
thread the context through the call path instead (e.g. as a parameter on `Run`/`Execute`, or a
per-turn value the tools close over) rather than through `Host` state, and say so in your report —
this is worth a short note back, not a silent workaround. Do not use this investigation as a reason
to refactor `Host`'s concurrency model generally; only resolve it for the two new fields.
- `termserver/chat.go`: add `PlanningID`/`BoardID` fields to the chat request body struct (mirror
  `ProjectPath` at line 215) and to whatever session struct carries `ProjectPath` today. Update
  `AgentRunner`'s signature to also take a `planningID, boardID string` (or a small struct if that
  reads better — match whatever's more idiomatic next to the existing `projectPath string` param,
  don't over-engineer a new type for two strings).
- `agenthost/host.go`: add the planning-context plumbing using whichever shape the concurrency check
  above justifies (mutable `Host` field via a `SetPlanningContext` method, matching `SetRootPath`'s
  shape, only if turns are confirmed serialized).
- `backend/backend.go`: in the `runner` closure (~line 172), thread the Planning context through
  next to the existing `host.SetRootPath(...)` call, before `host.Run`.
- `termserver/assets/app.js`: the chat form submit handler already sends `project_path` when set
  (see the `refs.chatForm` submit listener). Send `planning_id`/`board_id` the same way, but unlike
  `project_path`, **always send both keys, every submission** — the real values while docked in a
  Board, explicit `""`/`""` everywhere else (Home, Planning-list, right after `undockChatToHome()`
  runs). Never omit one or the other, and never send just one populated — see the state table in
  "Context lifecycle" above.

### 2. The three tools (new file, e.g. `agenthost/planning_tools.go`)

- Constructed with a `*planningstore.Store` (agenthost will need it injected — add it as a
  parameter to `agenthost.New` or a `WithPlanningStore`-style functional option, whichever matches
  how `newAgentHost` is already called from `backend.go`).
- `planning_list_board`: input `{}` (uses the Host's current planning/board context, not
  agent-supplied IDs — the agent should never need to guess an ID). Returns the board's existing
  Knowledge as **title + type + author** (author matters here, not just for display later — it's
  what lets the model tell an accepted fact apart from a still-pending suggestion instead of
  treating `ai_suggested` content as confirmed; it's also what the round-trip test below needs to
  assert on). **Filter to `human`/`ai_suggested`/`accepted` only — never return `rejected` or
  `archived` entries by default.** A rejected entry must not keep influencing the agent's behavior on
  later turns (e.g. it must not see a rejected "use Redis" note and keep treating it as active
  context) — this matches the product rule that rejected stays historical but disappears from the
  normal workspace. This is Round 2's list endpoint specifically for the agent, not a general
  Knowledge query API — don't add a filter parameter, just hardcode the exclusion.
- `planning_create_board`: input `{name}`. Creates a Board on the *current* Planning (from Host
  context), `Author` doesn't apply to Board (it has none in the model) — fine as is.
- `planning_create_note`: input `{type, title, body}`, `type` restricted to `note`/`research`/
  `question` (reject `decision`/`principle`/`reference` with a clear tool-result error, don't
  silently downgrade them). Creates it on the *current* board with `Author: AuthorAISuggested`.
- All three return a clear error string (not a panic, matching the existing tool `Execute` shape —
  see `atom_tool.go`) when Planning context isn't set for this turn (i.e. tools registered but
  somehow called outside a board — shouldn't happen if step 3 gates registration correctly, but
  defend anyway).
- **Tools must re-load the current Planning (and, where relevant, the current Board within it) from
  `planningstore.Store` on every `Execute` call — never cache a `Planning`/`Board` value across
  turns.** The Board a tool is about to write into may have been deleted or changed between when
  context was set and when the tool actually runs; reloading fresh each time means a missing
  Board surfaces as the same clear "not currently inside a Planning board"-style error instead of a
  nil dereference or a write against stale data.

### 3. Tool registration

Follow "Tool availability" above exactly: register all three tools unconditionally in
`buildToolRegistry`, gate only at `Execute` time. No dynamic per-turn registry swapping.

### 4. HTTP: accept/reject endpoints

- `termserver/planning.go`: add `POST /api/plannings/{id}/knowledge/{knowledge_id}/accept` and
  `.../reject`, thin wrappers that load the Planning, flip `Author` to `AuthorAccepted` or
  `AuthorRejected` on the matching Knowledge entry, `Save`, return the updated entry. Follow the
  existing handler shape in that file exactly (origin validation, CSRF on POST, 404 via
  `loadPlanningOr404`).
- **These endpoints only perform the `ai_suggested → accepted` and `ai_suggested → rejected`
  transitions.** If the target Knowledge entry's current `Author` is anything other than
  `AuthorAISuggested` (already `human`, already `accepted`, already `rejected`, `archived`, ...),
  reject the request (409, with a clear message) instead of flipping it anyway. `AuthorState` is a
  state machine, not a free-form field these two endpoints can set to anything — they exist
  specifically to resolve the one pending state Round 2 introduces, not as a generic "set author"
  API. Add a test for the rejected-transition case (call accept/reject on an already-`human` or
  already-`accepted` entry, confirm it 409s and the entry is unchanged).

### 5. Frontend: badge + accept/reject

- `renderNotesList` in `app.js` (Round 1): when `entry.author === "ai_suggested"`, add a badge
  (reuse `.planning-note-type`'s visual language, don't invent a new component) and two small
  buttons, Accept/Reject, calling the new endpoints and re-rendering the list on success.
- Rejected entries: per the manifesto, don't delete — just don't show them in the default Notes
  list once rejected (filter `entry.author === "rejected"` out client-side, same as Round 1 already
  does implicitly by only ever showing what the API returns — if the API still returns rejected
  entries, filter them here).
- Board switcher: no "AI suggested" marker on boards, ever — see the Tier policy's Board exception
  above. Do not add one, do not add an `Author` field to `planningstore.Board` to support one.
- Post-turn refresh: wire "Post-turn refresh" above into `handleChatEvent`'s `"done"` case.

## Acceptance / how to verify

- `go build ./...` and `go test ./...` pass, including new tests for the three tools and the
  accept/reject endpoints (mirror `termserver/planning_test.go`'s style), plus these two required
  cases:
  1. **Context clears on leaving Planning**: a session with Planning context set, then a message
     sent with `planning_id: ""`/`board_id: ""`, then a planning tool call in that same session →
     must fail with the "not in a Planning board" error. Proves stale context can't silently persist
     into Home or a different board.
  2. **AI-created Knowledge round-trips correctly**: agent calls `planning_create_note`, then
     `planning_list_board` in the same or a later turn → the created entry is visible with
     `Author == AuthorAISuggested`. Proves the tool actually persists through `planningstore.Store`
     and reads back correctly, not just that `Execute` returns a success string.
- Manually: open a Planning Board, ask the chat "create a board called Auth Flow" → within the same
  turn's "done" event, the board switcher updates on its own (no manual reload) and the new board is
  indistinguishable from a human-created one (no badge). Ask it "add a note about X" with Notes
  already expanded → the entry appears with an "AI suggested" badge and Accept/Reject controls on
  the same turn, without re-opening the panel; rejecting it makes it disappear from the list.
- Ask the chat to "create a decision that we always use Postgres" → **the request must not result in
  a `decision` Knowledge entry, full stop.** Ideally the model explains in text that it can't create
  Decisions through Planning tools (ideally it doesn't even attempt the call, since
  `planning_create_note`'s `Definition().Description` should restrict `type` to
  note/research/question in the schema itself). But if it nevertheless calls `planning_create_note`
  with `type: "decision"` anyway, the tool must reject that input clearly (this is the actual
  property under test — don't rely on a specific model's reasoning to make this pass, verify the
  tool's own input validation rejects it directly, e.g. in the unit test for the tool).
- Open the Home chat (no Planning open) and confirm planning tool calls fail with the "not in a
  Planning board" error — Home chat behavior is otherwise unchanged. Then open a Planning Board,
  send one message, click "New chat" (which calls `undockChatToHome`), and confirm a planning tool
  call in that fresh Home session also fails — this is the context-clearing path exercised through
  the real UI action, not just the API test above.

## Explicitly not this round

If you find yourself building Decision/Principle creation, delete/edit tools, canvas positioning
for AI-created objects, or a generic "approve any AI action" framework — stop, that's a later round.
Flag it back instead of building it.
