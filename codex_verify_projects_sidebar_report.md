# Codex verification report: "Projects" sidebar workspace plan

This report audits the current `exo` codebase exactly against the requested pre-implementation check. No application code was modified during this audit.

## 1. Gap check: does `GET /api/chat/sessions` include `project_path` today?

No. The list endpoint currently returns `chatstore.ChatSessionSummary`, and that summary type does not carry `ProjectPath`.

Evidence:

- [`chatstore/store.go:45`](./chatstore/store.go#L45) defines `ChatSessionSummary` with only `ID`, `Title`, `CreatedAt`, and `UpdatedAt`.
- [`chatstore/store.go:112`](./chatstore/store.go#L112) `Store.List()` reads full sessions from disk, but only appends those four fields into each summary at [`chatstore/store.go:132`](./chatstore/store.go#L132).
- [`termserver/chatsessions.go:54`](./termserver/chatsessions.go#L54) `handleChatSessions` returns `s.chatStore.List()` directly on `GET /api/chat/sessions`.
- By contrast, the detail endpoint does include `project_path`: [`termserver/chatsessions.go:125`](./termserver/chatsessions.go#L125) to [`termserver/chatsessions.go:130`](./termserver/chatsessions.go#L130).

Implication:

- If the frontend wants to filter the sidebar's "Recent" list by project client-side without N extra detail requests, `project_path` needs to be present in the `GET /api/chat/sessions` response.
- The concrete missing change is to add `ProjectPath string \`json:"project_path,omitempty"\`` to `chatstore.ChatSessionSummary` and populate it from `session.ProjectPath` inside `Store.List()`.

Verdict: **Needs `project_path` added to `ChatSessionSummary` and `Store.List()` output; not supported today.**

## 2. Can `chatstore.Store.List()` support server-side filtering too, and which approach fits better?

Yes, the store already loads enough data to support server-side filtering by `ProjectPath`.

Evidence:

- Full persisted sessions include `ProjectPath` in [`chatstore/store.go:32`](./chatstore/store.go#L32) to [`chatstore/store.go:42`](./chatstore/store.go#L42).
- `Store.List()` already reads every session JSON file via `s.readLocked(id)` at [`chatstore/store.go:127`](./chatstore/store.go#L127) to [`chatstore/store.go:128`](./chatstore/store.go#L128), so `session.ProjectPath` is already available in memory during listing.
- `handleChatSessions` is intentionally thin and delegates data shaping to the store through the `chatSessionStore` interface at [`termserver/chatsessions.go:11`](./termserver/chatsessions.go#L11) to [`termserver/chatsessions.go:19`](./termserver/chatsessions.go#L19).

What is missing for server-side filtering:

- The `chatSessionStore` interface currently exposes only `List() ([]chatstore.ChatSessionSummary, error)` at [`termserver/chatsessions.go:14`](./termserver/chatsessions.go#L14) to [`termserver/chatsessions.go:19`](./termserver/chatsessions.go#L19).
- `handleChatSessions` does not read any query parameters before calling `List()` at [`termserver/chatsessions.go:54`](./termserver/chatsessions.go#L54).
- So server-side filtering would require either changing `List()` to accept a filter or adding a new store method.

Recommendation on style fit:

- For this specific plan, client-side filtering fits the current code style better with the smallest surface-area change: the sidebar already fetches one full session list in `refreshChatSessions()` at [`termserver/assets/app.js:308`](./termserver/assets/app.js#L308) to [`termserver/assets/app.js:316`](./termserver/assets/app.js#L316), and `renderChatSessionList()` already renders from in-memory state at [`termserver/assets/app.js:318`](./termserver/assets/app.js#L318) to [`termserver/assets/app.js:339`](./termserver/assets/app.js#L339).
- Server-side filtering is still feasible and structurally clean because the store already has the necessary data, but it would require new API/store plumbing that does not exist today.

Verdict: **Supported in principle by current store internals, but server-side filtering needs new API/store plumbing; client-side filtering from enriched summaries fits the current implementation style better.**

## 3. Is there already a conflicting backend concept of a "global active project"?

No explicit backend concept of a user-level or app-level "global active project" exists today.

Evidence:

- The only persisted project association on chats is per session: `ChatSession.ProjectPath` in [`chatstore/store.go:39`](./chatstore/store.go#L39) to [`chatstore/store.go:42`](./chatstore/store.go#L42).
- `POST /api/chat` accepts optional `project_path` per request body at [`termserver/chat.go:212`](./termserver/chat.go#L212) to [`termserver/chat.go:216`](./termserver/chat.go#L216), and only copies it onto the session when present at [`termserver/chat.go:243`](./termserver/chat.go#L243) to [`termserver/chat.go:248`](./termserver/chat.go#L248).
- The runner receives `sess.ProjectPath` per turn at [`termserver/chat.go:257`](./termserver/chat.go#L257) to [`termserver/chat.go:263`](./termserver/chat.go#L263).
- The backend server's `projectRoot` field in [`termserver/server.go:71`](./termserver/server.go#L71) to [`termserver/server.go:73`](./termserver/server.go#L73) is only the filesystem root used by `GET /api/projects`, not an active project selection.
- `WithProjectRoot` only wires scanning for the projects endpoint at [`termserver/server.go:118`](./termserver/server.go#L118) to [`termserver/server.go:130`](./termserver/server.go#L130).

Frontend note:

- There is already a browser-local selection persisted in `localStorage` under `exo.selectedProjectName` at [`termserver/assets/app.js:45`](./termserver/assets/app.js#L45), written by `setSelectedProject()` at [`termserver/assets/app.js:459`](./termserver/assets/app.js#L459) to [`termserver/assets/app.js:466`](./termserver/assets/app.js#L466). But opening a stored chat currently overwrites that local value from the session's own `project_path` at [`termserver/assets/app.js:351`](./termserver/assets/app.js#L351) to [`termserver/assets/app.js:360`](./termserver/assets/app.js#L360), so it is not truly a global independent app state yet.

Verdict: **Supported today from a backend-conflict perspective; there is no existing backend global-project model that blocks making this a frontend/localStorage concern.**

## 4. What in `app.js` can be reused, and what markup/CSS is missing for a sidebar version?

### Reusable JavaScript pieces

These functions are already reusable in behavior, but are currently hard-wired to composer DOM nodes:

- `openProjectList()` fetches `GET /api/projects` and calls `renderProjectList()` at [`termserver/assets/app.js:384`](./termserver/assets/app.js#L384) to [`termserver/assets/app.js:395`](./termserver/assets/app.js#L395).
- `renderProjectList()` builds clickable project items from the fetched list at [`termserver/assets/app.js:430`](./termserver/assets/app.js#L430) to [`termserver/assets/app.js:454`](./termserver/assets/app.js#L454).
- `selectProject()` and `setSelectedProject()` already update in-memory state plus `localStorage` at [`termserver/assets/app.js:459`](./termserver/assets/app.js#L459) to [`termserver/assets/app.js:470`](./termserver/assets/app.js#L470).
- `restoreSelectedProjectLabel()` already restores the locally saved project pick on boot at [`termserver/assets/app.js:473`](./termserver/assets/app.js#L473) to [`termserver/assets/app.js:485`](./termserver/assets/app.js#L485).

### Current DOM refs tied to the composer

The project UI is currently bound to these composer-specific refs:

- `chatProjectSelect`, `chatProjectSelectLabel`, and `chatProjectList` are grabbed at [`termserver/assets/app.js:38`](./termserver/assets/app.js#L38) to [`termserver/assets/app.js:40`](./termserver/assets/app.js#L40).
- Click wiring for the composer button is at [`termserver/assets/app.js:243`](./termserver/assets/app.js#L243) to [`termserver/assets/app.js:250`](./termserver/assets/app.js#L250).
- Outside-click closing assumes the floating composer dropdown at [`termserver/assets/app.js:252`](./termserver/assets/app.js#L252) to [`termserver/assets/app.js:256`](./termserver/assets/app.js#L256).
- `positionProjectList()` is specifically built for a floating popover anchored to the composer button at [`termserver/assets/app.js:401`](./termserver/assets/app.js#L401) to [`termserver/assets/app.js:423`](./termserver/assets/app.js#L423).

### Current markup that would be removed or replaced

- The composer project row exists only in the chat panel markup at [`termserver/assets/index.html:142`](./termserver/assets/index.html#L142) to [`termserver/assets/index.html:149`](./termserver/assets/index.html#L149).

### Existing sidebar pattern that can be mirrored

- The working expand/collapse pattern for "Recent" already exists in sidebar markup at [`termserver/assets/index.html:47`](./termserver/assets/index.html#L47) to [`termserver/assets/index.html:61`](./termserver/assets/index.html#L61).
- The toggle behavior is already driven by `chatSectionToggle` at [`termserver/assets/app.js:231`](./termserver/assets/app.js#L231) to [`termserver/assets/app.js:234`](./termserver/assets/app.js#L234) and `setChatSectionExpanded()` at [`termserver/assets/app.js:289`](./termserver/assets/app.js#L289) to [`termserver/assets/app.js:292`](./termserver/assets/app.js#L292).
- CSS for that expand/collapse relationship is currently specific to `.chat-session-list` at [`termserver/assets/app.css:216`](./termserver/assets/app.css#L216) to [`termserver/assets/app.css:218`](./termserver/assets/app.css#L218).

### What new markup/CSS would still be needed

- The "Projects" nav button is decorative only today; it has no `id`, no target container, and no handler in JS. The relevant markup is just the plain nav button at [`termserver/assets/index.html:30`](./termserver/assets/index.html#L30) to [`termserver/assets/index.html:33`](./termserver/assets/index.html#L33).
- There is no sidebar project container equivalent to `#chat-session-list`, so a new section such as `#project-section-toggle` plus `#project-list` would need to be introduced in markup.
- There are no sidebar-specific refs in `app.js` for a projects section today.
- CSS exists for `.chat-session-list` and `.chat-session-item` at [`termserver/assets/app.css:228`](./termserver/assets/app.css#L228) to [`termserver/assets/app.css:260`](./termserver/assets/app.css#L260), but the current project picker uses separate composer-specific classes such as `.chat-project-list`, `.chat-project-option`, and `.chat-project-empty` elsewhere in the stylesheet, so either those classes would need repurposing or a sidebar-specific style would need to be added.

Verdict: **Data-fetching and selection logic are partly reusable, but the current project UI is composer-bound; a sidebar projects section needs new markup, new DOM refs, and some JS/CSS retargeting.**

## 5. Additional risks or ambiguities not covered above

### A. "No backend changes needed" is not fully safe if client-side filtering uses only the existing list API

- As noted in section 1, `GET /api/chat/sessions` does not currently expose `project_path`, so the exact plan assumption in item 6 is incomplete if client-side filtering is the intended route.

### B. Empty-project sessions may inherit the previous turn's working directory at runtime

- `backend` calls `host.SetRootPath(projectPath)` before every turn at [`backend/backend.go:153`](./backend/backend.go#L153) to [`backend/backend.go:169`](./backend/backend.go#L169).
- `Host.SetRootPath("")` is a no-op at [`agenthost/host.go:146`](./agenthost/host.go#L146) to [`agenthost/host.go:149`](./agenthost/host.go#L149).
- That means a turn for a session with empty `ProjectPath` does not actively reset the process back to the original root after a previous turn changed it.

This does not block the sidebar plan mechanically, but it is an important behavioral ambiguity relative to the assumption that old sessions with no `ProjectPath` are simply "general/unfiltered" and untouched.

### C. Current "selected project" behavior is explicitly session-coupled in the UI

- `startNewChat()` clears the selected project at [`termserver/assets/app.js:345`](./termserver/assets/app.js#L345) to [`termserver/assets/app.js:348`](./termserver/assets/app.js#L348).
- `openChatSession()` replaces the local project selection with that session's stored `project_path` at [`termserver/assets/app.js:351`](./termserver/assets/app.js#L351) to [`termserver/assets/app.js:360`](./termserver/assets/app.js#L360).

So the current UI model is not just "project picker in the composer"; it is also implicitly "active chat decides current selected project." That coupling would need to be deliberately unwound.

### D. One storage key name is misleading for the new model

- The browser key is named `exo.selectedProjectName` at [`termserver/assets/app.js:45`](./termserver/assets/app.js#L45), but the stored value includes both `name` and `path` via JSON at [`termserver/assets/app.js:463`](./termserver/assets/app.js#L463).

That is not a blocker, but it is an ambiguity worth noting because the new design treats the project as app-wide source of truth rather than just a label.

Verdict: **The plan is broadly feasible, but there are two real gaps/ambiguities beyond the prompt itself: summary responses lack `project_path`, and empty-project sessions may not truly reset to a neutral backend root after a previously project-scoped turn.**
