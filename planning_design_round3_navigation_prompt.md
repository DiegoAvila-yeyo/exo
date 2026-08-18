This is a **design-critique round only** — same Claude↔Codex adversarial-review pattern already
used for `build_prompt_PLANNING_ROUND2.md` (closed, implemented) and the `M8_design_round*` files
in this repo. Nothing gets built yet. Your job: attack the draft below as hard as those rounds
attacked M8 — find the races, the undefined states, the places where this disagrees with what's
already closed. Write back a report + your own recommendation, not code.

## What's closed and must not be re-litigated

- **`PLANNING_MANIFESTO.md`** — frozen product philosophy. The rule this round leans on hardest:
  *"El humano siempre dirige. La IA organiza, sugiere y conecta. Nunca es dueña del proyecto."*
  Also: *"La pantalla por defecto está vacía"* — the UI never surfaces things the user didn't ask
  for.
- **Round 1** (`build_prompt_PLANNING_ROUND1.md`) — `/api/plannings*` HTTP API, Planning
  list/Board screens, `#chat-panel` re-parented between Home and a Board's slot
  (`dockChatIntoBoard`/`undockChatToHome` in `termserver/assets/app.js`), one SSE stream
  (`/api/chat/stream`) broadcasting to every connected client.
- **Round 2** (`build_prompt_PLANNING_ROUND2.md`, implemented) — `planning_list_board`,
  `planning_create_board`, `planning_create_note` agent tools (`agenthost/planning_tools.go`); a
  Host-level `planningContext` (`agenthost/planning_context.go`) scoped per turn via
  `Host.SetPlanningContext`, verified safe as mutable state only because `Server.agentMu`
  (`termserver/chat.go`) serializes every turn end-to-end — no two turns run concurrently on one
  `Host`; the atomic `(planning_id, board_id)` state machine in `resolvePlanningContext`
  (`termserver/chat.go`), just extended past the original Round 2 shipped version to also allow a
  **planning-only** state (`planning_id` set, `board_id` explicitly `""`) so `planning_create_board`
  can bootstrap a Planning's first Board without already being inside one. Decision/Principle
  creation is explicitly out of reach of every agent tool — no workaround exists.
- **The whole point of the atomic context rule**: the chat only acts on the Planning/Board the user
  is *currently looking at* — `planning_id`/`board_id` are sent by `app.js`, not decided by the
  model, specifically so the model can never write into a Planning the user isn't watching.

## What the user is asking for now

Today: the agent can only create a Board/Note while the user has already manually navigated into
that Planning (or at least a planning-only state) via the sidebar/UI. The user wants this instead:

> Desde Home (`New chat`), le pido "creá un board llamado X" (o "metete al board Y") y la propia
> app navega sola a Planning, crea (o abre) lo que pedí, y me deja parado ahí — todo dentro de la
> misma sesión de chat, que se mueve de pantalla en pantalla con el usuario.

Explicit constraint from the user, which is the one thing keeping this inside the manifesto's
"human directs" rule rather than breaking it: **the human names the target in natural language —
"tal board", "con el nombre que pido"** — the agent never invents a destination or a name on its
own. If the human says "abrí el board Auth", it must already exist or the agent says so; if the
human says "creá un board llamado Auth", it creates exactly that name. The agent is executing a
named instruction, not deciding where to take the user.

## Draft design (mine — attack it)

### 1. One new tool: `planning_navigate`

```
Input:  { planning_name: string (required), board_name: string (optional),
          create_if_missing: bool (required) }
```

- `create_if_missing` is something the model sets per turn based on the human's phrasing ("créame
  un board..." → true; "abrí el board..." / "metete al board..." → false) — not a fixed tool
  config. Whether that's the right split (one tool, model-derived flag) vs. two separate tools
  (`planning_open` / `planning_create_and_open`) is an open question below.
- Planning/Board lookup is by exact, case-insensitive name match within the tools' reach (all
  Plannings for the Planning name; that Planning's Boards for the board name).
- Ambiguous match (two Plannings named the same) → tool returns a clear error, doesn't guess.
  Zero match with `create_if_missing: false` → clear error, doesn't create anyway.
- Zero match with `create_if_missing: true` → creates it (reuses the exact same
  `planningstore.Store` calls `planning_create_board`/`Store.Create` already use — no duplicated
  domain logic).
- This tool has **no existing Planning-context gate** — it's the one tool allowed to run from Home
  (`planning_id`/`board_id` both `""`), since its entire job is to establish that context. Every
  other planning_* tool keeps requiring context exactly as Round 2 left it.

### 2. Getting the result from the agent process to the browser tab

No existing mechanism carries a structured instruction from a tool call to the frontend — SSE today
only has `idle`/`busy`/`output`/`approval`/`done` (`termserver/chat.go`'s `chatStateEvent`/
`chatOutputEvent`/etc.). Draft: a new SSE event, `navigate`:

```json
{"type": "navigate", "planning_id": "...", "planning_name": "...", "board_id": "...", "board_name": "..."}
```

`planning_navigate`'s `Execute` doesn't emit this directly (tools only return a string to the
model) — the *runner* (`backend.go`'s closure) would need to detect that this specific tool ran
this turn and push the event, OR the tool result itself carries a sentinel the runner inspects. See
open question below — this is the least-worked-out part of the draft.

`app.js`'s `handleChatEvent` gets a new `case "navigate":` that calls (something like)
`openPlanning(event.planning_id)` then, if `event.board_id` is set, selects that board — reusing
Round 1's existing `openPlanning`/`switchBoard`, not new navigation code.

### 3. Session continuity

`state.activeChatSessionId` already survives dock/undock (Round 1) — the chat session itself
doesn't change identity when `#chat-panel` moves between Home's slot and a Board's slot, so "same
session throughout" is close to free. The open question is *when* the move happens relative to the
rest of the turn (see below).

## Open questions — this is what I want your report to actually dig into

1. **Where does the `navigate` SSE event actually originate?** The runner closure in `backend.go`
   doesn't currently see which tools ran, only the final text/history. Does it need to? Should
   `Host.Run` or the tool itself get a hook to signal "a navigation happened this turn," and if so
   what's the cleanest shape that doesn't turn into a second ad-hoc side-channel next to the
   existing `ChatOutputWriter`?
2. **One global SSE broadcaster, multiple tabs.** `chatBroadcaster` broadcasts to every connected
   `/api/chat/stream` client, not per-session. If the user has exo open in two tabs, does a
   `navigate` event yank *both* tabs into the new Board, even the one that was doing something
   else entirely? Round 1/2 never had to answer this because nothing before now caused
   *involuntary* client-side navigation from server data — this is new.
3. **Mid-turn vs. post-turn.** Round 2's board-creation refresh (`refreshBoardScreenAfterTurn`) only
   runs after `"done"`. Should `navigate` fire the moment the tool executes (agent still typing),
   or wait for turn completion like everything else does today? Firing mid-turn means the user
   watches their screen move while the agent keeps talking — is that the intended feel, or jarring?
4. **One tool with a model-derived `create_if_missing` bool, or two tools?** A wrong bool (model
   sets `true` when the human only asked to open something, or vice versa) is a silent
   create-vs-fail mistake with no human confirmation step — is a boolean the model infers really
   safe enough, or does the ambiguity argue for two distinctly-named tools so a misfire is at least
   a clearly wrong tool name in the transcript, not a flipped flag?
5. **Ambiguous name resolution.** Exact case-insensitive match was the draft's easy answer — is
   that actually enough, or does it need fuzzy matching (and if so, how does that interact with
   "never guess the destination")?
6. **Does this need its own Tier, above Round 2's?** Round 2's Tier policy never had to consider
   "the AI changes what's on your screen." Is `create_if_missing: true` from Home effectively
   creating a whole new Board (and possibly Planning) with zero human review — is that consistent
   with Round 2's "Boards are structural, no review needed" call, or does *first Board of a
   brand-new Planning, initiated from Home* deserve a confirmation step Round 2 never needed
   because the user was always already inside the Planning when it fired?
7. **What about creating a brand-new Planning entirely from Home**, not just a Board inside an
   existing one? The user's ask implies "créame un board" should work even with zero Plannings in
   the workspace — does `planning_navigate` also need `create_if_missing` to extend to the Planning
   itself, not just the Board? If so, an unnamed/default-named Planning is exactly the kind of
   silent-invention the manifesto forbids — does this require the human to always name the Planning
   explicitly too ("en el planning X, creá un board Y"), even the first time?

## What I want back

A written report: where this draft breaks, what's underspecified, and your own recommendation for
each open question above — not an implementation. If you think the whole `planning_navigate`
approach is wrong and there's a cleaner shape, say so and argue it, same as the M8 rounds did when
a draft's core idea itself was the problem.
