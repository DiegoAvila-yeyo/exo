Round 3 of the Canvas-home design critique. Same pattern as Rounds 1–2
(`planning_design_canvas_home_prompt.md`, `planning_design_canvas_home_round2_prompt.md`) and the
parallel atoms round (`planning_design_atoms_canvas_anchor_prompt.md`) — design-critique/design-
proposal only, nothing gets built. This round exists because your own Round 2 report named the real
blocker: the anchoring architecture is coherent on paper but has nowhere to attach — there is no
Canvas object model yet, so there is no real `object_id` for anchor atoms to point at.

## What's closed from Rounds 1–2 — accepted, don't re-litigate

- Round 1 findings (all 10, confirmed by you): screen-based navigation end-to-end, chat-session-
  scoped context vs. project-scoped Canvas, no draft→materialize state, single-turn-only intent
  guard (`nameMentioned`), no activate/deactivate lifecycle anywhere, atoms today are pull/opt-in/
  task-level, `scale_catalog*` is research not runtime, manifesto's `Workspace→Planning→Board`
  conceptually breaks once Planning is a Canvas object type. `planningstore/model.go` has **no**
  visual-object model — `Board` is `{ID, Name, timestamps}` + Knowledge references only.
- Round 2's converged anchoring architecture — **this is the design this round has to build the
  missing foundation for**:
  ```
  Canvas object model (this round's job) → object_id
          ↓
  activeObjectSet — new, explicit, per-project/visual-session state — NOT folded into planningContext
          ↓
  dynamicCentro(activeObjectSet) — resolves active objects' anchor atoms, injects at prompt-build
          time, no tool call, same delivery path as today's static centro
  ```
  Explicitly settled in Round 2, do not reopen: `planningContext` stays exactly as it is today
  (scope for legacy Planning tools only, still gated by `Server.agentMu` serializing one turn at a
  time on a `Host`) — it does **not** become the anchoring mechanism. Anchor atoms carry `object_id`
  provenance, not `planning_id`/`board_id`. "Siempre" = per-turn while active, not project-lifetime;
  deactivating flips a flag in `activeObjectSet`, doesn't touch the atom. Edits version via
  `supersedes`, never mutate in place. Active set should stay small/human-curated — cost is real and
  unbounded activation is a plausible root cause of the deferred context-budget problem (Round 1
  item #8, still deferred, still not this round's job).

## What this round has to actually produce

A concrete design for the Canvas object model — the thing Round 2 confirmed doesn't exist and can't
be patched into `planningstore` as-is. Not a full implementation, but specific enough that
`activeObjectSet` and `dynamicCentro` from Round 2 have something real to reference, and specific
enough that the eventual build session isn't starting from a blank page here either.

Ground it in what's already decided about Canvas objects (from `planning_design_canvas_home_prompt.md`):
object types are open-ended (diagram first, then image/text/planning-file/music/aprendizajes/...),
each object has a draft phase (discussed, not yet real) and a materialized phase (exists in Canvas),
each materialized object supports delete/deactivate/activate, editing is dual-mode (manual panel +
per-object mini-chat), and Planning itself becomes one of these object types rather than a top-level
section — so whatever store this round proposes has to be able to hold "a Planning" as one instance
of an object, not just diagrams.

Questions to resolve with an actual recommendation, not just a list back:

1. **Where does this live relative to `planningstore` and to Project?** A new top-level package
   (`canvasstore`, analogous to `planningstore`/`chatstore`)? A layer that composes `planningstore`
   Plannings as one possible object payload alongside others? Justify against the existing
   one-JSON-file-per-aggregate pattern `planningstore`/`chatstore` already use — should Canvas
   objects follow that same persistence shape, or does this project-scoped, potentially-large,
   many-object-type surface need something different (e.g. one file per project holding all its
   objects, vs. one file per object)?
2. **Schema for the generic object envelope.** At minimum: `object_id`, `type` (open vocabulary, not
   a fixed enum given the "todo" future list), `project_id`, lifecycle state (draft / materialized /
   active / inactive / deleted — reconcile this against Round 1's separately-requested
   delete/deactivate/activate, is "draft" a 4th state or a separate axis?), timestamps, and a
   type-specific payload. Does the payload live inline in this envelope, or does each object type own
   its own sub-store (e.g. a future `diagramstore` for diagram content) referenced by `object_id` from
   the generic envelope? Argue one, don't just present both.
3. **How does Planning-as-object-type actually nest?** If a Planning becomes one Canvas object, does
   its `object_id` wrap an existing Planning's `planningstore` ID (composition, no duplication — a
   thin Canvas-object record pointing at `planning_id`), or does Planning's data get absorbed into the
   new store wholesale (migration, `planningstore` eventually retired)? Round 1 flagged this exact
   fork (finding: "does `planning_navigate`'s contract transfer, or does object-creation need its own
   tool family") — this round should give it a real answer given the object model you're proposing.
4. **What does `activeObjectSet` actually reference and where does it live?** Round 2 said "new,
   explicit, per-project/visual-session state, not `planningContext`" but didn't design it. Given the
   object model you propose here: is `activeObjectSet` a list of `object_id`s persisted alongside the
   project (survives reconnects/reloads) or ephemeral per-connection state (lives only in the `Host`/
   session like `planningContext` does today, lost on restart)? The atoms round assumed "human
   deliberately curates a small active set" — does that imply persistence (so the human doesn't have
   to re-activate objects every session) or is losing activation state on reload acceptable?
5. **Concurrency.** `Server.agentMu` serializes one turn at a time on one `Host` — Round 2 leaned on
   this to say the anchoring design doesn't reopen races. Does a *store* for Canvas objects (written
   to from tool calls, read from by `dynamicCentro` at prompt-build time, and — per the dual-edit
   requirement — also written to directly by a human via the floating panel's manual module,
   independent of any chat turn) need its own concurrency story, given the manual-edit path doesn't
   go through `agentMu` at all? This is a genuinely new surface Round 1–2 didn't have (all prior
   writes were tool-call-mediated).

## What I want back

Same as before: a concrete recommendation for each question, not a menu of options. If the honest
answer is "this needs its own round because the schema questions and the Planning-nesting question
are independent enough to blow up scope," say that explicitly and propose how to split it — don't
force a single coherent answer if the questions don't actually converge to one.
