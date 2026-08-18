You are building Round 3 of `exo`'s Planning section: letting the chat navigate the app to a
Planning/Board on the human's explicit instruction — "abre Auth", "en Exo crea un board Backend" —
without ever letting the agent decide *for itself* what the human should be looking at. Go +
frontend, real build task. This round does not touch Round 1/2's HTTP API, screens, or Knowledge
Tier policy except where explicitly noted.

## The one architectural rule everything else in this document follows

> **Navigation does not establish agent Planning context. The browser does.**

The agent can ask the UI to navigate somewhere. It cannot, by doing so, make `Host.planningContext`
or the chat session's persisted context become that destination. The new context only exists once
the browser is actually showing that destination and sends the *next* chat request carrying its
real `planning_id`/`board_id` — through the exact same `resolvePlanningContext` state machine
Round 2 already built, completely unmodified. This preserves Round 2's core guarantee: **the agent
only ever acts on the Planning/Board the human is currently looking at.**

Concretely: `planning_open`/`planning_create_board_and_open`/`planning_create_planning_and_open —
see below — never call `Host.SetPlanningContext`. They only ever queue a `NavigateAction` for the
frontend to act on. A turn that both navigates *and* tries to create Knowledge in the same message
("abre Auth y agregá una nota") opens Auth and tells the user it's now there — it does **not**
create the note in the same turn. That happens on the next message, once the browser has actually
moved and sends real context.

## What's closed and must not be re-litigated

- `PLANNING_MANIFESTO.md`, Round 1 (`build_prompt_PLANNING_ROUND1.md`), Round 2
  (`build_prompt_PLANNING_ROUND2.md`, implemented) — `resolvePlanningContext`'s atomic state
  machine (`termserver/chat.go`), `Host.SetPlanningContext`/`planningContext`
  (`agenthost/planning_context.go`), the three existing tools (`agenthost/planning_tools.go`), the
  Tier policy (Boards are structural/no review; Knowledge from AI is `ai_suggested` pending
  accept/reject; Decision/Principle unreachable by any tool). None of this changes.
- `Server.agentMu` (`termserver/chat.go`) still serializes every turn end-to-end on one `Host` —
  the same guarantee Round 2's concurrency check relied on.

## New tools — three, not one with a boolean

A single tool with a `create_if_missing` boolean means a misfire (model sets the flag wrong) is
silently the *wrong behavior inside an apparently-correct call*. Three distinctly-named tools mean
a misfire is *the wrong tool name in the transcript* — far more auditable, and each one gets a
tight, non-overlapping contract:

- **`planning_open`** — `{ planning_name, board_name? }`. Never creates anything. Fails clearly if
  the Planning (or, when given, the Board) doesn't exist.
- **`planning_create_board_and_open`** — `{ planning_name, board_name }`. The Planning must already
  exist (fails clearly if not — this tool does not create Plannings). Creates the Board directly
  (consistent with Round 2: Boards are structural, no review step) and queues navigation to it.
- **`planning_create_planning_and_open`** — `{ planning_name, initial_board_name? }`. The only tool
  that can create a Planning. Without `initial_board_name`, navigation lands on the new Planning in
  its planning-only state (no Board open) — exactly the state Round 2 already extended
  `resolvePlanningContext` to support. **`planning_open`'s own board-less behavior is the same rule,
  stated once here:** given a Planning with no `board_name`, it always lands planning-only — never
  infers "the only Board" or "the most recently used one," even when that would be unambiguous.
  Write this down explicitly in the tool's code comment, because it's exactly the kind of
  "helpful" shortcut someone adds later without realizing it reopens an unnamed-destination choice
  the human never made.

**Optional name arguments are an invariant of the tool, not a best-effort validation.** If the model
supplies `initial_board_name` (or any future optional name argument on these tools) and that string
doesn't pass the explicit-naming check below, the call fails — the tool does not silently drop the
argument and proceed as if it had been omitted. Dropping it would let the model "helpfully complete"
an intention the human didn't state; failing loudly is what actually enforces "the agent never
invents a destination."

### Exact matching only — fuzzy is a suggestion, never an execution

Name lookup is exact, case-insensitive (reasonable whitespace normalization, nothing fancier). No
tool ever executes against a fuzzy/best-guess match:

- Zero exact matches on `planning_open`/`planning_create_board_and_open`'s required-to-exist target
  → clear error. The tool *may* mention a close name if one exists ("no board named 'Aut' — did you
  mean 'Auth'?") purely as text back to the model to relay to the user; it must not open it.
- More than one exact match (e.g. two Plannings named "Exo" with different casing colliding after
  normalization) → clear "ambiguous" error, nothing opens.
- Creation tools also refuse to create a second Planning/Board that exact-matches an existing name
  — don't let the agentic surface make duplicate-name collisions worse than they already can be.
  (This is a check inside these tools, not a global uniqueness constraint added to
  `planningstore` — don't touch the model for this.)

### Explicit naming must be checked, not just instructed

The manifesto-level rule "the human names the target, the agent never invents one" is currently
just a sentence in a tool description — nothing stops a model from calling
`planning_create_board_and_open(board_name="Authentication Architecture")` when the human only said
"quiero pensar sobre auth." Make it a real check: the tool call handling has access to the current
turn's human message text; before acting, verify (normalized, substring-tolerant — not full NLP)
that `planning_name` (and `board_name`/`initial_board_name` when present) actually appear in that
message. If a name doesn't appear in what the human just typed, refuse with a clear error ("that
name wasn't in your message — say the name you want explicitly") instead of executing. This is a
best-effort guard, not a formal proof — document it as such in the code comment, and note if it
turns out too strict in practice it can be relaxed later, but ship it checking something.

## Turn-scoped `NavigateAction`, not a runner side-channel

Do **not** have `backend.go`'s runner closure "detect which tool ran" by inspecting text, and do
**not** use a sentinel string in the tool's return value (e.g. `__EXO_NAVIGATE__:...`) — both are
instant technical debt. Instead:

- Introduce a small turn-scoped structure the three navigation tools write into and the chat
  handling layer reads after the turn completes — conceptually a `TurnContext` carrying at most one
  `NavigateAction`, threaded the same way `planningContext` is threaded into the tools (a shared
  pointer constructed per-turn, not global mutable `Host` state this time, since — unlike
  `planningContext`, which represents "where is this turn scoped," `NavigateAction` represents "what
  did this turn *produce*," and should not leak into the next turn the way a forgotten
  `SetPlanningContext` reset would).
- Fields: `ClientID`, `SessionID`, `TurnID`, `PlanningID`, `PlanningName`, `BoardID` (optional),
  `BoardName` (optional).
- **At most one `NavigateAction` per turn.** If a second navigation tool call succeeds after the
  first already queued one, refuse it: "A navigation target has already been selected for this
  turn." The model doesn't get to change its mind mid-turn about where to send the user.
- This is deliberately narrow — a typed carrier for exactly this one turn output, not a general
  "arbitrary UI actions" framework. Future UI-affecting tools can extend the same shape later if the
  need actually shows up; don't speculatively generalize it now.

### Committed vs. delivered — these are two different moments, keep them separate

A `NavigateAction` is **committed** the instant its tool call returns success — that's when the
human's intent was actually carried out (the Board/Planning exists now, or the open-target was
successfully resolved), and nothing that happens afterward in the turn can retroactively make it
not have happened. It is **delivered** — sent to the frontend — whenever the turn reaches a
terminal state, success or error. Keeping these separate matters even for `planning_open`, which
mutates nothing: if the tool resolved the target successfully and then the LLM connection fails
before the turn produces a normal `"done"`, the navigation still committed and must still be
delivered — don't let a later, unrelated failure decide whether an already-successful tool call
"counts." Concretely: commit by writing into the turn's `NavigateAction` slot the moment `Execute`
returns success; deliver by flushing that slot's contents (if set) whenever the turn's terminal
state is reached, whatever that state is.

## Per-tab identity: `client_id` and `turn_id`

`chatBroadcaster` (`termserver/chat.go`) broadcasts every SSE event to every connected
`/api/chat/stream` client — fine for `idle`/`busy`/`output`, **not** fine for `navigate`: two tabs
open on the same session must not both get yanked to a new Board because one of them asked to go
there.

- Frontend generates a `client_id` once per tab, stored in `sessionStorage` (not `localStorage` —
  must not survive across tabs). Sent on every `/api/chat` request alongside `message`.
- **`turn_id` is generated by the frontend, per submit — not derived server-side from message index,
  session state, or anything else.** Two tabs (or two rapid submits) can produce overlapping
  frontend-side turns even though `agentMu` serializes their execution in the backend; only the
  frontend actually knows which submit a given response belongs to. The chat submit handler
  generates a fresh `turn_id` alongside reading `client_id`, sends both on the `/api/chat` request,
  and the server treats `turn_id` purely as an opaque correlation token — it does not invent, reuse,
  or reinterpret it, only carries it through into the `NavigateAction` and back out on the
  `navigate` SSE event.
- The `navigate` SSE event carries `client_id`, `session_id`, `turn_id` alongside the navigation
  target. **Every tab's frontend ignores `navigate` events whose `client_id` doesn't match its own**
  — cheap, no broadcaster refactor needed this round; server-side per-client routing is a fair
  future improvement, not required now.
- Frontend also tracks the `turn_id` it generated for its own most recent in-flight submit and
  ignores a `navigate` event carrying any other `turn_id` (e.g. arriving late after a reconnect,
  after the user already moved on) — a stale navigation must not yank the screen backward.

## Frontend: applying the navigation

- New SSE case in `handleChatEvent` (`termserver/assets/app.js`): `case "navigate":` — after the
  `client_id`/`turn_id` checks above pass, call the existing `openPlanning(planningId)` (Round 1),
  then select `boardId` if present via the existing `switchBoard`/board-select machinery — no new
  navigation code, this reuses what's already there.
- Applied **post-turn** (on the terminal-state delivery described above), not mid-turn — the screen
  should move once, smoothly, after the agent's turn concludes (ideally after it's said something
  like "abriendo Auth"), not jump out from under the user while text is still streaming in.
- The chat session (`state.activeChatSessionId`) is untouched by this — `#chat-panel` re-parenting
  between Home and a Board's slot already preserves session identity (Round 1); navigating via this
  new path is no different from a user manually clicking into a Board.
- Do **not** send real `planning_id`/`board_id` on the turn that triggered the navigation itself —
  that request still reflects wherever the user actually was when they hit send (Home: `""`/`""`).
  The *next* message, sent after the frontend has visibly navigated, is what carries the real IDs
  into `resolvePlanningContext` — exactly as it does today for a manual click-through.
- **Landing on a planning-only Planning (`planning_open("Exo")` with no Board) must produce a
  frontend state distinguishable from Home, not collapse into it.** Concretely: `state.activePlanning`
  stays set to the opened Planning, `state.activeBoardId` stays `null`/unset — the chat submit
  handler's existing `inPlanning`/`inBoard` distinction (added when Round 2 extended
  `resolvePlanningContext`) already encodes this correctly *if* `activePlanning` is actually set by
  this new navigation path. Double-check `undockChatToHome()` isn't called (or anything else that
  clears `state.activePlanning`) as part of applying a `navigate` event that lands planning-only —
  it's easy to accidentally route through a helper that clears both fields together and silently
  turn "Planning, no Board" back into indistinguishable-from-Home. Add a frontend test/manual check
  specifically for this: navigate to a Planning with no Board, send the next message, confirm the
  request body is `planning_id=<id>, board_id=""` — not `""`/`""`. Getting this wrong quietly breaks
  the exact bootstrap-the-first-Board flow Round 2 was extended to support.

## Explicitly out of scope this round

- No new Tier for navigation. It's a different axis from Knowledge's `AuthorState` machine — don't
  invent "Tier 1.5." The rule instead: a navigation only ever fires when the human explicitly asked
  for it in the current turn (enforced by the explicit-naming check above). The agent may *suggest*
  navigating somewhere unprompted ("Backend Architecture parece relevante, ¿querés que lo abra?") —
  as text, never as an executed tool call.
- No fuzzy-match auto-navigation, ever — fuzzy is suggestion-only, per above.
- No server-side per-client SSE routing (the `client_id`-filter-on-the-frontend approach is enough
  for this round).
- No changes to `planningstore`'s uniqueness model, Round 2's Tier policy, or the HTTP `/api/plannings*`
  surface.

## Acceptance / how to verify

- `go build ./...`, `go test ./...` pass, plus new tests:
  - `planning_open`/`planning_create_board_and_open`/`planning_create_planning_and_open` each tested
    for: exact match succeeds, zero match fails, ambiguous match fails, creation-tools refuse
    duplicate names, explicit-naming check rejects a name absent from the human's message.
  - A turn that queues two navigations (simulate two successful tool calls in one turn) — second
    one refused, first one still delivered.
  - Committed-vs-delivered: simulate a tool succeeding (commit) then the turn erroring before
    `"done"` — `NavigateAction` still delivered on that terminal error state, not dropped.
  - Round 2 regression: a turn that navigates does **not** call `Host.SetPlanningContext`, and the
    session's persisted `planning_id`/`board_id` are unchanged immediately after that turn.
  - `initial_board_name` (or any optional name arg) present in the tool call but absent from the
    human's message → call fails outright, does not proceed with the name dropped.
  - Frontend: after landing planning-only (`planning_open` with no board), the next chat submission's
    body is `planning_id=<id>, board_id=""` — explicitly not `""`/`""`.
- Manually: two browser tabs on the same chat session; ask one to "abre Auth" — only that tab
  navigates, the other stays put. Ask from Home "en Exo crea un board Backend" (Exo exists) — Board
  created, tab navigates there, then send a follow-up message and confirm *that* message is what
  carries the real `planning_id`/`board_id` (check via the same session-inspection approach Round 2's
  tests used). Ask "créame un board Auth" with no Planning named — refused, agent asks which
  Planning. Ask "crea un planning llamado Ecommerce" — created, lands in planning-only state, no
  Board invented. Ask "abre Exo" on a Planning that has exactly one Board — still lands planning-only,
  does not auto-open that one Board.
