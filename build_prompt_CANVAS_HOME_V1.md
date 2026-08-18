You are building Exo's Canvas — a new permanent home-view: left sidebar (existing, unchanged) /
canvas center / chat right. The chat becomes the control surface for the whole app, not just
Planning. Go + frontend, real build task. This document is the consolidated output of four
Claude↔Codex design-critique rounds (`planning_design_canvas_home_prompt.md`,
`_round2_prompt.md`, `_object_model_round3_prompt.md`, `_intent_render_round4_prompt.md`) plus a
parallel atoms-anchoring round (`planning_design_atoms_canvas_anchor_prompt.md`). Nothing here is a
fresh idea — every decision below survived an adversarial pass. Read those five files for the
reasoning; this document states the resulting decisions as build instructions.

## The one architectural rule everything else follows

> **Composition, not absorption.** The Canvas object model is new and lives beside
> `planningstore`, not instead of it. Planning becomes one Canvas object type — a thin wrapper
> referencing an existing `planning_id` — never a migration of Planning's data into the new store.

This is why: `planningstore/model.go` has zero visual/spatial object model today (`Board` is
`{ID, Name, timestamps}` + Knowledge references, nothing else) and `PLANNING_MANIFESTO.md`'s
`Workspace → Planning → Board/Knowledge` root model would break if Planning's own data moved. Trying
to migrate `planningstore` into the new store in the same effort as building the Canvas turns this
into three migrations at once (conceptual + runtime + persistence) — explicitly rejected in Round 3.
`/api/plannings*`, `planningContext`, and the `planning_*` navigation tools (Rounds 1–3 of Planning
navigation, all implemented) stay exactly as they are, untouched, serving legacy Planning screens
that may or may not stay reachable outside the Canvas — that's a product decision for later, not a
build blocker now.

## What's closed and must not be re-litigated

- `PLANNING_MANIFESTO.md` — "el humano siempre dirige," "nada se pierde, todo evoluciona." Applies
  to the new store the same way it applied to the old one.
- Rounds 1–3 of Planning navigation (implemented) — `resolvePlanningContext`'s atomic state machine,
  `Host.SetPlanningContext`, `Server.agentMu` serializing one turn at a time on one `Host`. **None of
  this changes.** The Canvas's manual-edit path (below) is new precisely because it's the first write
  path in this codebase that does *not* go through `agentMu`.
- `planningstore/model.go`, `store.go` — unchanged, no `object_id` patched into it, no visual fields
  added to `Board`.
- The four Canvas design rounds and the atoms round, in full — this document only restates their
  conclusions; if something here seems underspecified, the source round almost certainly has the
  reasoning.

## New store: `canvasstore`

New top-level package, parallel to `planningstore`/`chatstore`, same JSON-file persistence style.
**One JSON file per project-canvas**, not one file per object — the Canvas needs to read/write its
object set, ordering, and active-set together as one coherent unit.

```go
// ProjectCanvas is the root aggregate — one JSON file per project.
type ProjectCanvas struct {
    ProjectID       string          `json:"project_id"`
    Objects         []CanvasObject  `json:"objects"`
    ActiveObjectIDs []string        `json:"active_object_ids"`
    Version         int             `json:"version"`    // CAS token, see Concurrency below
    CreatedAt       time.Time       `json:"created_at"`
    UpdatedAt       time.Time       `json:"updated_at"`
}

// CanvasObject is the generic envelope. Every object type (diagram today; image/text/
// planning-file/music/aprendizajes later) uses this same envelope with a type-specific payload.
type CanvasObject struct {
    ObjectID      string          `json:"object_id"`
    Type          string          `json:"type"`          // open vocabulary, not a fixed enum
    Name          string          `json:"name"`           // required — see Materialization below
    Phase         string          `json:"phase"`          // "draft" | "materialized" | "deleted"
    Activation    string          `json:"activation,omitempty"` // "active" | "inactive" — only meaningful when Phase == "materialized"
    Payload       json.RawMessage `json:"payload"`         // inline, structured, type-specific (see Render engine)
    AnchorAtomIDs []string        `json:"anchor_atom_ids,omitempty"`
    CreatedAt     time.Time       `json:"created_at"`
    UpdatedAt     time.Time       `json:"updated_at"`
}
```

Two independent lifecycle axes, not one flat enum — `Phase` (existence: draft/materialized/deleted)
and `Activation` (whether it's currently anchoring context; only meaningful once materialized).
Deactivating an object flips `Activation`, never touches `Phase`, `Payload`, or its atoms.

### Concurrency — new problem, not inherited from anywhere else

`agentMu` serializes agent turns on a `Host`; it does not protect `canvasstore`, because the manual
editing panel (below) writes to a `CanvasObject`'s payload directly, outside any chat turn. Minimum
sufficient v1 answer, per Round 3: **optimistic concurrency at the aggregate level.** Every write
(tool-driven or manual) reads `ProjectCanvas`, mutates, and saves with a compare-and-swap on
`Version`; a stale write is rejected and the caller re-reads and retries. No distributed locks, no
CRDT/OT, no per-shape merge — explicitly deferred past v1 (Round 4). If a human's manual drag and an
AI edit collide, one write bounces and reapplies; that UX is accepted for v1, not solved finer.

## Planning as a Canvas object

`type: "planning"`, payload is just `{"planning_id": "..."}`. No new Planning data lives in
`canvasstore` — opening/editing this object type reads/writes through the existing
`planningstore`/`planning_*` tools exactly as today. This is the composition seam; do not add
Planning-specific fields to the generic `CanvasObject` envelope.

## Draft → materialize lifecycle

Two-phase, and **draft is a real persisted record, not a conversation waiting to become one.** The
moment the agent can coherently describe a candidate object (Round 4's framing: "puede resumir
'estamos definiendo un diagrama X sobre Y'" — not "human said something diagram-adjacent"), it
creates a `CanvasObject` with `Phase: "draft"`, a required `Name` set at creation time, and whatever
partial `Payload` exists so far. This object is **not shown on the main Canvas** while in draft.
Casual discussion before that threshold creates nothing. This threshold is a prompt/contract detail
for the tool description below, not further architecture — don't build a separate classifier for it.

**Multiple drafts may coexist** — do not constrain to single-draft-at-a-time, it won't hold. Every
draft must be named before it's persisted (no anonymous drafts), so a later ambiguous "ya, hazlo" has
something to disambiguate against.

### Materialization tool

A new, narrow tool — not a reuse of `planning_create_board_and_open`'s shape, and not a boolean flag
on an existing tool:

```
canvas_materialize_draft: { draft_object_id: string (required) }
```

Only flips an *already-existing* `Phase: "draft"` object to `Phase: "materialized"` (making it
visible on the Canvas). It cannot create a draft and materialize it in one call — creation and
materialization are separate moments by design, mirroring the human's own two-phase mental model
(discuss, then commit). If more than one draft exists and the human's instruction doesn't
unambiguously name one, the tool call must not guess — respond asking which draft, or point at the
`/materialize <name>` fallback below. This mirrors Round 3-of-navigation's `nameMentioned()`
discipline, extended from "one turn's message" to "the set of currently open drafts."

## Intent detection — hybrid, three channels with distinct roles

- **Natural language** is the primary path: "materialízalo," "hazlo ya," "plásmalo" when intent is
  unambiguous (single draft, or draft named in the same instruction) drives a
  `canvas_materialize_draft` call.
- **Slash command** is the unambiguous fallback: `/materialize <draft-name>` — always available,
  always resolves to exactly one draft or a clear error, no inference involved.
- **Button** is a contextual affordance, not a universal trigger: it only appears once the agent has
  itself signaled (in its own reply) that a given draft is well-defined enough to materialize. It is
  never the only way to materialize something — NL and the slash command both work without it.

Detection logic stays inside the main agent loop (no separate classifier/heuristic process) — a
split-brain between "the agent that talks" and "the thing that decides what's materializable" was
explicitly rejected. The narrowness comes from the tool's contract (operates only on existing named
drafts), not from moving judgment outside the model.

## Render engine — structured, not freeform

**Diagrams (first object type) use a constrained shape-graph JSON payload, not SVG/raster/freeform
strokes.** Decided because `payload` is already JSON living inside a JSON-persisted, versioned,
CAS-protected aggregate — a declarative format an AI can generate/edit *and* a UI can render+diff
fits that substantially better than freeform drawing, and is far more stable for AI-driven edits
("move this node," "add this edge") than editing raw strokes or SVG would be.

v1 diagram payload shape: `nodes`, `edges`, `labels`, optional `groups`/`frames`, layout metadata,
and a limited set of style tokens. This is **not** a universal meta-DSL for every future object
type — don't build one preemptively. The shared principle that *does* apply to every future type:
**each object type defines its own structured, validatable, deterministically-renderable payload —
never an opaque freeform blob**, so "human and AI both edit this safely" doesn't get re-solved from
scratch per type later.

## Anchoring — dynamic centro, separate from `planningContext`

```
canvasstore (object_id exists now) → ProjectCanvas.ActiveObjectIDs (persisted, not ephemeral)
        ↓
dynamicCentro(activeObjectIDs) — resolves each active object's AnchorAtomIDs, injects their bodies
        into the prompt at build time — no tool call, same delivery path as today's static centro
```

- `planningContext` (`agenthost/planning_context.go`) is **not** extended to carry active objects.
  It keeps its current, narrow job: scope for legacy Planning tools. Mixing "where can this turn
  write" with "what memory is mandatory this turn" was explicitly rejected as conflating two
  responsibilities.
- `ActiveObjectIDs` persists inside `ProjectCanvas` (not `Host`-local, not lost on reload) — a
  human-curated anchor set that resets every session contradicts the point of anchoring.
- "Always read" = every turn while the object's `Activation == "active"`, never project-lifetime.
  Deactivating flips the flag; the atom body is untouched.
- Edits to a materialized object's content **version, never mutate in place** — a new atom with
  `supersedes` pointing at the prior one; "current atom" = walk the chain to the active end.
  `AnchorAtomIDs` on a `CanvasObject` should generally resolve to the head of that chain.
- Keep the active set small and human-curated by design — no automatic accumulation. This is not
  separable from the still-deferred context-budget/sub-session design (explicitly out of scope
  below); an unbounded active set is a likely root cause of that problem when it gets designed.

## Frontend

Three permanent columns replacing today's `#home-view` (chat centered, full width): left sidebar
(unchanged), canvas center, chat right. Clicking a materialized object opens a floating panel with
two halves: a manual editing module (mutates `Payload` directly, subject to the CAS rule above) and
an embedded mini-chat scoped to that object (drives edits via the AI instead). Both write paths hit
the same `CanvasObject` through the same optimistic-concurrency store methods — no separate code path
for "AI edit" vs. "human edit" at the storage layer.

## Explicitly out of scope / deferred — do not design or build these now

- **Sub-sessions / context-budget management** ("one project = one continuous unit of work, internal
  sub-sessions created when context fills up") — deferred by the user from the first planning round.
  Only tie-in required now: nothing above should make this harder later (e.g. don't hardcode
  chat-session-scoped assumptions into `canvasstore`).
- **Migrating `planningstore` into `canvasstore`**, or retiring Planning's own screens/tools.
  Composition only, per the architectural rule above.
- **Per-shape/CRDT-style merge** for concurrent dual edits — aggregate-level CAS is the v1 answer;
  finer merge is future work the structured payload format makes *possible* later, not required now.
- **A universal payload meta-DSL** across object types — each type defines its own structured
  payload; do not generalize prematurely.
- **Object types beyond diagram** (image, text, planning-file, music, aprendizajes) — the envelope
  is designed to hold them, but only `diagram` (and the `planning` composition wrapper) need working
  payload schemas and UI in this build.

## Acceptance / how to verify

- `go build ./...`, `go test ./...` pass, plus new tests for `canvasstore`: CAS rejects a stale
  write and the caller can retry successfully; draft creation requires a `Name`; `Phase`/`Activation`
  transitions only move the allowed directions (no `deleted` → `materialized`, no `Activation` set
  while `Phase != "materialized"`).
- `canvas_materialize_draft`: fails clearly on an unnamed/ambiguous target with multiple drafts open,
  succeeds on a single unambiguous draft, refuses to operate on an already-`deleted` or already-
  `materialized` object.
- Manual edit path and a simulated concurrent tool-driven edit on the same object: confirm one
  bounces on stale `Version` and a retry after re-read succeeds — this is the concurrency guarantee
  the whole design leans on, verify it directly rather than trusting the design doc.
- Manually: discuss a diagram in chat without crossing the materialize threshold — nothing appears
  on Canvas, but a `Phase: "draft"` object exists in `canvasstore` once the agent has summarized a
  coherent candidate. Say "materialízalo" — object appears on Canvas. Open two drafts, say "ya,
  hazlo" with no name — agent asks which one, does not guess. Click the materialized object — floating
  panel opens with both manual module and scoped mini-chat; edit via each, confirm both land in
  `canvasstore` and the object's active anchor atom reflects the latest edit via `supersedes`, not a
  mutated original.
