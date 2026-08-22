Build "Sesiones" — the session-recall subsystem designed in
`planning_design_session_recall_round1_prompt.md` / `_response.md`. Real build task, Go + frontend,
`go build ./...` / `go test ./...` clean (`go vet` too). This is a new subsystem, not a redesign of
anything that exists — read the two docs above first, they're the actual spec; this file translates
their decisions into concrete pieces with file anchors, it doesn't re-argue them.

## What's already true, don't re-litigate

- **Two separate systems.** "Memorias de la IA" (`nucleo-base` Layer 4, `agenthost/host.go`'s
  `openMemoryStoreBestEffort`) is done and untouched by this build. This build is only the second
  system: session summaries + recall.
- **Sessions are currently fully isolated** (`termserver/chat.go` loads/creates by exact ID, nothing
  reads across sessions) and **stay isolated** — this build adds a pull-based recall *tool*, never
  automatic cross-session injection.
- **Store shape is decided**: one JSON per project, CAS-protected, same pattern as `canvasstore`
  (`canvasstore/store.go`'s `Load`/`Save` — read `onDisk.Version`, compare, bump on success, return
  `ErrStaleVersion` on conflict). Copy that pattern, don't invent a new one.
- **Tool contract is decided**: `session_recall` tool, `list`/`get` only — mirrors
  `agenthost/atom_tool.go` exactly (same two-action shape, same "list returns names+one-line
  descriptions, get fetches one full body" split). `get` returns **summary + metadata only, never
  the raw transcript** — the raw `chatstore` transcript is human/manual backup, not an agent-
  reachable escape hatch. Do not add a third action that reaches into `chatstore`.
- **Project scope comes from the app, never the model** — same principle Canvas's
  `canvasCell.projectID` already enforces (`agenthost/canvas_context.go`, refreshed every turn by
  `Host.BeginTurn`, `agenthost/planning_navigate.go:75`: `h.canvasCell.projectID =
  h.currentRootPath`). `session_recall`'s `list` must scope to that same value, not to anything the
  tool call's arguments claim.
- **Closed is terminal.** No "reopen and continue" on the same session ID. A closed session stays
  readable (transcript untouched in `chatstore`), gets a visual "Closed" marker, sorts below open
  sessions within its project group in the sidebar (`termserver/assets/app.js`'s
  `renderProjectList`/`buildSessionsListElement`, from the 2026-08-21 UI round). Continuing means
  opening a brand-new session.
- **Close sequence is `generate summary → persist recall entry → mark chatstore session closed`,
  and closing is idempotent** — retrying the sequence after a partial failure (e.g. process died
  between persisting the recall entry and flipping the `chatstore` status) must be safe to run
  again without duplicating the recall entry (same `session_id` key — an upsert, not an append) and
  must still succeed in reaching `closed` on the `chatstore` side. If summary generation itself
  fails, the session stays open — never mark closed without a persisted summary first.
- **The token metric is last-turn, not cumulative.** `nucleo-base/layer2-runtime-rails/runtime/
  coordinator.go` already computes exactly the right number and exposes it —
  `TurnResult.TokenDelta` (`coordinator.go:232`: `(after.InputTokens + after.OutputTokens) -
  (tokensBefore.InputTokens + tokensBefore.OutputTokens)`, computed around the single
  `Agent.SendWithHooks` call for *that turn only*). **`agenthost/host.go:404` currently discards
  this** (`_, err = h.coordinator.Run(ctx, coordinatorInput)`) — the fix is to stop discarding it,
  not to build new token accounting from scratch. Do not sum `TokenDelta` across turns for the
  context-window estimate; each value already stands alone as "tokens the window holds right now."

## New wrinkle found while writing this prompt — not covered by the design round, decide sensibly

**Context window size isn't uniformly resolvable across providers.**
`nucleo-base/layer2-runtime-rails/provider/catalog.go`'s `FetchModels` — the only place `ContextLen`
comes from — only works against a LiteLLM gateway (`GET {LITELLM_BASE_URL}/model/info`). Direct
`ANTHROPIC_API_KEY`/`OPENAI_API_KEY` providers (`agenthost/provider.go`'s `buildProviderFromEnv`,
see `CONFIGURING_PROVIDER.md`) have no catalog endpoint to query at all. Resolve this the same
best-effort way `openMemoryStoreBestEffort` already handles its own failure mode (log, degrade,
never fail startup):
1. If `LITELLM_API_KEY`/`LITELLM_BASE_URL` are the active provider, best-effort `FetchModels` once
   at `Host` construction (not per-turn) and look up the configured model's `ContextLen`.
2. Otherwise (or if that fetch fails), fall back to a small static table keyed by model-name
   substring for the models `CONFIGURING_PROVIDER.md` already documents as defaults
   (`claude-sonnet-4-6`, `gpt-5-codex`) plus obvious siblings (other `claude-*`, `gpt-5*`,
   `gemini-*` patterns) — same spirit as `provider/catalog.go`'s existing `qualityPatterns`
   substring-matching table, don't invent a different matching style.
3. If truly unresolvable, use a conservative generic default (200,000) rather than disabling the
   meter entirely — being approximately right beats not showing anything.

## 1. `appconfig.SessionRecallStoreDir()`

Same pattern as `CanvasStoreDir()` (`appconfig/config.go:66`) — new directory under
`AppSupportDir()`, e.g. `session_recall/`. Test mirrors the existing `TestCanvasStoreDirUnder...`
style.

## 2. New package `sessionrecall`

Mirror `canvasstore`'s file layout (`model.go` + `store.go` + tests):

- `Store.Load(projectID string) (ProjectRecall, error)` — auto-creates an empty
  `ProjectRecall{Version:0}` in memory when no file exists, same as `canvasstore.Load`.
- `Store.Save(pr ProjectRecall) (ProjectRecall, error)` — CAS on `Version`, `ErrStaleVersion` on
  conflict, identical shape to `canvasstore.Save`.
- `ProjectRecall{ProjectID, Entries []SessionSummary, Version, CreatedAt, UpdatedAt}`.
- `SessionSummary{SessionID, Title, Description, SummaryBody, ClosedAt, ModelID,
  ContextPctAtClose, Status, Supersedes}` — per the design round's decision #4. `Status` starts as
  a single value (there's no draft/materialized-style lifecycle here, unlike Canvas — an entry
  exists once the session is closed, full stop) but keep the field for future use rather than
  omitting it, since `Supersedes` implies entries can be superseded later (re-summarization), which
  needs somewhere to record that without deleting the old entry (`canvasstore`'s
  `Supersedes`-chain precedent — never mutate, append and point back).
- Upsert by `SessionID` on `Save` (closing sequence must be idempotent — see above): if an entry
  for that `SessionID` already exists and isn't itself being superseded, treat a repeat close as a
  no-op success, not a duplicate append.

## 3. Capture the turn's token delta in `Host.Run`

`agenthost/host.go:404` — capture the `TurnResult` instead of discarding it:
```go
result, err := h.coordinator.Run(ctx, coordinatorInput)
```
Thread `result.TokenDelta` (plus the model's resolved `ContextLen`, from piece 0 above) into
whatever persists it — see piece 4. `Host` needs a way to report "this turn's usage" back up to
`backend.go`'s runner (which already threads other per-turn outputs like the Canvas suggestion and
navigate action back to `termserver` — follow that existing plumbing shape, e.g. a new return value
or a `Host` getter read right after `Run` returns, matching how `TakeNavigateAction` already works
post-`Run`).

## 4. Persist per-session usage in `chatstore.ChatSession`

`chatstore/store.go:32` — add fields per the design round's decision #1:
```go
LastTurnTokens      int    `json:"last_turn_tokens,omitempty"`
ContextWindowTokens int    `json:"context_window_tokens,omitempty"`
ModelID             string `json:"model_id,omitempty"`
Status              string `json:"status,omitempty"` // "" (open) or "closed"
```
Written on every `Save` call from the chat handler (`termserver/chat.go`), same place `Messages`/
`Entries` already get updated after a turn. Compute the percentage client-side or server-side from
`LastTurnTokens`/`ContextWindowTokens` — either is fine, pick whichever keeps `ChatSessionSummary`
(the sidebar's lightweight list shape) simple; the sidebar doesn't need the percentage today, only
the active session's own view does.

## 5. Surface the percentage to the frontend + the 85% warning

Expose `LastTurnTokens`/`ContextWindowTokens` (or a precomputed percentage) on whatever the chat
already returns per turn — the SSE `done` event (`termserver/chat.go`, `handleChatEvent`'s `"done"`
case in `app.js`) is the natural spot, matching how canvas-suggestion detection already piggybacks
on turn completion. In `app.js`, on `"done"`: if `pct >= 0.85`, show a banner recommending closing
the session — reuse `chat-feedback`'s existing pattern (`setChatFeedback`,
`termserver/assets/app.css`'s `.chat-feedback`) rather than inventing new banner chrome, or a
dedicated small banner styled like `canvas-suggest-banner` if a persistent (not auto-dismissing)
control makes more sense here — your call, but reuse an existing banner pattern, don't invent a
third one.

## 6. "Close & summarize" action

New endpoint, e.g. `POST /api/chat/sessions/{id}/close` (`termserver/chat.go`, same routing style as
the existing `/api/chat/sessions/{id}` handlers) that runs the sequence from "what's already true"
above:
1. Run a **separate summarization call** (per the design round's decision #3 — not folded into the
   turn that triggered the warning) — a fresh, minimal agent/provider call over the session's
   transcript + metadata, producing `{title, description, summary_body}`. Keep this call narrowly
   scoped (summarization only, no tools) rather than reusing the full `agent.Agent`/`Coordinator`
   machinery built for actual coding turns.
2. `sessionrecall.Store.Save` the resulting `SessionSummary` (upsert by `SessionID`).
3. Only after that succeeds, `chatStore.Save` the session with `Status: "closed"`.
4. If step 1 or 2 fails, return an error and leave the session open — never reach step 3 without a
   persisted summary.

Wire a "Close" control in `app.js`'s chat panel (near the existing chat-feedback/status UI) that
calls this endpoint.

## 7. `session_recall` agent tool

New file `agenthost/session_recall_tool.go`, structurally mirroring `agenthost/atom_tool.go`:
```
session_recall:
  action: "list" | "get"  (required)
  session_id: string      (required when action is "get")
```
- `"list"` — reads the active project's `sessionrecall.ProjectRecall` (scoped via
  `h.canvasCell.projectID`/`h.currentRootPath`, never from tool input) and returns each closed
  session's `title`/`description` (not the full `summary_body` — mirrors `atom`'s list returning
  name+description, not full bodies).
- `"get"` — requires `session_id`, returns that entry's `summary_body` + metadata
  (`closed_at`, `model_id`, `context_pct_at_close`). Reject (with a clear error, same style as
  `atomTool`'s "not found" message) a `session_id` not present in the active project's recall store
  — never fall through to reading `chatstore` directly.
- Register it in the tool registry the same place `atomTool{}` is registered (`agenthost/host.go`
  — grep for where `atomTool{}` gets added to `h.registry`).

## 8. Sidebar: closed sessions

`termserver/assets/app.js`'s `buildSessionsListElement` (2026-08-21 UI round) — sort closed
sessions after open ones within each project group's list, and give closed items a "Closed" badge
(reuse `.canvas-object-card__badge`-style chrome or add a small dedicated class — your call, keep
it visually quiet, this is metadata not a call to action). Clicking a closed session still opens it
read-only via the existing `openChatSession` path — per "what's already true," don't block viewing,
only block continuing (piece 9).

## 9. Block continuing a closed session

Wherever the chat form submit handler (`app.js`, `refs.chatForm`'s `"submit"` listener) posts to
`/api/chat`, and server-side in `termserver/chat.go`'s handler: reject (client-side disable the
input with a clear message, and server-side return an error) a new message targeting a session
whose `Status == "closed"`. The human's path to continue is opening a new session, not this one.

## Tests to write

1. `appconfig`: `TestSessionRecallStoreDirUnderAppSupportDir`, matching existing style.
2. `sessionrecall`: `TestSaveAndLoadRoundTrip`, `TestSaveRejectsStaleVersion`,
   `TestSaveUpsertsBySessionIDIdempotently` (closing twice with the same `SessionID` doesn't
   duplicate the entry).
3. `agenthost`: `TestSessionRecallToolListScopesToActiveProject` (two projects' entries, list from
   one only returns its own), `TestSessionRecallToolGetRejectsUnknownSessionID`,
   `TestSessionRecallToolGetNeverReturnsRawTranscript` (asserts the returned string only contains
   summary fields, not anything sourced from `chatstore.ChatSession.Messages`/`Entries`).
4. `termserver`: `TestCloseSessionSequenceLeavesSessionOpenWhenSummaryFails` (force the
   summarization call to error, assert `chatStore` status is still open and no `sessionrecall` entry
   was written), `TestCloseSessionIsIdempotent` (call close twice, assert one recall entry, session
   ends up closed both times without error), `TestClosedSessionRejectsNewMessages`.
5. `agenthost`: `TestHostRunCapturesTurnResultTokenDelta` (replaces the discarded `_,` at
   `host.go:404` — assert the value actually reaches wherever piece 3/4 threads it).

Report back what actually got built vs. deferred, same as every other `build_prompt_*` in this
repo — if any piece turns out bigger or smaller than scoped here once you're in the code, say so
rather than silently reshaping the piece.
