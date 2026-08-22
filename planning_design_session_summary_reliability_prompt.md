This is a **design-critique round**, same Claude↔Codex pattern used throughout this project — but
narrower than the earlier rounds: one specific, live-reproduced bug in a feature that's already
built and merged (`build_prompt_SESSION_RECALL.md`, `sessionrecall/`, `agenthost/
session_summarize.go`). Not asking you to redesign the session-recall subsystem — that's closed —
just this one call's reliability problem, with two real attempts already tried and their actual
results.

## What's closed and already built — context, not up for debate

- **The session-recall feature** (`planning_design_session_recall_round1_prompt.md`/`_response.md`,
  `build_prompt_SESSION_RECALL.md`) is built and working end-to-end except for this one call:
  token accounting (`TurnResult.TokenDelta`), the 85% context-window banner, the
  `POST /api/chat/sessions/{id}/close` endpoint, the `sessionrecall` store (CAS, one JSON per
  project), and the `session_recall` agent tool (`list`/`get`) are all live and verified against a
  real running `exo serve` with a real LiteLLM-backed provider.
- **`Host.SummarizeSession`** (`agenthost/session_summarize.go`) is the one broken link in that
  chain: `POST .../close` calls it to produce `{title, description, summary_body}` from a session's
  transcript, **before** persisting a `sessionrecall` entry and marking the session closed. Per the
  original design (`planning_design_session_recall_round1_response.md`, gap #3): a separate,
  minimal, tool-less completion call — deliberately not routed through `h.agent`/`h.coordinator`'s
  full agentic loop.
- **The close sequence's fail-safe already works correctly** and is not what this round is about:
  when `SummarizeSession` errors, `POST .../close` returns 500 and the session stays open,
  untouched, no partial/corrupt `sessionrecall` entry gets written. Confirmed live. The problem is
  purely that `SummarizeSession` keeps failing, not that failure is handled badly.
- **Provider setup**: `agenthost/provider.go`'s `buildProviderFromEnv` picks one of Anthropic
  direct / LiteLLM gateway / OpenAI direct based on which env var is set (`CONFIGURING_PROVIDER.md`).
  In this project's live test, `LITELLM_API_KEY` is set, routing through a local LiteLLM gateway
  (`~/pacta-harness/tools/litellm-gateway`) whose `primary` alias goes through `CLIProxyAPI` — a
  proxy in front of an actual Claude Code CLI subscription, not a raw completions API. This may or
  may not be relevant to the actual cause (see below) — don't assume it's the explanation, that
  hypothesis was raised and never confirmed.

## The bug, reproduced live, twice, with evidence

`Host.SummarizeSession` asks the model (system prompt + one user message containing the session's
rendered transcript) to reply with **only** a JSON object (`{title, description, summary_body}`).
Instead, on both attempts below, the model replied with an ordinary short conversational sentence,
completely ignoring the instruction — `StopReason` was `end_turn` (normal completion, not truncated,
not an error) both times.

**Attempt 1 — original code, bare task-only system prompt:**
```go
resp, err := h.provider.Send(ctx, summarizeSystemPrompt, req, nil)
```
where `summarizeSystemPrompt` was a short, standalone string — no `exo` identity, no
`yeyo.RenderCentro()`, nothing else. Result:
```
RAW="¿Hay algo más en lo que pueda ayudarte?"
```

**Attempt 2 — after finding that every provider's `Send` only falls back to its own baked-in
default system prompt (`p.system`, which *is* the full identity+`yeyo.RenderCentro()`+periferia
prompt — confirmed in `anthropic.go:96`, `openai.go:115`, and LiteLLM is an `OpenAIProvider`
underneath, `litellm.go:40`) when the caller passes `system == ""` — and that passing a non-empty
custom string (as attempt 1 did) silently discards that entire default, no fallback at all — the
fix tried was prepending `h.agent.System` (the live, current, full prompt every ordinary turn
already uses) ahead of the task instructions:**
```go
system := h.agent.System + "\n\n" + summarizeTaskPrompt
resp, err := h.provider.Send(ctx, system, req, nil)
```
Result — same failure mode, not fixed:
```
STOP=end_turn
CONTENT_BLOCKS=1
RAW=¿En qué puedo ayudarte?
```

**Live hypothesis after attempt 2, not yet tested**: `h.agent.System`'s base identity line is
*"You are exo's integrated coding agent. Help the user from the browser chat..."* — and the user
message this call sends looks like a rendered chat transcript (`api.RenderTranscript(messages)`,
literal lines resembling a back-and-forth conversation). The identity block may be actively working
*against* the task here: it primes the model to treat the transcript as something to *respond to*
(continue the chat) rather than data to *process and report on* — meaning attempt 2's fix, while
correcting a real and separate bug (silently discarding the default system prompt is wrong
regardless of whether it explains this specific failure), may have made this specific symptom no
better, or arguably reinforced the wrong framing. This is a live hypothesis, not confirmed —
dig into whether it holds.

## The constraint found while scoping a fix — real, verified

**There is no `tool_choice`/forced-tool-call lever anywhere in `nucleo-base`'s provider
abstraction.** Checked `Send(ctx context.Context, system string, messages []api.Message, tools
[]api.ToolDef) (api.Response, error)` in all three providers
(`layer2-runtime-rails/provider/{anthropic,openai,litellm}.go`) — `tools` get passed through to the
underlying SDK call, but nothing sets `tool_choice`/equivalent to force a specific tool; the model
always has the option to reply in plain text instead of calling any tool given to it. `Send` itself
already parses `BlockToolUse` blocks out of a one-shot (non-agentic-loop) response correctly (see
`openai.go`'s `Send`, ~line 148: `for _, tc := range choice.Message.ToolCalls { ... }`) — so tool
calling *works* structurally for a single `Send` call outside the full agent loop, it's just not
forceable.

## What I'm about to try, unless you argue for something better

Give the call one `submit_session_summary` tool def (`{title, description, summary_body}` schema,
matching `sessionSummary`'s fields) via `Send`'s `tools` param, instead of asking for free-text
JSON:
1. If the model calls it (`resp.StopReason == api.StopToolUse`), extract the first `BlockToolUse`
   block's `ToolInput` directly — already-valid JSON per the provider's own schema handling, no
   text-scraping needed.
2. If it doesn't (still just replies with text, since nothing forces the tool), fall back to the
   existing `parseSessionSummary` text-extraction on whatever text came back.
Framed to the human as a probability improvement (tool-calling reliably triggers structured
decoding in most models even without forcing), explicitly **not** a guarantee, given the missing
`tool_choice` lever above.

## What I want back

1. **Does the tool-calling-with-text-fallback plan above actually address the observed failure**,
   or is it solving the wrong layer given the live hypothesis about the identity/transcript framing
   fighting the task? Take a real position, including whether tool-calling would plausibly help
   *regardless* of that framing problem (tool-calling might route the model into a different
   internal mode entirely, sidestepping the "is this a chat to continue" confusion) — or whether the
   framing problem needs fixing on its own first, independent of text-vs-tool output format.
2. **Is prepending `h.agent.System` (attempt 2) actually correct engineering, independent of whether
   it fixes this specific symptom?** I believe yes — silently discarding a provider's own default
   system prompt because a caller passed a non-empty override is a real inconsistency other call
   sites in this codebase don't have, and it's worth keeping fixed on principle. Confirm or push
   back.
3. **Is the identity block ("Help the user from the browser chat...") actually the active
   ingredient causing the conversational-reply failure**, or is that a plausible-sounding story that
   doesn't hold up? If you have a way to reason about this more rigorously than "test another prompt
   variant and see," say so.
4. **Any structural alternative neither of us has tried** — e.g., restructuring the request so the
   transcript isn't rendered in a chat-like shape at all (stripping role labels, presenting it as a
   log/document instead of a conversation), a completely separate/narrower system prompt built
   specifically for this call (not `h.agent.System`, not the original bare string, something
   in-between purpose-built for "process this data, don't converse"), or something about how
   `CLIProxyAPI`-routed models in particular need to be prompted differently than raw completions
   APIs (if you have real signal on that, not speculation) — argue for whichever you think is
   actually right, don't just enumerate options back.
5. Whether extending `nucleo-base`'s `Send` signature to support real `tool_choice` forcing is worth
   proposing as a follow-up to that shared repo (used by `avengers` and others too, not just `exo`)
   given this failure mode, or whether that's over-scoped for what's actually a narrow, low-stakes
   call (a session summary, not a safety-critical path).
