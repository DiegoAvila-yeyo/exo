You are redesigning the chat panel of `exo`'s existing browser UI, in `~/exo/termserver/assets/`
(`index.html`, `app.js`, `app.css`). Frontend-only, no Go code. Real build task.

## Context

`exo`'s dashboard (read the current `index.html`/`app.js`/`app.css` in full first) already has a
working chat panel — `#chat-log`, `#chat-form`/`#chat-input`, `#chat-status-indicator`,
`#approval-banner` — wired to real backend routes (`POST /api/chat`, `GET /api/chat/stream`,
`POST /api/approve`, all already built and working, do not touch any of that wiring or any Go
code). Today it's a small, boxy panel wedged between the terminal-session sidebar and the terminal
view. The user wants it restyled closer to ChatGPT's layout: when there's no conversation yet, a
centered "what can I help with" headline, a large rounded input bar, and a few clickable suggestion
chips that pre-fill the input with a real example of what this specific agent can do. Once a
conversation starts, it transitions to a normal scrolling message log with the input pinned at the
bottom (like it already roughly does, just needs the "empty state" to look intentional rather than
a small gray placeholder line).

**Scope for this pass, explicitly**: only the chat panel's visual layout and empty/active states.
Do **not** touch the terminal-session sidebar, the terminal (xterm) view, session
create/list/close, the connection/ownership indicators, or the approval banner's *logic* — only
restyle the approval banner if needed to fit visually with the new chat layout, without changing
what triggers it or how it resolves. No tab split between "Chat" and "Terminal" — that was
discussed and explicitly deferred, this pass is chat-panel-only.

## What to build

### 1. Empty state (no messages yet)

When `#chat-log` has no entries, show a centered layout inside the chat panel:
- A short headline, e.g. "¿Con qué puedo ayudarte?" or similar (match the user's language — this
  UI's existing copy is in English, e.g. "Ask the agent to inspect, run, or explain something" —
  keep whatever the existing convention is, don't mix languages inconsistently; if unsure, keep
  English to match the rest of the UI).
- A large, rounded input bar (visually bigger than today's), vertically centered in the available
  chat panel space when empty.
- 3-4 clickable suggestion chips below the input, each with a short label and an icon/emoji if it
  fits the existing minimal aesthetic (check `app.css`'s existing icon usage, e.g. the 🖥️/💻/📁
  emoji already used in chat log entries — matching that convention is fine, don't add an icon
  font/library). Suggestion text should reflect **real, working exo capabilities** verified in this
  session, not generic placeholders:
  - "Open a terminal and run a command" (terminal session + real shell)
  - "Search my GitHub repositories" or "List my open pull requests" (real MCP tool, already wired)
  - "Show my Jira tickets" or similar (real MCP tool, already wired)
  - "Create a file in this project" (real write_file tool)
  Clicking a chip should fill `#chat-input`'s value with the corresponding example text (don't
  auto-submit — let the user review/edit before sending, matching normal chat UX expectations).

### 2. Active state (conversation in progress)

Once the first message is sent (i.e. `#chat-log` has content), the layout transitions to: message
log fills the available vertical space and scrolls, input bar pinned at the bottom (smaller than
the empty-state input, matching the existing input's current size is fine here). This is close to
what already exists — the main change is making the *empty* state look deliberate instead of a
one-line gray placeholder, and making the transition between the two states not jarring (a CSS
transition or simple JS class toggle is enough, don't over-engineer this).

### 3. Implementation approach

- In `app.js`, add a small function that toggles a class on the chat panel container (e.g.
  `chat-panel--empty` vs `chat-panel--active`) based on whether `#chat-log` has any rendered
  entries, called from wherever messages currently get appended (find the existing render/append
  function — read the file to locate it) and once on page load.
- In `app.css`, add the styles for both states under those two classes, reusing existing CSS custom
  properties (colors, spacing, border-radius) already defined at the top of the file — don't
  introduce a new color palette or spacing scale.
- Suggestion chips: plain buttons, `type="button"`, each with a `data-prompt="..."` attribute
  holding the example text, with one shared click handler that reads `data-prompt` and sets
  `#chat-input`'s value (and focuses the input).

## What NOT to do

- Do not touch any Go code, any backend route, or the SSE event handling logic — this is styling
  and a small amount of new DOM/JS for the empty-state chips only.
- Do not change the terminal sidebar, terminal view, or session management UI in any way.
- Do not change the approval banner's trigger/resolve logic — cosmetic restyling only if it visibly
  clashes with the new layout, and even that should be minimal.
- Do not add a framework (React, Vue, etc.) — this is vanilla JS/CSS matching the existing file's
  style exactly.
- Do not remove or hide the terminal panel — it stays exactly where and how it is, just visually
  less emphasized relative to the now-larger chat panel is fine, but it must remain fully
  functional and reachable.

## Verification

No Go tests apply (frontend-only). Build the Go binary (`go build ./...` from `~/exo`) to confirm
the embedded-assets setup still compiles (a broken HTML/JS/CSS file won't fail a Go build, this
just confirms you haven't broken file paths/embed directives). Real verification happens by
actually running `exo serve` and looking at it in a browser afterward — describe file-by-file what
changed clearly enough that this can be checked visually.

## When done

Report: exact files touched, the exact suggestion chip texts you used, and confirm
`go build ./...` still succeeds in `~/exo`.
