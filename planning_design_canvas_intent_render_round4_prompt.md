Round 4 of the Canvas-home design critique. Same pattern as Rounds 1–3
(`planning_design_canvas_home_prompt.md`, `_round2_prompt.md`, `_object_model_round3_prompt.md`) plus
the parallel atoms round (`planning_design_atoms_canvas_anchor_prompt.md`) — design recommendation,
nothing gets built. This round closes the two items flagged as unresolved since Round 1 and never
picked up since: **intent detection** for draft→materialize, and **render engine** choice.

## What's closed from Rounds 1–3 — accepted, don't re-litigate

- Round 1: screen-based nav end-to-end, chat-session-scoped context vs. project-scoped Canvas, no
  draft→materialize state today, single-turn-only `nameMentioned` guard, no lifecycle infra, atoms
  today are pull/opt-in, `scale_catalog*` is research not runtime, manifesto conceptually breaks once
  Planning is a Canvas object type.
- Round 2: anchoring architecture — `Canvas object model → object_id → activeObjectSet →
  dynamicCentro`. `planningContext` untouched, stays legacy-Planning-tool scope only. Anchor atoms
  key off `object_id`. "Siempre" = per-turn while active. Edits version via `supersedes`. Active set
  small/human-curated.
- Round 3 — **this round's concrete foundation**: `canvasstore`, new, project-scoped, one JSON file
  per project-canvas (holds `objects[]` + `active_object_ids[]` together, not one-file-per-object).
  Generic envelope: `object_id`, `project_id`, `type` (open string vocabulary), two independent
  lifecycle axes — `phase = draft | materialized | deleted` and `activation = active | inactive`
  (only meaningful once `phase == materialized`) — timestamps, `payload` (inline, no per-type
  sub-stores yet), `anchor_atom_ids`. Planning enters by **composition** (a `type: "planning"` object
  wrapping an existing `planning_id`), not absorption — `planningstore` stays untouched, `planning_*`
  navigation tools remain legacy-compatible. Concurrency: manual-edit path (floating panel) writes
  outside `agentMu`'s turn-serialization entirely — new problem, resolved with optimistic concurrency
  (CAS on `ProjectCanvas.version`/`updated_at`) at the aggregate level, no distributed locks.

## Question 1: Intent detection for draft → materialize

From Round 1, decision #3/#4 (Yeyo's own framing, kept verbatim): the flow is discuss-first
(nothing appears), then an explicit "materialize it" moment where the object actually gets created in
`canvasstore` with `phase: materialized`. His own words on the trigger: could be slash-commands,
buttons, natural language, or some combination — with an explicit worry that pure NL means "hay
muchas palabras y [la IA] debería estar al tanto de cada una para que no se le confunda."

Ground this against what Round 1 also found: `nameMentioned()` (Round 3-of-navigation's explicit-
naming guard) only checks the *current turn's* message — no multi-turn intent tracking exists
anywhere in the closed system today. A "we've been discussing this diagram for 20 messages, now
build it" trigger cannot reuse that guard as-is.

Resolve, with a real recommendation:

1. Is this NL-only, command-only, button-only, or hybrid — and why? If hybrid, what's each
   channel's actual job (e.g. NL for casual "hazlo ya", a slash command as the unambiguous fallback
   when NL detection is uncertain, a button that only appears once the AI itself signals "esto está
   listo para materializarse")?
2. Where does the "this looks materializable" signal originate — does the model decide per-turn via
   a dedicated tool call (`canvas_materialize_object` the model invokes when it judges the human's
   intent is clear), the same way today's `planning_create_board_and_open` fires on an explicit
   instruction? Or does detection need to happen outside model-tool-call space entirely (e.g. a
   lighter classifier/heuristic on the human's message, separate from the main agent loop) because a
   tool-call-only approach inherits the same "model has to correctly infer intent from one message"
   fragility the atoms round already found and rejected for anchoring?
3. What happens on a **draft** object concretely, state-wise, before materialization? Does discussing
   a diagram create a `phase: draft` `canvasstore` object immediately (so there's something concrete
   the "materialize" trigger flips to `materialized`), or does draft-phase conversation produce
   nothing in `canvasstore` at all until the materialize moment, at which point the object is created
   directly in `phase: materialized`? This decides whether "draft" is really a stored state or just a
   name for "hasn't been created yet" — Round 3 modeled `phase: draft` as a real enum value, which
   implies the former, but nothing has confirmed a draft object actually gets written before
   materialization, or by what trigger.
4. Multiple candidate objects in flight — if the human is discussing two different future diagrams in
   the same conversation before materializing either, does intent detection need to disambiguate
   *which* draft the human means when they say "ya, hazlo"? Does this need every draft to be
   explicitly named the moment discussion starts (same explicit-naming discipline Round 3-of-
   navigation already enforces for Planning/Board names), or is single-draft-at-a-time an acceptable
   v1 constraint?

## Question 2: Render engine

From Round 1, decision #7 (Yeyo's own framing): the same object must be editable by a human via a
manual module (floating panel) *and* by the AI via its embedded mini-chat — whatever format is chosen
has to be mutable from both sides without the two edit paths fighting or needing a lossy round-trip.
The live tradeoff named then: freeform pixel/vector drawing (Excalidraw-style) vs. a structured
declarative format (JSON/DSL an AI can generate and a UI can render+diff).

Round 3 now gives this a concrete home to evaluate against: `canvasstore`'s generic envelope has an
inline `payload` field, JSON, versioned via the same aggregate the object lives in. Resolve:

1. Given `payload` is already JSON living inside a JSON-persisted aggregate, does a structured/
   declarative rendering format (the AI emits/edits a JSON or DSL description, the frontend renders
   it deterministically, e.g. something mermaid-like or a constrained shape-graph) fit this
   substantially better than freeform drawing would, or is that too quick a conclusion — what would
   freeform (raw SVG/canvas strokes, or an Excalidraw-style scene graph) actually cost here that
   structured doesn't?
2. **Concurrent dual-edit, concretely**: if the human is mid-edit in the manual module (say, dragging
   a box) at the same moment the AI's mini-chat produces an edit from a chat instruction, Round 3's
   optimistic-concurrency/CAS answer means one of the two writes gets rejected and has to retry. Is
   that an acceptable UX for this (the rejected side just re-reads and reapplies), or does the render
   format choice itself need to make concurrent edits mergeable at a finer grain (e.g. per-shape
   diffs) rather than whole-`payload` CAS? This is the first place Round 3's aggregate-level
   concurrency answer gets stress-tested by an actual interaction, not just a design principle — say
   whether it holds up.
3. Does the render format need to be type-specific from the start (diagrams get one schema, a future
   "text" object type gets a completely different one), or is there a shared minimal shape-language
   worth defining once now, given `type` is meant to stay an open vocabulary and re-solving "how does
   a human and an AI both edit this" per new object type later would be expensive to redo each time?

## What I want back

A concrete recommendation for both questions, same as before — not a menu. If the two questions
turn out to be more coupled than they look (e.g. the render format choice constrains what "draft"
can even mean, since a draft might need to hold a partial/invalid version of that format), say so
explicitly. This is intended to be the last open-design round before consolidating into a build
prompt — flag anything you think still can't responsibly be handed to a build session after this
round closes.
