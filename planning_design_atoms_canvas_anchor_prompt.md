This is a **design-critique round only**, same pattern as `planning_design_round3_navigation_prompt.md`
and `planning_design_canvas_home_prompt.md`. Nothing gets built yet. This is the one piece of the
Canvas-home direction that got carved out specifically for this session, because it's about the atom
model, not about the Canvas UI — don't re-litigate the Canvas UI itself here, that's the other
document's job.

## What's closed and must not be re-litigated

- The atom system as it exists today: `agenthost/atom_tool.go` exposes the yeyo periferia catalog as
  a **pull** mechanism — the model calls `atom{action:"list"}` to see the catalog (name +
  description of every entry), then `atom{action:"get", name:"..."}` to fetch one atom's full body
  *when it decides it's relevant*. Nothing is force-fed into context automatically today. There's
  also `atoms_decision_tool.go` / `atoms_decision_gate_test.go` — a decision-gate layer on top of the
  base catalog — read those before assuming the "get" path above is the whole picture.
- `PLANNING_MANIFESTO.md`'s "el humano siempre dirige" applies here too: whatever anchoring mechanism
  comes out of this round still has to be something the human caused (by materializing a Canvas
  object), not something atoms silently accumulate on their own.

## What the user is asking for now

From the Canvas-home planning round (see `planning_design_canvas_home_prompt.md`, decision #5): when
a Canvas object gets materialized (a diagram, later other object types), the user's own proposed
mechanism for making it act as a persistent **anchor** for the rest of that session is: *"que cada
cosa sea un conjunto de átomos que sí o sí deba leer la IA siempre."* I.e., an object doesn't just
sit there passively — it's backed by one or more atoms the model is *forced* to read, not atoms it
may optionally fetch.

## The tension to dig into

Today's atom model is **pull, optional, model-initiated** — the model decides whether a given atom
is relevant and fetches it if so. What the user wants for Canvas anchoring is closer to **push,
mandatory, object-driven** — the mere existence of a materialized object in the active Canvas forces
its atom(s) into every turn, regardless of whether the model would have chosen to fetch them.

Questions this round needs to actually answer, not just restate:

1. **Is "mandatory always-read" a new mechanism, or a mode on the existing one?** E.g. does
   `atom_tool`'s `"list"` response for an active-Canvas turn get pre-filtered/pre-augmented to
   already include the anchored atom bodies (so the model never has to choose to `"get"` them), or is
   there a separate, always-injected-into-the-prompt path that bypasses the tool-call mechanism
   entirely? These have very different cost/latency/context-budget implications.
2. **What counts as "always"?** Every turn for the life of the project (per decision #8 in the Canvas
   doc — projects are now the persistent unit, not chat sessions)? Only while that specific object is
   the "active" one (per decision #6/#9 — objects can be activated/deactivated)? If deactivating an
   object is supposed to stop it being an anchor without deleting its data (per Canvas decision #9),
   this round needs to define what "stop being an anchor" actually flips off.
3. **One atom per object, or a set?** The user said "un conjunto de átomos" (plural) per object —
   does a diagram get exactly one atom describing it, or can materializing an object spawn/reference
   multiple atoms (e.g. one for its content, one for edit history, one for why it was created)? If
   multiple, how do they relate to each other and to the object's own identity in
   `planningstore`/wherever Canvas objects end up persisted?
4. **Who writes the atom's content, and when?** Is the atom generated once at materialization time
   and then static, or does it get rewritten every time the object is edited (manually or via its
   embedded mini-chat, per Canvas decision #6)? If it's rewritten on every edit, that's a different
   engineering problem (atom mutation, versioning — does "nada se pierde, todo evoluciona" from the
   manifesto apply to atoms the same way it applies to Planning Decisions?) than "written once."
5. **Cost.** If a project accumulates many materialized objects over its lifetime and all their atoms
   are mandatory-always-read, what stops this from blowing the context budget the same way "inject
   full history every turn" would? Does mandatory-read only apply to *active/visible* objects in the
   current Canvas viewport, or literally every object the project has ever materialized? This
   directly touches decision #8 from the Canvas doc (sub-sessions get created when context fills up)
   — is atom accumulation actually the main driver of that, and if so this round and that deferred
   persistence design aren't as separable as they look.

## What I want back

A written report: a recommended shape for "mandatory anchor atoms" that answers questions 1–5 above
concretely — not a redesign of the whole atom system, just the extension needed to support Canvas
object anchoring. If forcing atoms to be mandatory-read turns out to be the wrong lever entirely (e.g.
the right mechanism lives in `planningContext`-style per-turn scoping instead of the atom catalog),
say so and argue the alternative, same as the other rounds.
