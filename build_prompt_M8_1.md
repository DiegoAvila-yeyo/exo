You are building the first piece of M8 for `exo` (Go module `github.com/DiegoAvila-yeyo/exo`).
This is a real build task, not planning — write actual Go code, run the tests you write, make sure
they pass with `go test -race`.

## Context

M8 connects `nucleo-base`'s AI agent to `exo`'s terminal infrastructure so a browser chat message
can drive a real, ownership-aware PTY session. The full design went through 3 rounds of
Claude↔Codex adversarial critique (closed, not open for re-litigation) — read
`~/exo/M8_INTEGRATION_DESIGN.md` in full before writing any code, it is the canonical source of
truth for every decision below. This build prompt covers only the first of three pieces: the
foundational session-creation API changes in `exo` that everything else in M8 depends on. The
other two pieces (a new terminal adapter, and new SSE chat/approval routes in `termserver`) are
separate build prompts that come after this one is done and reviewed.

Also read, for context on the existing shipped code you're extending:
- `~/exo/sessions/manager.go` (the `Manager` you're adding to)
- `~/exo/realpty/realpty.go` (the PTY constructor you're extending)
- `~/exo/ptyactor/session.go` (already has `WithInitialOwner` — you are not modifying this file,
  just wiring into it correctly)
- `~/exo/termserver/server.go` (the consumer whose existing behavior must not change)

## What to build

### 1. `sessions.CreateOptions` and `sessions.Manager.CreateWithOptions`

Add to `~/exo/sessions/manager.go` (or a new file in that package if that's cleaner):

```go
type CreateOptions struct {
    Workdir      string
    Name         string
    Command      string // empty = today's interactive-shell path, unchanged behavior
    InitialOwner string // empty = defer to whatever ptyactor.NewSession does by default
}

func (m *Manager) CreateWithOptions(opts CreateOptions) (SessionInfo, error)
```

`CreateWithOptions` does everything `Create` currently does (workdir validation, id allocation,
session-cap check, `sessionstore` persistence, wrapping in `ptyactor.NewSession`), plus:
- if `opts.Command` is non-empty, pass it through to `realpty.New` via the new `WithCommand`
  option below, and store the **logical command string** (`opts.Command`, not a shell path) in the
  resulting `SessionInfo.Command`.
- if `opts.InitialOwner` is non-empty, pass `ptyactor.WithInitialOwner(opts.InitialOwner)` to
  `ptyactor.NewSession`.

### 2. `Create` becomes a thin wrapper — and fixes a real, already-shipped bug

```go
func (m *Manager) Create(workdir, name string) (SessionInfo, error) {
    return m.CreateWithOptions(CreateOptions{
        Workdir:      workdir,
        Name:         name,
        InitialOwner: "human",
    })
}
```

**Why `InitialOwner: "human"` matters — this is not cosmetic.** Today `Create` calls
`ptyactor.NewSession(pty)` with no owner override, so it silently inherits `ptyactor`'s
`defaultOwner = "agent"`. `termserver` sends that owner in the initial `ready` WebSocket message,
and the frontend (`termserver/assets/app.js`) uses it *before any write or takeover happens* to
decide whether to show the "Agent has control" lock banner, gate write access, and show the "Take
control" button. Net effect in the current shipped code: **a human opening a brand-new browser
session immediately sees "Agent has control" / read-only input, with no agent involved at all.**
Fixing `Create` to explicitly pass `"human"` fixes this. Confirm this fix doesn't change any
existing test's expectations in a way that reveals a test was relying on the buggy behavior — if
you find one, that test was asserting the bug, fix the test's expectation, don't revert the fix.

### 3. `realpty.WithCommand` option

Add to `~/exo/realpty/realpty.go`:

```go
func WithCommand(command string) Option
```

Behavior:
- Not passed, or passed with `""`: unchanged — today's interactive-shell resolution and `shell -i`
  startup path, exactly as it works today.
- Passed with a non-empty string: instead of starting the resolved shell interactively, launch the
  command via `/bin/sh -lc <command>` in the PTY. Store the **logical requested command string**
  (the value passed to `WithCommand`, not `/bin/sh`) as `Terminal.command` — this is what
  `SessionInfo.Command` will read from. This matches how `nucleo-base`'s existing
  `terminal.Manager.Open` already launches commands (`~/nucleo-base/layer2-runtime-rails/
  terminal/manager.go`, read it for the exact pattern it uses — you're matching an existing
  convention, not inventing one).

Do not change any existing exported behavior of `realpty.New` or its current options — this must
be purely additive.

### 4. `sessions.Manager`'s existing `sessionStore` consumer in `termserver` must not need changes

`termserver/server.go` depends on `sessions.Manager` through a narrow interface requiring only
`Create(workdir, name)` (unchanged signature), `Get`, `List`, `Close`, `Touch`. Confirm after your
changes that `termserver` compiles and its existing tests pass with zero changes to `termserver`
itself — if something doesn't compile, you've made a breaking change where an additive one was
required; fix your approach, don't touch `termserver`.

## Tests to write

1. `TestCreateWithOptionsStartsCommandBackedSession` (`sessions` package) — `CreateWithOptions`
   with a non-empty `Command` results in a `SessionInfo.Command` equal to the logical command
   string (not a shell path), and the underlying PTY actually ran that command (assert via
   whatever test seam/fake you introduce for `realpty` — don't require a real shell/PTY in this
   test if you can avoid it, following the existing test style in `sessions/manager_test.go`).
2. `TestCreateWrapperStillCreatesInteractiveShellWithHumanOwner` — `Create(workdir, name)` still
   produces an interactive shell session (unchanged from today), AND its resulting session's
   `ptyactor.Session.Lease().Owner` is `"human"` (this is the regression test for the bug fix —
   write it so it would have failed against the old `Create` implementation).
3. `TestCreateWithOptionsPlumbsInitialOwner` — `CreateWithOptions` with `InitialOwner: "agent"`
   results in `Lease().Owner == "agent"`; with `InitialOwner: ""`, falls back to whatever
   `ptyactor.NewSession`'s own default is (don't hardcode an assumption about what that default is
   in this test — read it from `ptyactor` and assert against that).
4. `realpty` package: a test for `WithCommand` — with the option unset, behavior is byte-for-byte
   what existing tests already assert; with it set, the resulting `Terminal.Command()` (or
   equivalent accessor) reflects the logical command string, not the shell path used to launch it.
5. Run the full existing `sessions`, `realpty`, `ptyactor`, and `termserver` test suites
   (`go test -race ./...` from `~/exo`) and confirm nothing regresses. If `termserver`'s existing
   tests assumed the old buggy owner behavior for `Create`, update those specific assertions (not
   the routes/logic) to expect `"human"` — call out explicitly in your report which test(s), if
   any, you had to update this way.

## What NOT to do

- Do not touch `~/nucleo-base` at all in this build — that's a separate, later piece of M8.
- Do not build the terminal adapter or any `termserver` chat/approval routes — separate build
  prompts.
- Do not change `ptyactor/session.go` — `WithInitialOwner` already exists and does what's needed;
  you're only wiring `sessions.Manager` to use it.
- Do not remove or rename the existing `Create(workdir, name)` signature — it must keep working
  for `termserver` unchanged.

## When done

Report back: the exact diff/files touched, full `go test -race ./...` output from `~/exo`, and
explicitly confirm (a) `Create()` now yields `Lease().Owner == "human"` and (b) any pre-existing
test you had to adjust because it was asserting the old buggy behavior.
