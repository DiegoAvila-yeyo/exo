# Codex verification prompt — "Projects" sidebar workspace plan

You are auditing a plan **before** any implementation starts. Do not write,
edit, or commit any code. Your job is to read the current codebase at
`/Users/eltitoyeyo/exo` and produce a written report answering the questions
below — nothing else.

## Context: what exists today

`exo` is a Go backend (`termserver/`, `agenthost/`, `chatstore/`,
`projects/`) + vanilla JS/HTML/CSS frontend (`termserver/assets/`) serving a
ChatGPT-style agent UI at `http://127.0.0.1:45873`.

Recently implemented (already working, verified live):

- `chatstore.ChatSession` has a `ProjectPath string` field (`chatstore/store.go`),
  persisted per chat session as one JSON file.
- `POST /api/chat` accepts an optional `project_path` in its JSON body
  (`termserver/chat.go`, `handleChat`). If provided, it's saved onto the
  session; if omitted on a later message, the session keeps whatever
  `ProjectPath` it already had.
- `agenthost.Host.SetRootPath(path string) error` (`agenthost/host.go`)
  `os.Chdir`s the process into `path` and rebuilds the agent's system prompt
  via `instructions.Load(path)`, so the project's own `AGENTS.md` /
  `.harness/instructions/*.md` get re-read and injected fresh.
- `backend/backend.go`'s `runner` closure calls `host.SetRootPath(projectPath)`
  before every turn, using whatever came through from the request.
- `projects.Scan(root string) ([]Project, error)` (`projects/scan.go`) lists
  first-level folders under a root (currently `$HOME`) that look like real
  projects (have `.git`, `go.mod`, `package.json`, etc.), most-recently-modified
  first. Served at `GET /api/projects`.
- Frontend: the chat composer (`termserver/assets/index.html` /
  `app.js` / `app.css`) currently has a "Seleccionar proyecto" row attached
  directly below the input box. Clicking it opens a dropdown built from
  `GET /api/projects`; picking one sets `state.selectedProjectPath` and sends
  it as `project_path` on the next `POST /api/chat`. This selection is
  effectively per browser-tab-session right now, re-applied to whichever
  chat session is active.
- The sidebar's "Recent" section (`#chat-session-list`) already lists ALL
  chat sessions via `GET /api/chat/sessions`, unfiltered, sorted by most
  recently updated. Clicking a row calls `GET /api/chat/sessions/{id}` and
  loads that session's transcript.
- The sidebar has a "Projects" nav button (`.sidebar-nav-item`, next to
  "Library" and "Automations") that is currently **decorative** — no click
  handler, does nothing.

## The plan we want verified (do NOT build it — just check feasibility)

1. Make the sidebar's "Projects" nav item functional: clicking it expands a
   list of real projects (same `GET /api/projects` data), the way "Recent"
   already expands/collapses today.
2. Picking a project there marks it "active" (persisted in the browser,
   e.g. localStorage) for the whole app — not per chat session anymore.
3. While a project is active, the sidebar's "Recent" list should be
   filterable to **only that project's chat sessions**.
4. "New chat" created while a project is active should be tagged with that
   project's path automatically (no per-message picking needed).
5. Remove the "Seleccionar proyecto" row from the chat composer entirely —
   the composer goes back to just input + the on/off switch + send button.
6. Backend storage/`chdir`/rules-rereading logic is assumed to need **no
   changes** — only the frontend's source of truth for "which project" and
   how the session list gets filtered should change.
7. Old chat sessions that have no `ProjectPath` set should keep showing up
   in the general/unfiltered view, untouched.

## What to check and report back

For each item, say whether the current codebase already supports it, what's
missing, and where (file + line references):

1. **Gap check**: does `GET /api/chat/sessions` (`chatstore.ChatSessionSummary`,
   `termserver/chatsessions.go` `handleChatSessions`) currently include
   `project_path` in its response? If not, confirm this is required to filter
   "Recent" by project client-side without doing N detail-fetches, and note
   exactly what struct/field needs to change.
2. Confirm `chatstore.Store.List()` has everything needed to filter/group by
   `ProjectPath` server-side too, in case filtering server-side (via a query
   param like `GET /api/chat/sessions?project_path=...`) turns out cleaner
   than client-side filtering. Recommend which approach fits the existing
   code style better (see how `handleChatSessions` and `Store.List` are
   structured today).
3. Confirm there is no existing concept of a "global active project" state
   anywhere in the Go backend that would conflict with making this purely a
   frontend/localStorage concern (it shouldn't need to be — `project_path` is
   already sent per-request — just confirm).
4. Check `app.js` for what would need to move/change: the existing
   `openProjectList` / `renderProjectList` / `selectProject` /
   `setSelectedProject` functions currently target `#chat-project-select` /
   `#chat-project-list` inside the composer. Identify exactly which DOM refs,
   CSS classes, and JS functions can be reused as-is for a sidebar version
   vs. what needs new markup (e.g. mirroring `#chat-section-toggle` /
   `#chat-session-list`'s expand/collapse pattern for a new
   `#project-section-toggle` / `#project-list` in the sidebar).
5. Flag anything else risky or ambiguous about doing this that isn't
   addressed above — but do not propose solutions beyond noting the
   question, and do not write any code.

## Output format

A single markdown report, section per numbered question above, each ending
with a clear "supported today / needs X" verdict. No code changes, no new
files besides this report (save it as `codex_verify_projects_sidebar_report.md`
in the repo root).
