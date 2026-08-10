This is round 3 of the M8 design-critique conversation. Rounds 1-2 are closed — read
`~/exo/M8_INTEGRATION_DESIGN.md` first, it's the canonical consolidated state (do not re-read the
individual round1/round2 prompt/response files unless you need a specific citation this doc
doesn't have). Your own round 2 verdict was "ready for build prompts, round 3 optional" — this
round exists only to close the 3 items you listed as open, so nothing gets silently deferred into
the build prompts with an ambiguous answer. Still planning only, nothing gets built.

## The 3 open questions — give a definitive v1 answer to each, not another "could go either way"

### 1. Structured `session_id` on approval SSE events: widen the callback, or keep prompt-parsing?

Round 2's answer was: keep `tool.RequestToolApproval(prompt, detail string) bool` unchanged, and
have `termserver` best-effort parse `session_id` out of the known `terminal_write` prompt string
(`"approve write to interactive terminal <sessionID>?"`), omitting the field if parsing fails.

Decide definitively: is prompt-parsing an acceptable v1 answer, or is it too fragile? Consider
concretely — not abstractly — what happens when parsing fails: the approval banner in the browser
UI shows a prompt with no session correlation. In a single-session turn this is harmless (there's
only one session it could be). In a multi-session turn (round 1 explicitly kept multi-session
agent turns allowed), an approval with no `session_id` is ambiguous to the human — which terminal
is this about? Is that ambiguity acceptable for v1 (single most-common-case: agent usually drives
one shell-like session per turn in practice), or does it justify widening
`tool.SetGlobalApproveFunc`'s signature to `func(prompt, detail string, meta map[string]string) bool`
(or similar structured metadata) now, before the build prompt is written, since retrofitting a
callback signature after tools are built against the old one is more expensive than deciding now?
Give one answer and defend it with the actual failure mode, not a hedge.

### 2. Should browser-created (human) sessions get an explicit `InitialOwner:"human"`?

Today, plain `Create(workdir, name)` (browser session creation) relies on `ptyactor`'s existing
default owner. Round 1 noted `defaultOwner = "agent"` in `ptyactor/session.go`. Read that file
again now that `CreateWithOptions`/`InitialOwner` exists: with M8 shipped, does a browser-created
session actually start with owner `"agent"` by default (which would be semantically backwards — a
human opened it, nothing agent-related is happening in it, yet it reports `owner:"agent"` in its
lease) unless `termserver`'s existing session-creation call site is updated to explicitly pass
`InitialOwner:"human"`? If so, this isn't just a "cleaner label" nice-to-have — say plainly whether
it's actually a **pre-existing latent bug in already-shipped M3 code** that M8 happens to be the
first to notice because M8 is the first caller that cares about the owner field's correctness, or
whether it's genuinely cosmetic (e.g. if nothing currently branches on a browser session's initial
owner value before the first real takeover/write). Check whether any shipped code (UI banner logic
in `termserver/assets/app.js`, or Go-side logic) reads a session's owner before any write happens
and would behave differently for `"agent"` vs `"human"` as the initial label.

### 3. `PID`/`ExitCode` on `SessionInfo` — this milestone or deferred?

Round 2 kept these deferred ("optional future field... not required for tool flow"). Confirm that
verdict holds now that the full error-mapping table (round 2, `terminal_write`/`terminal_kill`
cases) is written out: does *any* row in that table actually need `ExitCode` to produce a correct,
non-degraded `ToolEnvelope`, or can every case in the table be satisfied with the `nil`/omitted
value as already specified? If the table is genuinely fine without it, say so and close this as
"confirmed deferred, no change." If you find a row that's meaningfully worse without it (e.g. an
agent that can't tell a clean exit from a crash when deciding whether to retry a command), say so
and propose the minimal field addition.

## What I want from you

A definitive answer to each of the 3 — not a menu of options. If an answer requires a small change
to already-shipped code (like question 2 might), say exactly what changes and where, at the level
of detail round 2 used for `CreateWithOptions` (real file, real function, real signature). If you
determine one of these is actually a latent correctness issue rather than a style preference,
elevate it — say so explic3itly, the way round 1 elevated the bare-`Write()` issue from "a detail"
to "a fatal bug." End by confirming: with these 3 closed, is `~/exo/M8_INTEGRATION_DESIGN.md` now
fully ready for build prompts with zero remaining open questions, or is there a 4th round needed
and on exactly what?

Do not touch or modify any files in `~/exo`, `~/nucleo-base`, `~/pacta-harness`, or `~/forge` —
planning/critique only.

Write your full response to `~/exo/M8_design_round3_response.md`.