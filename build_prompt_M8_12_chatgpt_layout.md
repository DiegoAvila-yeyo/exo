You are doing a bigger visual pass on `exo`'s browser UI (`~/exo/termserver/assets/`: `index.html`,
`app.js`, `app.css`). Frontend-only, no Go code. This supersedes the look of the previous chat-panel
redesign (the empty-state headline/input/suggestion-chips content from that pass stays — you're
restructuring the page shell around it, not rebuilding the chat logic).

## Context and goal

The user wants `exo`'s dashboard to visually match ChatGPT's own app shell: a dark theme, a fixed
left sidebar (logo/search/collapse icons at top, a "New chat" action, a short static nav list, a
"Pinned"/"Recent" section), and the main content area centered — headline, a large rounded input
bar, suggestion chips below it. Two reference screenshots were provided in conversation (not
attached here, described below); match their structure and dark palette, not pixel-for-pixel.

**Explicit scope for this pass**: only the page shell and chat empty/active state. `exo` has real
functionality beyond chat — terminal session list/create form, the live xterm terminal view,
connection/ownership indicators, take-control/approval flows — none of that goes away, but none of
it is visually reachable in this pass either. Read the current `index.html`/`app.js` in full to see
exactly what those elements are (`#session-list`, `#create-session-form`, `#terminal-wrapper`,
`#connection-indicator`, `#owner-indicator`, `#take-control-button`, `#close-session-button`,
`#banner`, `#terminal-overlay`, `#approval-banner`, and all their supporting JS/WebSocket logic in
`app.js`). **Hide them, do not delete them or their logic** — wrap the sidebar's session
management section and the whole terminal card in a container with `hidden` (or a CSS class that
sets `display:none`), so the underlying JS (WebSocket connect/reconnect, session list refresh,
takeover, approval resolution) keeps running exactly as it does today, just not rendered. This is
explicitly temporary — the user said "ahorita vemos cómo las adaptamos" (we'll figure out how to
re-integrate them visually later), so don't rip anything out.

## What to build

### 1. Dark theme

Add a dark palette to `app.css` — near-black page background, slightly lighter dark-gray cards for
the sidebar and input bar, off-white/light-gray text, subtle low-contrast borders (matching the
reference screenshots' aesthetic: pure black background, `#1e1e1e`-ish input bar, white headline
text, muted gray secondary text). This replaces the current light cream/orange theme's colors —
update the existing CSS custom properties at the top of the file rather than hardcoding new colors
inline, so the rest of the (currently hidden) UI still resolves sensibly through the same variables
if/when it's un-hidden later.

### 2. Sidebar shell

New fixed-width left sidebar (~260-280px), replacing the current "Terminal Sessions" branded
sidebar's visual content (the branded header text can stay or be simplified to just "exo" — your
call, keep it minimal) with this structure, top to bottom:
- A small top row with a couple of icon buttons (search icon, sidebar-collapse icon are fine as
  static/decorative — collapsing the sidebar is out of scope, don't build real collapse behavior
  unless it's trivial; if you do wire a collapse toggle, keep it simple and don't let it break
  the hidden-elements-still-work requirement above).
- A "New chat" button/row with a pencil-square icon — wire this one for real: clicking it should
  reset the chat view back to its empty state (clear `#chat-log`, clear `#chat-input`, and
  whatever else the existing chat JS needs reset to look like a fresh session — read how the chat
  panel currently transitions between empty/active state, from the prior redesign pass, and drive
  it back to empty deliberately). This does **not** need to actually start a new backend chat
  session/turn — `exo`'s chat is a single ongoing stream today, this is a visual-only reset. State
  that clearly in your report.
- A short static nav list below that (a few rows, icon + label, e.g. things like "Library",
  "Projects" from the reference — since `exo` has no real equivalent features yet, render these as
  plain non-interactive rows, or give them a no-op click handler — do **not** invent fake behavior
  or fake data for them. Comment in the code that they're placeholders pending future features.
- A "Pinned" section and a "Recent" section below that, each with a label and a short static list.
  Same rule: no fake data — either leave these sections' lists empty (just the section header) or
  omit them entirely if an empty list looks broken; use your judgment for what looks least
  unfinished, but don't fabricate chat history that doesn't exist.
- A small account row pinned to the bottom (avatar circle + name), if there's a natural source for
  a name (there isn't, today) — a generic placeholder is fine here, or omit it; again, no fake data
  presented as real.

### 3. Main content area

Center the existing chat empty-state content (headline, large input, suggestion chips — from the
prior redesign pass, keep that copy/behavior as-is) both horizontally and vertically in the
available space to the right of the sidebar. When a conversation is active, same transition
behavior as before (log fills space, input pinned at bottom) — just now happening inside this new
full-bleed dark layout instead of the old boxed-panel one.

Do not build the "Chat / Work" pill tab switcher from the reference — `exo` has nothing to switch
to right now since the terminal view is hidden in this pass. Skip it; note in your report that it
was intentionally omitted, not forgotten.

## What NOT to do

- Do not delete any terminal/session/connection/approval DOM elements, JS functions, WebSocket
  handling, or event listeners — hide the containers, keep everything underneath fully functional
  in the DOM/JS so it can be un-hidden later without re-writing it.
- Do not touch any Go code or backend routes.
- Do not fabricate chat history, pinned items, or a user identity — placeholders must look like
  placeholders (or be omitted), never like real data.
- Do not build sidebar collapse, search, or the nav items' real functionality — decorative/inert
  for this pass, explicitly deferred.

## Verification

`go build ./...` from `~/exo` to confirm the embedded-assets setup still compiles. Real visual
verification happens afterward by running `exo serve` and looking at it — describe clearly what
you changed so that can be checked.

## When done

Report: exact files touched, confirm the terminal/session UI is hidden-but-functional (not
deleted) and how you verified that (e.g. the WebSocket connect logic still runs, session refresh
still fires, just not visible), what "New chat" actually resets, which reference elements you
deliberately left out (Chat/Work tabs, sidebar collapse, etc.) and why, and confirm
`go build ./...` succeeds.
