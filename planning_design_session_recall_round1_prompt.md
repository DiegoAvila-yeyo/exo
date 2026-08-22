This is a **design-critique round**, same Claude↔Codex adversarial-review pattern used throughout
this project (`planning_design_canvas_home_prompt.md` through `_round4_prompt.md`,
`planning_design_atoms_canvas_anchor_prompt.md`, `planning_design_canvas_next_round5_prompt.md`,
`M8_design_round1-3`). Nothing gets built yet.

## What's closed and must not be re-litigated

- **Two separate systems, not one, and this round is only about the second.** The human
  (Yeyo) drew this line explicitly in conversation:
  1. **"Memorias de la IA"** — `nucleo-base`'s Layer 4 (`memoryservice`/`localstore`, SQLite
     `memory_items` table). Already built and wired (`agenthost/host.go`'s
     `openMemoryStoreBestEffort`, `SetLocalMemoryService` — see `build_prompt_M8_10_memory.md`).
     Confirmed live: `~/Library/Application Support/exo/memory.db` exists, the schema is real, the
     coordinator calls `applyPostTaskUpdate` automatically every turn. It currently has zero rows
     across 205 real chat sessions — traced to `nucleo-base/layer4-knowledge-memory/memoryservice/
     service.go`'s `PostTaskUpdate`, which only creates an item when a turn has both a non-empty
     summary AND `len(ChangedFiles) > 0` (real git-diff-detected changes in the project directory);
     none of this project's browser-chat turns ever tripped that gate. **Not a bug, not in scope
     here** — this system is working as designed, just never had a qualifying turn yet.
  2. **"Sesiones"** — does not exist yet, this round designs it. What this is: currently, chat
     sessions (`chatstore` — one JSON file per session, full transcript, shown in the sidebar's
     per-project groups per the 2026-08-21 UI round) are **completely isolated from each other**.
     Confirmed in code: `termserver/chat.go` loads a session by exact ID
     (`s.chatStore.Load(body.SessionID)`) or creates a fresh one (`s.chatStore.Create()`) — nothing
     ever reads across sessions. There is no existing "inject all sessions as context" behavior to
     fix; the human is asking to design the *right* mechanism before any future feature naively
     builds the wrong one (full injection) — this was flagged as a real risk because it's exactly
     the kind of thing "one project = one continuous unit of work, sub-sessions created when context
     fills up" (deferred in `build_prompt_CANVAS_HOME_V1.md`, decision #8) would need, and nobody
     designed it at the time.
- **The pull-vs-push precedent this round explicitly follows**: `agenthost/atom_tool.go`'s existing
  `yeyo` periferia catalog contract — `atom{action:"list"}` returns name+description of every entry,
  `atom{action:"get", name:"..."}` fetches one body, only when the model decides it's relevant.
  Nothing force-fed. The human wants session recall to work the same way — the model pulls a
  session's summary when it decides it needs to, never gets the full backlog pushed into context.
- **Atoms themselves stay read-only for the live agent, and this round does not change that.**
  `atom_tool.go` only implements `list`/`get` — there is no `create`/`write` action anywhere in
  `nucleo-base`'s tool package. Atom files (`~/yeyo/atoms/<project>/periferia/*.md`) are authored
  outside the live coding agent's own turn. The human explicitly chose, when offered the
  alternative, **not** to extend the real `yeyo` catalog to hold session summaries (chosen option
  "B" below) — do not re-open "should atom_tool grow a write action" in this round.
- **`Atom.Scope`** (`~/yeyo/atom.go`) already models global-vs-project (`GlobalScope`, or a project
  identifier matching `path.Base(rootPath)`, stored under `atoms/<project>/periferia/`). Whatever
  gets built for session recall should reuse the same per-project scoping *idea*, even though it
  will live in its own store, not inside `~/yeyo` itself (see decision below).

## What the user decided, verbatim inputs to this round

1. **Who triggers recall**: the AI, via a tool, "preferiblemente con átomos" — i.e. modeled on the
   atom system's pull mechanism (list/get), reusing that shape rather than inventing something
   unrelated.
2. **Where session summaries live — explicit choice between two options offered, human picked B**:
   - A. Real `yeyo` atoms — summaries become files in the curated `~/yeyo` periferia catalog,
     mixed with behavioral-guidance atoms, would require adding a write action to `atom_tool`.
   - **B (chosen)**: A new, separate store on the `exo` side — session summaries do **not** live in
     `~/yeyo` (a catalog curated by hand and shared across every project in the ecosystem, not just
     `exo`) — but the *tool* the model uses to search/fetch them mirrors `atom_tool`'s exact
     `list`/`get` contract and pull behavior. Same shape for the model, different (and separate)
     backing store.
3. **How "the session is about to fill up" is detected**: real token accounting against the active
   model's context window — the human's own reference is Claude Code's desktop context-window
   meter (screenshot shown in conversation: "Ventana de contexto 454.5k / 967k (47%)"), not a proxy
   like message count or elapsed turns.
4. **What happens when the threshold is hit**: the user sees a message recommending closing the
   session ("la sesión está por llenarse, se recomienda cerrar para hacerle resumen y abrir una
   nueva"). On close, the AI generates a summary of the session, saves it (into the new store from
   #2), and the session is marked closed.
5. **What happens to the original transcript**: it stays. Full backup, untouched, "por si acaso" —
   closing a session never deletes or truncates its `chatstore` file.
6. **Recall scope**: per active project only — never cross-project.

## What this round needs to actually design — the real gaps, not yet decided

1. **Token accounting doesn't exist in the real chat path today.** `api.Usage{InputTokens,
   OutputTokens}` and `provider.CatalogModel.ContextLen` both already exist in `nucleo-base` — but
   the only place `Usage` is currently accumulated is `agenthost/checkpoint_scale.go`, a separate
   evaluation harness, not `Host.Run` (what the browser chat actually calls). Building this for
   real means: capturing `Usage` from every turn's response inside the actual `Host.Run`/coordinator
   path, accumulating it per `chatstore.ChatSession` (new persisted field(s)), and resolving the
   active model's `ContextLen` at runtime to compute a percentage. Take a real position on where
   this accumulation should live (on `Host`? on `chatstore.ChatSession` itself, persisted alongside
   the transcript? somewhere else?) and how it survives a process restart (mid-session token count
   currently only exists in memory anywhere in this codebase — does it need to be persisted per
   turn, or is an approximate in-memory-only counter, reset on restart, acceptable for v1?).
2. **What threshold** — Claude Code's own UI (the human's reference) just shows a live percentage,
   always visible, no hard cutoff shown in the screenshot. The human's request is narrower: a
   message that recommends closing once the session is "about to fill." Pick and argue an actual
   number (e.g. 80%? 90%?) or a formula, not just "some threshold" — and say whether it should be a
   fixed constant or something that could later become configurable.
3. **Who writes the summary, and how** — is it the same agent turn that's already running (the model
   writes its own summary as part of responding to the close action, in normal conversation), or a
   dedicated, separate summarization call (closer to how `memoryservice`'s own internal "librarian"
   sub-calls work for Layer 4)? This has real cost/latency/quality tradeoffs — argue one.
4. **The new store's shape.** Given decision #2/B and the per-project scoping precedent
   (`Atom.Scope`), the natural persistence precedent already in this codebase is `canvasstore`'s
   pattern: one JSON file per project (keyed by `ProjectID`/absolute path), CAS-protected writes,
   write-tmp-then-rename. Does a session-recall store want the exact same shape (one file per
   project, containing every closed session's summary for that project), or does per-project
   scoping argue for something else? Also decide: does a summary entry need its own lifecycle
   (can it ever be superseded/re-summarized, deleted, marked stale — mirroring `CanvasAtom`'s
   `Supersedes` idiom) or is it write-once, immutable, forever?
5. **The new tool's exact contract.** Mirrors `atom_tool` in spirit (`list`/`get`), but needs real
   answers: does `list` return session titles/summaries' one-line descriptions for the active
   project only (per decision #6 above — how does the tool even know which project is "active" —
   same `canvas_object_id`-style explicit scoping precedent from Canvas, or something else)? Does
   `get` return only the AI-written summary, or can it also reach into the full backed-up
   `chatstore` transcript (decision #5 keeps the raw transcript around specifically "por si acaso" —
   does that mean the tool should have an escape hatch to pull it, or is the raw transcript
   deliberately human-only/manual-recovery-only, never agent-reachable through this tool)? Take a
   position — don't just list the fork.
6. **Session lifecycle mechanics** — once a session is "closed" per this design, what actually
   changes about it in `chatstore`/the sidebar? Does it disappear from the per-project session list
   built in the 2026-08-21 UI round (`renderProjectList` in `app.js`), get a visual "closed" marker,
   or behave identically to any other session (openable, just also has a summary now)? And can a
   closed session ever be reopened and continued, or is "closed" terminal (the only way forward is
   a brand-new session, with the old one now only reachable via its summary or manually)?

## What I want back

A real recommendation for each of the six gaps above — argued, not just more open questions handed
back. If any of the six turns out to be smaller or bigger than it looks here once you dig in, say
so. If the whole "separate store, atom-shaped tool" direction has a real problem once you push on
it (not just style nitpicks), say that too, the way Round 5's response did for `atom_group`.
