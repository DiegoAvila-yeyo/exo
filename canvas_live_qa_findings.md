Live QA pass on the Canvas build (`67a9c68`), run against `exo serve` on port 45873 with you
watching. Full flow tried: discuss a diagram → draft created → `canvas_suggest` banner →
materialize via button → open floating panel → manual JSON edit + Save → AI mini-chat edit attempt.
Six findings, ranked by severity. #6 is the one to fix first — it's a missing piece of the plan, not
a polish bug.

## 1. Missing: no agent tool to edit an already-materialized object (build_prompt §"Frontend", dual-edit requirement)

The floating panel's mini-chat is supposed to let the AI edit a materialized object directly,
versioned via `supersedes` — that was the whole point of "dual-mode editing" in the design. Live
test: asked the mini-chat to change a node's label on an already-materialized diagram. It replied:

> "El borrador ya fue materializado, por lo que no puedo editarlo directamente desde aquí... ¿Quieres
> que cree un nuevo borrador actualizado con ese cambio para reemplazarlo?"

and called `canvas_list_drafts: {}` to confirm there was nothing to act on. Checked
`agenthost/canvas_tools.go` — only `canvas_create_draft`, `canvas_materialize_draft`,
`canvas_list_drafts` exist. There is no tool that mutates an already-`materialized` object's payload
and writes a new versioned atom with `supersedes`. The agent is behaving correctly given what it has
available — the tool itself doesn't exist. This needs a new tool, something like
`canvas_edit_object: { object_id, payload_patch | new_payload }`, that operates only on
`phase: "materialized"` objects, goes through the same CAS write path as manual edits, and produces
a new atom version via `supersedes` per the anchoring design (Round 2/4).

## 2. Diagram nodes render stacked at (0,0) — payload is correct, renderer has no layout fallback

Root cause found precisely, not guessed. `termserver/assets/app.js`'s `renderDiagramStage`
(`app.js:1670`) positions every node with `(node.x || 0)` / `(node.y || 0)` (`app.js:1729-1730`) and
every edge endpoint the same way (`app.js:1717-1720`). But `agenthost/canvas_tools.go`'s tool
definitions never ask the model for `x`/`y`/`w`/`h` on nodes — confirmed by grep, zero references to
those fields in that file. The model-generated payload (verified live) only ever contains
`id`/`label`/`type` per node and `from`/`to`/`label` per edge — no layout data at all. Every node
therefore defaults to the same `x:0, y:0, w:160, h:60` box and stacks exactly on top of each other;
only the last one rendered in DOM order stays visibly on top ("Reintentar" in the live test's
6-node diagram). Every edge collapses to a zero-length line between overlapping points, invisible.

Two ways to close this, pick one deliberately rather than half-fixing both:
- **(a)** Extend the tool schema/prompt so the model is required to emit `x`/`y` (and ideally `w`/`h`)
  per node — puts layout responsibility on the model, matches "AI generates the structured payload"
  from the render-engine decision, but asking an LLM for precise non-overlapping pixel coordinates
  for an arbitrary graph is a known-fragile thing to rely on.
- **(b)** Add an auto-layout fallback in `renderDiagramStage` (or a shared layout pass before
  render) that assigns positions when a node has no `x`/`y` — e.g. a simple top-to-bottom or
  left-to-right pass following edge order, closer to how a tool like mermaid auto-lays-out nodes so
  the model only has to describe structure. This also protects the manual-JSON-edit path (finding
  #5) from producing the same collapse if a human adds a node without coordinates.
Recommendation: (b), with (a) as an optional override if a node *does* specify `x`/`y` explicitly —
gives the model an escape hatch for deliberate layout without making it mandatory.

## 3. Draft-creation response leaks internal tool-call JSON into the visible chat

When `canvas_create_draft` fires, the chat transcript shows raw structured text — literally
`"Flujo de Registro de Usuario", "payload": {"nodes": [{"id":"start","labe...` — visible to the
user as if it were assistant prose. This is the tool call's arguments (or its raw result) leaking
into the rendered chat output instead of being handled as an internal step. Only observed on draft
creation, not on materialize (materialize's confirmation text — "materialized '...' – now visible on
the Canvas" — rendered clean).

## 4. Internal `=== FINAL ===` marker printed in a normal chat response

Same draft-creation turn also printed a literal `=== FINAL ===` line before the human-readable
summary. Reads like a raw prompt-formatting/section separator that's meant for internal structuring,
not something that should reach the chat transcript.

## 5. Manual JSON edit produced a dangling edge reference, saved without validation

During the manual-edit test, the edited payload ended up with an edge
`{"from": "error", "to": "reintentar"}` while every node still has `id: "retry"` (not
`"reintentar"`) — a reference to a nonexistent node id. It saved successfully with no validation
error. Given finding #2's fix, a dangling reference like this would silently drop that edge from
rendering (the `if (!from || !to) return;` guard at `app.js:1713` already no-ops on it) rather than
surfacing the mistake to whoever edited it. Worth a save-time validation pass on the manual-edit path
specifically — confirm every edge's `from`/`to` resolves to a real node `id` in the same payload
before persisting, reject with a clear error otherwise.

## 6. Empty-canvas placeholder doesn't hide once an object exists

"Nothing on the Canvas yet. Discuss something with the agent..." stays visible above a materialized
object instead of disappearing once `objects` is non-empty. Minor but was visible in every screenshot
of this session.

## What to prioritize

Findings #1 and #2 first — #1 is a real gap against the original plan (dual-edit was a named,
load-bearing decision from Round 1, not an implementation detail), #2 blocks any real visual
verification of diagrams going forward, both manual and AI-created. #3–#6 are all real but lower
severity and can follow in the same or a subsequent pass.
