Small, targeted fix in `~/exo` (module `github.com/DiegoAvila-yeyo/exo`), packages `launchagent`
and `cli`. Real build task, run tests, `go test -race` clean.

## The bug (found during manual persistence verification)

`launchd`-activated processes do **not** inherit the interactive shell's `PATH` — they get macOS's
minimal default. Verified live: after installing `exo` as a real `LaunchAgent` and restarting it
via `exo restart`, asking the agent to work in a real Next.js project (`~/Pacta-dashboard-AI`)
failed at the coordinator's preflight gate with `zsh:1: command not found: npm`, even though `npm`
works fine in an interactive shell (it's nvm-managed, at
`~/.nvm/versions/node/v22.23.1/bin/npm` on this machine, not on `launchd`'s default `PATH`). This
blocks the coordinator's `npm run build` preflight check (and any tool invocation that shells out
to `npm`/`node`/etc.) for any real Node project once `exo` runs as an installed service, not just
manually from a terminal.

## The fix

Capture `PATH` at `exo install` time (when the CLI is run interactively, so `os.Getenv("PATH")`
reflects the user's real shell setup — nvm, homebrew, rbenv, whatever they have) and bake it into
the generated `LaunchAgent` plist as an `EnvironmentVariables` entry, so the `launchd`-spawned
process gets the same `PATH` the user has interactively.

### 1. `launchagent.Config`/`RenderPlist`

In `~/exo/launchagent/plist.go`, add an `EnvironmentVariables` field to `Config`:
```go
type Config struct {
    Label       string
    ProgramPath string
    SocketName  string
    Port        int
    EnvironmentVariables map[string]string // optional; omitted from plist if empty
}
```
Extend `RenderPlist` to emit an `<key>EnvironmentVariables</key><dict>...</dict>` block (standard
launchd plist shape — one `<key>NAME</key><string>VALUE</string>` pair per entry) when the map is
non-empty, XML-escaped the same way existing string fields already are (reuse `xmlEscape`). Keep
existing behavior byte-for-byte identical when the map is empty/nil — don't add an empty
`<dict></dict>` block, omit the key entirely (confirm this against the existing
`plist_test.go` assertions, which check for exact expected keys — don't break those).

### 2. `cli.Install`

In `~/exo/cli/cli.go`'s `Install`, capture `os.Getenv("PATH")` and pass it through to
`launchagent.RenderPlist` as `EnvironmentVariables: map[string]string{"PATH": capturedPath}` —
only if it's non-empty (don't emit an empty PATH override if somehow unset, which would make
things worse, not better; fall back to not setting `EnvironmentVariables` at all in that edge
case). This is a small addition right where `RenderPlist` is already called in `Install` — read
the current call site first.

## Known limitation — document it, don't try to solve it

`PATH` is captured once, at install time. If the user later changes their Node version via `nvm`
(or otherwise changes their `PATH`), the installed service won't pick that up until `exo install`
(or a future `exo restart`-with-recapture, out of scope here) runs again. Add a one-line comment at
the capture site in `cli.go` noting this tradeoff explicitly — this is a deliberate, pragmatic
choice (matching how most `launchd`-wrapped dev tools solve this exact problem), not an oversight.

## Tests to write

1. `launchagent`: `TestRenderPlistIncludesEnvironmentVariablesWhenProvided` — assert the rendered
   XML contains the expected `<key>PATH</key><string>...</string>` pair (XML-escaped correctly if
   the test value contains characters that need escaping, e.g. a `&` in a path segment — use a
   deliberately tricky test value to prove escaping isn't broken, not just a plain path).
2. `launchagent`: `TestRenderPlistOmitsEnvironmentVariablesWhenEmpty` — confirm the existing
   no-env-vars case still renders identically to before this change (regression guard).
3. `cli`: extend whatever existing test covers `Install`'s plist generation to assert the captured
   `PATH` from `os.Getenv` ends up in the rendered plist passed to `launchagent.RenderPlist` — use
   the existing fake-runner/temp-`HOME` test pattern already in `cli/cli_test.go`, and use
   `t.Setenv("PATH", ...)` to control the captured value deterministically in the test rather than
   depending on whatever the real test-runner's `PATH` happens to be.

Run `go test -race -count=1 ./...` in `~/exo` and confirm nothing else regresses, especially the
existing `launchagent` plist tests.

## What NOT to do

- Do not try to dynamically resolve `nvm`'s "current" version symlink or otherwise get clever about
  Node version management — just capture and bake in `PATH` as-is at install time, per the
  documented limitation above.
- Do not touch anything else in `~/exo` or `nucleo-base` — this is scoped to `launchagent` +
  `cli.Install`'s plist-generation call site.

## When done

Report: exact diff, the exact rendered `EnvironmentVariables` XML shape, full
`go test -race -count=1` output, and confirm the existing (pre-this-change) plist tests still pass
unmodified in their expectations (or explain exactly what had to change and why, if anything).
