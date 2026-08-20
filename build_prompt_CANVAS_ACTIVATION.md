Fix the Canvas anchoring gap found during live QA retest (`canvas_activation_gap_findings.md`) —
real build task, Go + frontend. Not a redesign: every piece this needs was already decided in prior
rounds and mostly already built; this closes the one missing trigger.

## What's already true, don't re-litigate

- `dynamicCentro` (`agenthost/canvas_centro.go`) is correct and works — it iterates
  `pc.ActiveObjectIDs`, resolves each object's current atom via `pc.CurrentAtom`, and injects it
  into the system prompt every turn. Nothing wrong with this mechanism itself.
- `canvasstore.SetActivation(objectID, Activation)` (`canvasstore/mutate.go:80`) already exists,
  already validates phase (`ValidateActivation`), already maintains `ActiveObjectIDs` correctly on
  both activate and deactivate. Reuse it as-is — do not write new store logic.
- `POST /api/canvases/objects/{id}/activate` and `.../deactivate`
  (`termserver/canvas.go`'s `handleCanvasObjectActivation`) already exist and already call
  `SetActivation`. Reuse this HTTP path for the frontend control — do not add a second one.
- The only actual gap: **nothing calls any of the above.** No agent tool exists to activate/
  deactivate, and `app.js` only reads `obj.activation` to paint a badge (line ~1648) — it never
  fetches `/activate` or `/deactivate`.

## The one architectural rule this follows

> Activation stays a deliberate, explicit action — by the human or by the agent acting on an
> explicit human instruction. Never automatic, never a side effect of another tool.

This is why materializing a draft does **not** get changed to auto-activate as part of this build
(that was option A in the findings doc, explicitly not chosen) — Round 2/4 already decided the
active set must stay small and human-curated specifically because anchored atoms are the most
expensive thing in the whole design (full body injected every turn, unconditionally). Completing
the missing trigger has to preserve that guarantee, not quietly bypass it.

## 1. Two new agent tools

Same file, same pattern as `canvasEditObjectTool` (`agenthost/canvas_tools.go`) — narrow, each
doing exactly one state transition, no boolean flags collapsing two tools into one (same reasoning
Round 3-of-navigation already established for `planning_open` vs. the create-tools: a misfire on a
wrong tool name is auditable, a misfire on a flipped flag isn't):

```
canvas_activate_object:   { object_id: string (required) }
canvas_deactivate_object: { object_id: string (required) }
```

- Both call `pc.SetActivation` inside the same `saveWithRetry`/CAS pattern `canvasEditObjectTool`
  already uses — copy that structure, don't invent a new one.
- `canvas_activate_object` only succeeds on a `phase: materialized` object (mirrors
  `ValidateActivation`'s existing rule — surface its error text as-is on failure, don't reword it).
- Tool descriptions must make the deliberateness explicit to the model: only call these when the
  human explicitly asks to anchor/un-anchor something ("ancla esto," "ya no lo necesito presente,"
  "olvídate de ese diagrama por ahora") — not as a helpful inference from context. Same
  explicit-instruction discipline the existing navigation tools already document and enforce.
- Return text should confirm the new state plainly (e.g. `"'%s' is now active — I'll keep it in
  mind this session"` / `"'%s' is no longer anchored"`), since this is the one signal the human has
  that anchoring is actually on, given there's no other UI feedback loop for it in chat.

## 2. Frontend control

In the floating panel (`app.js`, same area that renders `openCanvasObjectPanel`), turn the
currently read-only activation badge into an actual toggle:

- Clicking it calls `POST /api/canvases/objects/{id}/activate` or `.../deactivate` depending on
  current state — reuse `fetch`/CAS-retry patterns already used elsewhere in this file for the
  manual-edit PATCH call, don't write a new request helper from scratch.
- On success, re-render the badge and (if you're already re-fetching `state.canvas` after other
  mutations) refresh from the same source of truth — don't hand-patch just the one field locally if
  a full re-fetch pattern already exists for other mutations in this file.
- On a 409/CAS conflict, same re-fetch-and-retry-once behavior already established for the manual
  edit path — don't introduce a different conflict-handling convention for this one control.

## Explicitly out of scope this build

- **No auto-activation on materialize** (option A, findings doc) — not this build, not silently
  folded in as a "convenience."
- **No default-active-on-materialize-with-manual-deactivate** (option C, findings doc) — explicitly
  deferred until after this ships and gets used for a while; revisit only as its own decision later,
  not bundled here.
- **No change to `dynamicCentro`, `CurrentAtom`, or the atom/`supersedes` chain** — all already
  correct, not touched by this build.
- **No retroactive activation of objects already materialized before this ships** — a human has to
  explicitly activate existing objects too, same as new ones; don't special-case old data.

## Acceptance / how to verify

- `go build ./...`, `go test ./...` pass, plus new tests: `canvas_activate_object` succeeds on a
  materialized object and fails clearly on a draft/deleted one; `canvas_deactivate_object` on an
  already-inactive object is a clean no-op (not an error); a CAS conflict on either tool retries and
  succeeds, mirroring the existing `canvas_edit_object` test coverage.
- Manually, repeat `canvas_qa_retest_checklist.md`'s test #1 end to end, but this time: materialize
  a diagram, **explicitly activate it** ("ancla este diagrama") before opening the floating panel's
  mini-chat, then ask for the same label-change edit as before. Confirm the edit this time preserves
  every node/edge that wasn't part of the request — the failure from `canvas_activation_gap_findings.md`
  (2 nodes silently dropped, 2 IDs renamed) should not reproduce once the object is genuinely
  anchored in the system prompt.
- Also verify the negative case: leave a second, separate diagram materialized but **not**
  activated, and confirm the agent has no knowledge of its contents when asked about it — anchoring
  should be visibly per-object, not global once anything is active.
