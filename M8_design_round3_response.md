# M8 Round 3: closing the last 3 optional questions

I re-read `~/exo/M8_INTEGRATION_DESIGN.md` first, then checked the live `exo` / `nucleo-base`
code paths the three questions depend on.

## Executive verdict

Two of the three are now easy to close definitively.

One of them is **not** just a refinement after checking the shipped code:

- **browser-created sessions starting with owner `"agent"` is a latent correctness bug in shipped M3/M6 behavior, not just a cosmetic label issue**

With that fixed in the design, I think M8 is fully ready for build prompts with **zero** remaining
open questions. No round 4 is needed.

## 1. Structured `session_id` on approval SSE events

### Definitive v1 answer

**Widen the approval callback now. Do not keep prompt-parsing as the v1 answer.**

### Why prompt-parsing is not acceptable after checking the actual failure mode

Today the approval path is string-only:

- `TerminalWriteTool` builds
  `prompt := "approve write to interactive terminal <sessionID>?"`
  and `detail := "Session command: ..."` (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminal_write.go:61-70`)
- the global callback is only
  `func(prompt, detail string) bool`
  (`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminalapproval.go:18-47`)

Round 2's prompt-parsing fallback works only as long as that exact prompt string never changes and
only as long as every approval that matters comes from that one tool path.

That is too fragile for the actual ambiguity here:

- round 1 explicitly kept **multi-session-per-turn** allowed
- when parsing fails, the browser gets an approval banner with no `session_id`
- in a multi-session turn, that is not just a degraded label, it is an **ambiguous human decision**
  with no reliable way to know which terminal the approval is about

That is the wrong place to economize on contract shape. The cost of widening the callback now is
small and localized; the cost of discovering later that the web approval UI cannot reliably
correlate approvals to sessions is higher because it hits the host wiring after tools already
depend on the old shape.

### Exact v1 change

Keep the six-method terminal backend decision untouched. The only `nucleo-base` change beyond that
should be the approval callback signature:

```go
var globalApprove func(prompt, detail string, meta map[string]string) bool

func SetGlobalApproveFunc(fn func(prompt, detail string, meta map[string]string) bool)

func RequestToolApproval(prompt, detail string, meta map[string]string) bool
```

Then `TerminalWriteTool` should call:

```go
RequestToolApproval(prompt, detail, map[string]string{
    "tool":       "terminal_write",
    "session_id": in.SessionID,
    "command":    meta.Command,
})
```

Files affected conceptually:

- `/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminalapproval.go`
- `/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/tool/terminal_write.go`
- the merged-host wiring in `exo` that installs the approval callback

### SSE event answer

With that change, `approval` events in `/api/chat/stream` should carry a **guaranteed**
`session_id` for terminal-write approvals, not a best-effort parsed one.

Verdict:

- prompt-parsing is **rejected**
- widening the callback is the v1 answer

## 2. Should browser-created sessions get `InitialOwner:"human"`?

### Definitive v1 answer

**Yes. Browser-created sessions must explicitly start with `InitialOwner:"human"`.**

### This is a latent bug, not a cosmetic preference

The current shipped code path is:

- `sessions.Manager.Create(workdir, name)` calls `ptyactor.NewSession(pty)` with no owner override
  (`/Users/eltitoyeyo/exo/sessions/manager.go:80-115`)
- `ptyactor`'s default owner is literally `defaultOwner = "agent"`
  (`/Users/eltitoyeyo/exo/ptyactor/session.go:18-23`)
- `termserver` immediately reads that lease and sends it in the initial `ready` message
  (`/Users/eltitoyeyo/exo/termserver/server.go:294-306`)

The frontend consumes that owner **before any write or takeover happens**:

- on `ready`/`lease`, it sets `state.owner = message.owner`
  (`/Users/eltitoyeyo/exo/termserver/assets/app.js:298-305`)
- write permission is gated by `state.owner === "human"`
  (`/Users/eltitoyeyo/exo/termserver/assets/app.js:338-340`)
- the owner pill shows `"You have control"` vs `"Agent has control"`
  (`/Users/eltitoyeyo/exo/termserver/assets/app.js:421-425`)
- the takeover button is shown when `state.owner === "agent"`
  (`/Users/eltitoyeyo/exo/termserver/assets/app.js:431-431`)
- the overlay explicitly locks the terminal when the ready owner is `"agent"`
  (`/Users/eltitoyeyo/exo/termserver/assets/app.js:459-462`)

So in the shipped browser path, a human can create a brand-new session and immediately receive a
UI that says:

- "Agent has control"
- input is read-only
- "Take control" is shown

even though no agent exists in that session and no takeover occurred.

That is not just a cleaner label issue. It is a **visible semantic inversion in current behavior**.
M8 is the first milestone to notice it because M8 is the first design pass that treats owner
semantics as part of the contract, but the bug is already latent in the shipped M3/M6 stack.

### Exact change

Once `CreateWithOptions` exists, `Create` should become a wrapper that explicitly passes
`InitialOwner:"human"`:

```go
func (m *Manager) Create(workdir, name string) (SessionInfo, error) {
    return m.CreateWithOptions(CreateOptions{
        Workdir:      workdir,
        Name:         name,
        InitialOwner: "human",
    })
}
```

And the M8 adapter should explicitly pass:

```go
CreateWithOptions(... InitialOwner: "agent")
```

Where:

- browser / existing `termserver` session creation path => human-owned from the start
- M8 agent-opened sessions => agent-owned from the start

That is the correct semantic split.

### Severity

Elevating this explicitly:

- this is a **latent correctness bug in already-shipped code**
- it should be fixed as part of the same `CreateWithOptions` work, not deferred

## 3. `PID` / `ExitCode` on `SessionInfo`: now or deferred?

### Definitive v1 answer

**Confirmed deferred. No change needed in this milestone.**

### Why the round 2 verdict still holds

The tool-side contract that matters is `terminal.ToolEnvelope`, not `sessions.SessionInfo`.

`ToolEnvelope` already has optional `PID` and `ExitCode` fields
(`/Users/eltitoyeyo/nucleo-base/layer2-runtime-rails/terminal/types.go:60-75`), but none of the
rows in the round 2 error table require `ExitCode` to remain correct:

- `terminal_read` timeout/no-output: does not need it
- ownership-lost: does not need it
- write to exited session: can still correctly return
  `already_exited:true`, `status`, and `"session is not running"` without it
- kill on already-exited session: remains correct without it
- kill on live session: `status:"killed"` is sufficient

The only thing `ExitCode` would improve is **diagnostic richness**, not correctness of the v1
agent/tool flow.

Concretely:

- an exited session without `ExitCode` is still distinguishable from ownership-lost because the
  status/error pair is different
- a killed session is still distinguishable from a running session because `already_exited` and
  `status` carry that meaning already
- the agent can already decide "do not retry a write to this dead session" without `ExitCode`

Would `ExitCode` help an agent decide whether a command crashed vs exited cleanly? Yes. But that is
an optimization, not a blocker for any row in the current table.

### Minimal conclusion

Keep:

- `PID` omitted in v1
- `ExitCode` omitted / nil in v1

No field addition is required to make the M8 `ToolEnvelope` table correct and non-degraded in the
sense that matters for build prompts.

## Final confirmation

With these three closed, `~/exo/M8_INTEGRATION_DESIGN.md` is now fully ready for build prompts with
**zero remaining open questions**.

No round 4 is needed.

If you update the canonical doc before writing the build prompts, the three concrete edits to carry
forward are:

1. Approval callback becomes structured; approval SSE `session_id` is guaranteed, not prompt-parsed.
2. Browser-created sessions explicitly start with `InitialOwner:"human"`; agent-created sessions
   explicitly start with `InitialOwner:"agent"`.
3. `PID` / `ExitCode` remain explicitly deferred with no change to the milestone scope.
