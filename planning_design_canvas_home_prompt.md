This is a **design-critique round only** — same Claude↔Codex adversarial-review pattern already
used for `planning_design_round3_navigation_prompt.md` and the `M8_design_round*` files in this
repo. Nothing gets built yet. Your job: attack the direction below as hard as those rounds attacked
M8/Round 3 — find the undefined states, the places this contradicts what's already closed, the
places where "todo" is doing too much work. Write back a report + your own recommendation, not code.
A third session (separate Claude, separate prompt) builds whatever comes out of this round.

## What's closed and must not be re-litigated

- **`PLANNING_MANIFESTO.md`** — frozen product philosophy for Planning-as-a-concept: "Planning
  decide, Project ejecuta." "El humano siempre dirige." "Nada se pierde, todo evoluciona." "La
  pantalla por defecto está vacía." These principles are not up for debate — what *is* up for debate
  in this round is whether Planning stays a top-level section or becomes an object type living
  inside the surface described below (see "What the user is asking for now").
- **Round 1–3 of Planning navigation** (`build_prompt_PLANNING_ROUND1.md`, `ROUND2`, `ROUND3` — all
  implemented): `/api/plannings*` HTTP API, the atomic `(planning_id, board_id)` context machine
  (`resolvePlanningContext`,
  `termserver/chat.go`), the three navigation tools (`planning_open`,
  `planning_create_board_and_open`, `planning_create_planning_and_open` —
  `agenthost/planning_tools.go`, `agenthost/planning_navigate.go`), the `NavigateAction`/`navigate`
  SSE event that moves the browser (`client_id`/`turn_id`-scoped, post-turn delivery), Tier policy
  (Boards structural/no review, Knowledge from AI is `ai_suggested` pending accept/reject,
  Decision/Principle unreachable by any tool). **This whole mechanism assumes Planning is a distinct
  screen the browser navigates to.** Read the section below carefully — that assumption is exactly
  what's now in question.
- **Correction, verified against the actual code (an earlier draft of this doc got this wrong):**
  `planningstore/model.go` does **not** model visual objects. Its own package comment says so
  explicitly — "It intentionally does not implement the canvas/UI layer... Board here is just a name
  and an ordered list of Knowledge entries surfaced on it, not visual objects
  (frame/arrow/rectangle/...)." `Board` today is `{ID, Name, CreatedAt, UpdatedAt}` plus Knowledge
  referencing it via `BoardID`. There is no geometry, no composition, no object model to inherit —
  "Planning as a Canvas object type" starts from nothing on the persistence side, not from an
  existing visual-object store.
- **Current layout** (`termserver/assets/index.html`): sidebar izquierdo fijo (projects, chats,
  planning nav item) + `main.workspace` con el chat centrado a todo lo ancho como `#home-view`. The
  chat panel re-parents between Home's slot and a Board's slot (`dockChatIntoBoard`/
  `undockChatToHome`, `app.js`) without losing session identity.
- There is a **separate, not-yet-written session about átomos** (`atom_tool.go`,
  `atoms_decision_tool.go`, `atoms_decision_gate_test.go` already exist in this repo) that this round
  depends on for one specific piece (anchoring, below) but does not itself resolve. Flag dependencies
  on it; don't try to design the atom model here.

## What the user is asking for now

A conversation with the human (Yeyo) established the following. Quote is the actual framing he used
in Spanish, kept verbatim where it matters:

> "lo que quiero con exo es que la ia tenga el control de todo desde el chat" — not scoped to
> Planning. The chat becomes the control surface for the whole app, not just for planeación.

Concretely, decided in conversation (treat these as fixed inputs to this round, not open questions):

1. **The Canvas is the new home-view, permanently — not modal, not conditional.** Layout: left
   sidebar (existing, unchanged) / canvas center / chat right. This *replaces* today's
   `#home-view` (chat centered, full width).
2. **Planning becomes an object type living inside the Canvas**, not a separate top-level section —
   this was confirmed explicitly after I raised the tension with Round 1–3's navigation model (see
   "The load-bearing tension" below). Other object types, today and future: diagram (first one to
   build), image, text, "archivos de planeación" (i.e. Planning's own Boards/Knowledge, now nested),
   music, "aprendizajes." No fixed final list — object type is meant to be an open/extensible
   category, not a hardcoded enum with five members.
3. **User flow for creating an object**: user discusses/plans an object with the AI in chat first
   (nothing appears yet — pure conversation, draft phase); user then explicitly says to
   materialize/plasmar it; only then does the object appear in the Canvas. Two distinct phases, not
   one.
4. **Detection of "materialize now" intent** — user's own words: could be slash-commands, buttons, or
   telling the AI in natural language ("the problem with just NL is there's a lot of words it'd have
   to track so it doesn't get confused"), or some combination of the three. **User explicitly wants
   this dug into deeper in this round** — it's the least resolved of the decided items, not a closed
   decision.
5. **Anchoring** — user's own proposed mechanism: each canvas object is backed by a set of átomos
   that the AI must always read for that session/context. **Depends on the not-yet-written átomos
   session** — do not design the atom model here, but do work out the *contract* this round needs
   from that system (what does "the AI always reads this object's atoms" require operationally: read
   on every turn? read on demand via a tool? something else?).
6. **Editing an already-materialized object is dual-mode**: clicking it opens a floating panel with
   (a) a manual editing module and (b) its own embedded mini-chat scoped to that object, so the human
   can edit by hand or by talking to the AI about that specific object.
7. **Render engine is undecided.** Constraint that must drive the choice: the *same* object must be
   editable both by a human (via the manual module above) and by the AI (via chat) — whatever format
   is chosen has to be mutable from both sides without the two edit paths fighting each other or
   needing a lossy round-trip. Freeform pixel/vector drawing (Excalidraw-style) vs. a structured
   declarative format (JSON/DSL an AI can generate and a UI can render+diff, closer to how the
   existing Board's `frame`/`arrow`/`rectangle` objects are already modeled in `planningstore`) is
   the live tradeoff — no decision made yet.
8. **Persistence**: "session" as a concept goes away at the top level — **one project = one
   continuous unit of work**, and everything in its Canvas persists for the life of the project.
   Internally, sub-sessions get created when context fills up, anchored to the project. **User has
   explicitly deferred this — do not design it in this round.** It only matters here insofar as it
   confirms objects are project-scoped, not chat-session-scoped.
9. **Object lifecycle**: each object supports delete / deactivate / activate. (Deactivate ≠ delete —
   presumably an inactive object stops being an anchor/context source without losing its data, but
   this wasn't specified further — worth digging into.)
10. **Relation to existing agent tools** (`atom_tool`, `scale_catalog`, `planning_navigate` family) —
    explicitly left open by the user for this round to dig into.

## The load-bearing tension — dig into this first

Round 3 built a real, working mechanism: the model calls a tool, a `NavigateAction` gets queued, an
SSE `navigate` event tells a specific browser tab to move screens (`openPlanning`/`switchBoard`).
That whole mechanism exists because Planning used to be **a place you navigate to** — a distinct
screen, entered and left.

If Planning becomes **an object type inside the Canvas** (decision #2 above), "opening a Planning"
stops being screen navigation and becomes something more like "this object becomes the active
anchor" or "this object's floating edit panel opens" (decision #6's mechanism) — the human never
leaves the Canvas at all. That's a different interaction shape than Round 3 was built for.

Questions to actually resolve, not just note:

- Does Round 3's `NavigateAction`/`navigate` SSE mechanism get **replaced** by whatever mechanism
  opens/focuses a Canvas object, or does it **compose** with it (e.g. Canvas objects use a new,
  narrower "focus this object" event, and the old screen-navigation code path is deleted since there
  are no longer other screens to navigate to)?
- `planning_open`/`planning_create_board_and_open`/`planning_create_planning_and_open`'s entire
  contract (exact-match-only lookup, explicit-naming check against the human's actual message, at
  most one navigation per turn) was designed for "pick an existing screen." Does that contract
  transfer cleanly to "pick/create an existing Canvas object," or does object-creation-via-chat
  (decision #3/#4 above) need its own, differently-shaped tool family instead of inheriting this one?
- Is there still a reason for a `planning-nav-item` in the left sidebar at all once Planning is just
  one object type among several, or does the sidebar's job shrink to project/chat list only, with
  *everything* project-specific living in that project's Canvas?

## What I want back

A written report, same shape as the Round 3 critique document produced: where the decided items
above (1–10) actually break or underspecify once you push on them, and — this is the important part
— **your own recommendation for the load-bearing tension** and for the two items the user explicitly
flagged as unresolved (#4 intent detection, #7 render engine), plus how #10 (relation to existing
agent tools) should actually shake out. Don't just list more open questions back — argue a position
for each, the way Round 3's critique did, so the next (build) session has something concrete to
start from. If the whole "Canvas replaces home, Planning becomes an object type" direction has a
cleaner alternative shape, say so and argue it.
