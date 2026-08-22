This is a handoff into a new planning conversation — same three-party pattern already used
throughout this project (a Claude session talks to the user, drafts a design, Codex critiques it
hard, repeat until sound — see `M8_handoff_prompt.md`, `planning_design_canvas_*.md`,
`build_prompt_PLANNING_ROUND*.md` for closed examples of exactly this). Read this whole brief
before saying anything back. Confirm you've understood `exo` and the Canvas feature in your own
words to the user first — this is a big amount of already-settled context and a garbled restart
would waste it — **then open a real conversation with the user about what they actually want next.
Do not invent the new design proposal yourself.** The user has something in mind; your job is to
draw it out, sharpen it with them, and only once it's genuinely ready, write the Codex critique
prompt that continues the three-way conversation (you, the user, Codex) the same way every prior
round in this project has worked.

## What `exo` is

Read `IMPLEMENTATION_PLAN.md` for the full milestone history. Short version: a Mac-only, `launchd`
socket-activated backend that gives the user a real, secure, ownership-aware (agent-vs-human,
epoch-based, race-free) terminal controllable from a browser, with a full AI agent (vendored from
`~/nucleo-base`, a sibling repo via a local `go.mod` `replace`) driving it end to end — M0 through
M8 are all closed and built, M8 being "connect the agent to the terminal" (the original product
goal: "program from a web dashboard as if it were Claude CLI/Desktop"). `exo`'s own module is
`github.com/DiegoAvila-yeyo/exo`; it also depends on `~/yeyo` (an atom/behavioral-guidance catalog)
the same way. All three repos are private on GitHub (`DiegoAvila-yeyo/exo`, `yeyoos/nucleo-base`,
`yeyoos/yeyo`) and, as of this handoff, fully pushed and in sync with `origin/main` — no
uncommitted or unpushed work anywhere.

## What Canvas is — the feature this handoff is really about

Read `CANVAS_STATUS.md` first — it's the living index of everything about this feature: which
design-round documents exist and in what order to read them, what's built, what's verified live vs.
only reported, what bugs are open, and what's explicitly deferred. Treat it as more current than
anything summarized below; this section is a snapshot, that file is the source of truth.

**The original idea**, in the user's own words from the very first message of the planning
conversation that started all of this: *"lo que quiero con exo es que la ia tenga el control de
todo desde el chat"* — the chat becomes the control surface for the whole app, not just for
planning. Concretely: a 3-column layout (existing left sidebar / a 2D canvas in the center / chat on
the right) where discussing something with the AI can materialize a real object onto the canvas —
first a diagram, eventually other types — which then stays anchored as persistent context for the
rest of that work, "sirve como ancla."

**What actually exists today** (all closed, built, and — per `CANVAS_STATUS.md`'s per-item
breakdown — mostly verified live against a real running `exo serve` instance with a real LLM, not
just unit-tested): `canvasstore` (a new, project-scoped store, deliberately separate from
`planningstore` — composition, not migration), a draft → materialize two-phase object lifecycle,
hybrid intent detection (NL primary, `/materialize` slash-command fallback, contextual button),
diagrams rendered as a structured shape-graph with client-side auto-layout, dual editing (a manual
JSON module and an AI-driven mini-chat, both going through the same CAS-protected write path),
content versioning via `supersedes` chains (never mutate, always append), and a real
activate/deactivate anchoring mechanism (`dynamicCentro`) that injects an active object's current
content into the system prompt every turn — with a same-day fallback fix so an object that's never
been edited still anchors from its original materialization payload, not silently skipped.

**What's still open, on purpose or not**: two systemic bugs (raw tool-call JSON and an internal
`=== FINAL ===` marker leaking into ordinary chat text — traced to `agenthost/stdout.go`, affects
every tool call in the whole app, not just Canvas, still needs a decision on filtering approach
before it's fixable) and two fixes with strong automated-test coverage but no live browser
click-through yet. And, deferred **on purpose, not forgotten** — this is likely relevant to whatever
the user's next proposal is: **Planning was always supposed to become one Canvas object type**
(`type: "planning"`, a thin wrapper around an existing `planning_id` — composition, per
`build_prompt_CANVAS_HOME_V1.md`) and this was never built, only designed. Same for every other
object type beyond diagram (image, text, music, "aprendizajes" were all named as eventual types),
and for sub-session/context-budget management once a project accumulates a lot of anchored objects
over time.

## The established workflow — follow it, don't reinvent it

Every substantial design decision in this project so far went through the same shape: a Claude
session (you) drafts something concrete, not vague — a real proposal with tradeoffs already
considered, not a menu of options — writes it into a `planning_design_*_prompt.md`-style document
addressed to Codex, explicitly marking what's already closed and must not be re-litigated. Codex
attacks it hard and writes back a real recommendation, not just more questions. That gets folded
back in, sometimes for another round, until a `build_prompt_*.md` can be written with zero open
questions left for an actual build session. Look at `planning_design_canvas_home_prompt.md` through
`_round4_prompt.md` plus `planning_design_atoms_canvas_anchor_prompt.md` for the fullest worked
example of this happening four-plus rounds deep on one feature, and `M8_design_round1-3` for another
closed example.

## What to actually do

1. Confirm you've understood `exo` and Canvas back to the user, briefly, in your own words.
2. Ask the user what they have in mind for the next round — don't guess, and don't default to
   "obviously it's Planning-as-object" just because that's the most visible gap; let them actually
   say it. If they don't have anything to add beyond what's in `CANVAS_STATUS.md`'s deferred list,
   that's a fine starting point too, but confirm it's genuinely their call, not your assumption.
3. Talk it through with them like any real design conversation — ask what problem it solves, what
   it should and shouldn't do, where it sits relative to what's already built, the same way earlier
   rounds started (see `planning_design_canvas_home_prompt.md`'s opening exchange for tone/depth).
4. Once the proposal is concrete enough to survive contact with a hard critique — not before —
   write the Codex prompt, same format as the existing `planning_design_*_prompt.md` files (what's
   closed and must not be re-litigated, what the user is asking for now, your own draft with real
   tradeoffs already considered, open questions for Codex to actually take a position on, what you
   want back). That's the artifact that continues this as a real three-way conversation between the
   user, you, and Codex — not something you hand off and disappear from.

## Tone and constraints to carry forward

- User's working language is Spanish for conversation; code, comments, and anything addressed to
  Codex stay in English — same convention used throughout this whole project.
- Don't build anything, don't touch `exo`/`nucleo-base`/`yeyo` source, during this planning
  conversation — this round is design only, same as every `planning_design_*` round before it.
- The user does not want rubber-stamped agreement — every prior round that mattered involved real
  pushback, either from Codex or from the user catching something in your own reasoning. Keep that
  standard.
