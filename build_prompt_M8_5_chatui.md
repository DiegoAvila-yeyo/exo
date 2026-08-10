You are adding a minimal chat panel to `exo`'s existing browser UI, in `~/exo/termserver/assets/`
(`index.html`, `app.js`, `app.css`). Real build task — write actual code. This piece has no
dedicated Go backend work; the backend routes it talks to (`POST /api/chat`, `GET
/api/chat/stream`, `POST /api/approve`) already exist and are built/tested (pieces 3-4 of M8,
closed — read `~/exo/M8_INTEGRATION_DESIGN.md` for their exact contract, especially the SSE event
shapes: `{"type":"idle"}`, `{"type":"busy"}`, `{"type":"output","text":"..."}`,
`{"type":"approval","prompt":"...","detail":"...","session_id":"..."}`, `{"type":"done"}`, plus a
`: heartbeat` comment line every 10s).

## Why this exists

The M8 goal is "type a request in a browser dashboard and watch an agent drive a real terminal you
can watch and take over." Pieces 1-4 built everything except the actual chat box — the existing UI
(`index.html`) only has the terminal-session panel from earlier milestones (M0-M6), no chat
interface. This piece adds that missing surface so the system can actually be used and manually
verified end-to-end for the first time.

## What to build

### 1. HTML: a chat panel

Add a new section to `index.html`, e.g. inside `<main class="workspace">` alongside the existing
`terminal-card`, or as an additional sidebar/panel — your call on layout, keep it simple and
consistent with the existing minimal aesthetic (read `app.css`'s existing class conventions —
`.panel`, `.panel-header`, `.primary-button`, `.ghost-button`, `.banner`, `.status-pill` — and reuse
them rather than inventing a new visual language). Needs:
- A scrollable message log area (agent output appears here as it streams in).
- A text input + submit button (or a `<form>`) for typing a chat message.
- A visible "busy"/"idle" indicator (reuse the existing `.status-pill` pattern from the connection
  indicator).
- An approval banner area: when an `approval` SSE event arrives, show `prompt`/`detail` and two
  buttons ("Approve" / "Deny") that `POST /api/approve` with `{"approved": true/false}`. If the
  approval event includes a `session_id`, show it too (e.g. "for session session-0007") so the user
  can see which terminal session it's about — this is the whole point of piece 3's `session_id`
  correlation work, don't drop it silently.

### 2. JS: wire it to the existing SSE/HTTP contract

In `app.js` (extend the existing file, following its current patterns — read the whole file first,
especially the `metaContent`, `fetchJSON`, and CSRF-token-in-query-string pattern already used for
`/api/sessions` calls around line 100 and 123):
- On page load, open an `EventSource` (or manual `fetch` + streaming reader, matching whatever's
  simplest given the existing code style) to `/api/chat/stream`, and handle each event type:
  `idle`/`busy` → update the status pill; `output` → append a line to the message log; `approval`
  → show the approval banner with prompt/detail/session_id; `done` → clear the busy state.
- On form submit, `POST /api/chat` with `{"message": "<input value>"}` and the same
  `?csrf_token=...` query-string pattern already used elsewhere in this file (read the exact
  existing `fetchJSON` calls to match it precisely — don't invent a different auth pattern for this
  one route). Handle the `409 {"error":"busy"}` case by showing a brief inline message (don't just
  silently fail) — matches the single-flight lock semantics already documented in the design.
- Approve/Deny buttons `POST /api/approve` the same way, with `{"approved": true}` or `{"approved":
  false}`, and clear the approval banner on response.
- Reconnect behavior: if the `EventSource`/stream drops, retry with a simple backoff (reuse
  whatever reconnect pattern the existing terminal WebSocket code in `app.js` already uses if
  there's one worth mirroring — check before inventing a new one).

### 3. CSS

Minimal additions to `app.css` for the new elements, matching existing spacing/color variables
(check the top of the file for CSS custom properties already defined, reuse them).

## What NOT to do

- Do not touch any Go code — this is frontend-only, the backend contract is already closed and
  tested.
- Do not add a chat history persistence mechanism, markdown rendering, or any feature beyond a
  plain-text scrolling log + input + approval banner — v1 scope only, matching the rest of M8's
  "smallest correct thing" discipline.
- Do not change the existing terminal-session panel's behavior or layout beyond adding space for
  the new chat panel.

## Verification

No Go tests apply here (pure frontend). Instead: build the `exo` binary (`go build ./...` from
`~/exo`, confirm it still compiles — the assets are embedded, so a broken HTML/JS file would still
compile fine in Go, this just confirms you haven't broken the Go embed setup) and describe exactly
what you changed file-by-file so it can be verified by actually running `exo serve` and clicking
through it in a real browser (that verification step happens after this build, not as part of it).

## When done

Report: exact files touched, a summary of the DOM structure you added, the exact SSE event
handling logic, and confirmation `go build ./...` still succeeds in `~/exo`.
