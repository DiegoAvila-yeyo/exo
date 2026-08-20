Rebuild the Canvas work lost in the git reset — but two things have to happen first, before any
file gets written. Both come from things found while investigating the reset, not from the original
plan, so they won't be in `~/.claude/plans/reflective-floating-book.md`.

## 1. Confirm `EXO_AGENT_ROOT_PATH` before doing anything else

`agenthost/host.go`'s `rootPathFromEnv()` falls back to `$HOME` when this env var is unset — verified
in the code, and verified unset in this environment right now (`env | grep EXO` returns nothing
relevant). That means any agent tool call (`write_file`, `bash`) you make while unset is scoped to
the entire home directory, not this project. Before rebuilding: confirm what this session's
`EXO_AGENT_ROOT_PATH` actually is right now, and if it's unset or not pointed at this project
directory, say so explicitly and stop — don't proceed with file writes until it's set correctly. This
isn't optional caution — a separate concurrent session already flagged a background task that grew
file-writing scope unexpectedly, and root-scoped-to-`$HOME` is exactly the condition that made that
possible.

## 2. Commit after each task, not once at the end

The reflog shows an unexplained `git reset --hard` + clean pattern that has happened **four times**
in this repo's history, not once — it's recurring, cause still unknown. Rebuilding all of tasks #1–7
and holding everything uncommitted until the end repeats the exact setup that already caused one full
loss. Instead: commit at the end of each individual task (#1, #2, #3...), with a message identifying
which task it is. If a reset happens again mid-rebuild, at most one task's work is at risk, not the
whole thing — and the commit history itself becomes evidence for whoever eventually finds what's
triggering the resets.

## 3. Then rebuild

Once 1 and 2 are confirmed: proceed with "Redo the Canvas work" as you proposed — tasks #1–5 from
scratch (you have the full plan and know what you built), continuing through #6–7. Before starting,
it's worth a quick look at the dangling stash (`de1503fdc15cb3a446a2fa7b68b58456c490c6ac`, partial
task #1–3 work, not yet gc'd) — `git show de1503fdc...` — only to see if anything there is faster to
recover than to redo; don't block on it if it's not obviously useful, the plan is the source of truth
either way.

Report back once 1 and 2 are actually confirmed done — don't fold that confirmation silently into
"starting the rebuild now."
