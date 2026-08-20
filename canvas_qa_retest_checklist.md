Retest checklist for commit `21ad82b` — run this against a live `exo serve` instance (not
production 45873 unless you've already deployed this commit there) before trusting the fixes.
Covers the 4 claimed-fixed findings from `canvas_live_qa_findings.md`; #3/#4 are explicitly not
retested here since they weren't fixed.

## #1 — `canvas_edit_object` / AI edit of a materialized object
1. Discuss a diagram, materialize it (repeat the original repro: register flow, 6 nodes).
2. Open the floating panel, use the mini-chat (not the manual editor) to ask for a label change on
   one node — the exact case that previously failed ("cambia el label del nodo dashboard a 'Panel
   principal'").
3. Confirm: the agent actually performs the edit (not "no puedo editarlo, ¿creo un nuevo borrador?").
4. Confirm on disk / via the object's atom chain that the edit versioned via `supersedes` rather than
   mutating the original atom in place — this was the whole point of #1, don't just check the visible
   label changed, check *how* it changed.
5. Repeat once more with a second edit right after the first, to confirm the chain has two entries
   (original → supersedes #1 → supersedes #2), not just "latest wins with no history."

## #2 — diagram auto-layout
1. Materialize a fresh diagram with several nodes (5+, some branching — reuse the registration-flow
   prompt from the original test for a direct comparison).
2. Confirm nodes render at visibly distinct positions, not stacked at the top-left corner.
3. Confirm edges are visible as lines connecting the right nodes (not zero-length/invisible).
4. Also test a payload where the model *does* provide explicit `x`/`y` on a node (ask it directly, or
   inspect via manual edit and add coordinates by hand) — confirm those are still respected rather
   than being overridden by the auto-layout fallback, per the fix's stated design ("escape hatch for
   deliberate layout").

## #5 — dangling edge reference rejected at save
1. Manual-edit path: open the floating panel's manual editor on a materialized diagram, edit an
   edge's `to` field to reference a node id that doesn't exist, click Save. Confirm it's rejected with
   a clear error, not silently accepted (this is the exact repro from the original finding).
2. Agent-tool path: ask the mini-chat to do something that would create the same kind of dangling
   reference (e.g. "conecta este nodo con uno que no existe llamado 'foo'") — confirm the tool call
   fails clearly rather than the agent silently producing a broken payload.

## #6 — empty-state placeholder hides correctly
1. Start from a project with zero Canvas objects — confirm the "Nothing on the Canvas yet..."
   placeholder shows.
2. Materialize one object — confirm the placeholder actually disappears (not just visually covered,
   check it's gone from the DOM/hidden) the moment the object appears, no page reload needed.
3. Deactivate that object (if that control is reachable in the UI yet) — decide/confirm whether the
   placeholder is supposed to reappear when the only object present is inactive, since that's a
   corner case the original fix report didn't mention either way.

## Still open, not retested here (carry forward)
- #3 raw tool-call JSON leaking into chat on draft creation
- #4 `=== FINAL ===` internal marker visible in chat
Both traced to `agenthost/stdout.go`'s `redirectStdout` per the build session's report — systemic,
affects every tool call's chat display, not Canvas-specific. Decide filtering approach before asking
for a fix (see the two options the build session offered).

## Also worth a look, not a formal finding yet
The build session mentioned splitting its own commit away from an unrelated, unfinished,
uncommitted fix from a concurrent session still sitting in `agenthost/host.go`. Worth confirming that
work is still intact and whoever owns it knows it's there — it's exactly the kind of uncommitted work
that's already been lost once today to the still-unexplained `git reset` pattern.
