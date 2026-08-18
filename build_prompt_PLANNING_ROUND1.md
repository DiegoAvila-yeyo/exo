You are building Round 1 of `exo`'s new Planning section: the backend HTTP API plus a minimal
frontend shell in `~/exo/termserver/assets/` (`index.html`, `app.js`, `app.css`). Real build task.

## Context — read these two things in full before writing any code

1. `~/exo/PLANNING_MANIFESTO.md` — the product philosophy and the frozen v1 conceptual model.
   Every UI/API decision below has to respect it, especially: **the word "Knowledge" never
   appears in the UI** (render Decision/Principle/Note/Research/Reference/Question by name), **the
   default screen is empty** (no always-visible list of decisions — they surface only when a board
   node that references them is selected), and **AI output is never silently canonical** — it's out
   of scope for this round (no AI wiring yet), but don't build anything that would make adding that
   review/accept flow later awkward.
2. `~/exo/planningstore/` (`model.go`, `store.go`, `store_test.go`) — the Go domain model already
   implemented and tested (`go test ./planningstore/...` passes, 10 tests). This round wires it up;
   it does not redesign it. If something in the model genuinely doesn't fit what you're building,
   stop and flag it instead of quietly reshaping it.

Also skim `termserver/assets/index.html`, `app.js`, `app.css` and `termserver/server.go` in full
before touching anything — this project already has a working sidebar (`.sidebar-nav`,
`.sidebar-nav-item`) and a chat panel (`#chat-panel`, wired to `POST /api/chat` /
`GET /api/chat/stream`) that this round must not break.

## Scope for this round, explicitly

**In scope:**
- HTTP handlers in `termserver` wrapping `planningstore.Store` (CRUD for Planning, Board,
  Knowledge; `Supersede`/`ResolveQuestion` actions).
- A new sidebar nav item, "Planning", alongside the existing ones.
- A Planning list screen (name, board count, last edited — mirrors `planningstore.PlanningSummary`).
- A Board screen: big empty canvas area (a static placeholder `<div>` is fine — no pan/zoom/drag
  yet, that's Round 2) with the existing chat panel moved to the bottom of that screen instead of
  center. The chat's backend wiring (`/api/chat`) does not change in this round; only its position
  when a Board is open.
- A lightweight side panel that shows Knowledge entries for a board (`GET` the board's Knowledge)
  ONLY when the user explicitly opens it (e.g. clicking a "Notes for this board" affordance) — not
  an always-visible list. This round can ship this as a simple list-by-board; the "surfaces only
  when a canvas node is selected" behavior depends on Round 2's canvas objects and is out of scope
  here — just don't build a permanent always-on sidebar of Knowledge, since that's the exact thing
  the manifesto rules out.

**Out of scope — do not build this round:**
- The actual infinite pan/zoom/drag canvas engine and visual objects (frame/arrow/rectangle/image).
  Round 2.
- Any AI-authored content, proposal/accept UI, or chat-to-canvas actions. Round 3+.
- Multi-user/collaboration.
- Project creation/linking UI (`ProjectLink` already exists in the model; no screen needs it yet).

## What to build

### 1. HTTP API (Go, `termserver/`)

Add routes (naming/style should match the existing `/api/chat*` handlers in `server.go`):

- `POST /api/plannings` `{name}` → creates via `planningstore.Store.Create`.
- `GET /api/plannings` → `Store.List()`.
- `GET /api/plannings/{id}` → `Store.Load(id)`.
- `POST /api/plannings/{id}/boards` `{name}` → `Planning.AddBoard`, then `Store.Save`.
- `POST /api/plannings/{id}/knowledge` `{type, title, body, board_id, derived_from}` →
  `Planning.AddKnowledge`, then `Store.Save`. Reject unknown `type` values (must be one of the six
  `Knowledge*` constants) with 400.
- `GET /api/plannings/{id}/boards/{board_id}/knowledge` → `Planning.ForBoard(board_id)`.
- `POST /api/plannings/{id}/knowledge/{knowledge_id}/supersede` `{new_id}` → `Planning.Supersede`.
- `POST /api/plannings/{id}/knowledge/{question_id}/resolve` `{decision_id}` →
  `Planning.ResolveQuestion`.

Construct the `planningstore.Store` once at server startup using `appconfig.PlanningStoreDir()`
(already added), same lifecycle as however `chatstore.Store` is constructed and threaded into
`Server` today — follow that exact pattern, don't invent a new one.

### 2. Sidebar entry

Add a "Planning" `sidebar-nav-item` above/below the existing ones (match current markup/CSS
conventions exactly — check how the existing nav items toggle active state). Clicking it shows the
Planning list screen in the main content area, replacing whatever's there without a full page
reload (same SPA-ish pattern the existing chat/terminal switch already uses, if there is one —
check `app.js` for how sections currently toggle before inventing a different mechanism).

### 3. Planning list screen

Simple: name, board count, "last edited X ago", a "+ New Planning" affordance (prompts for a name,
`POST /api/plannings`, then opens the list's first board once created — Round 2 note: for now, just
open the empty Planning with no boards; nothing to open yet).

### 4. Board screen

- Header: Planning name → Board name (breadcrumb-ish, plain text is fine this round).
- A `⌘K`-style quick switcher is Round 2 — this round can use a simple `<select>` or list to switch
  boards, it doesn't need to be polished, just functional.
- Canvas area: static placeholder div with a subtle empty-state message ("This board is empty —
  Round 2 adds the canvas"). Do not fake canvas interactions.
- Chat panel: reuse the existing `#chat-panel` markup/behavior, moved to the bottom of this screen
  instead of centered. Its backend wiring must not change.

## Acceptance / how to verify

- `go build ./...` and `go test ./...` both pass.
- Manually: open exo, click "Planning" in the sidebar, create a Planning, create a Board inside it,
  open the Board, confirm the chat panel still works (send a message, get a response) while sitting
  at the bottom of the Board screen instead of centered.
- No regression to the existing terminal/chat/session sidebar behavior — re-check those still work
  after your changes.

## Explicitly not this round

If you find yourself building drag/connect/zoom, an AI-proposal review UI, or a permanent
always-visible Decisions list — stop, that's the wrong round. Flag it back instead of building it.
