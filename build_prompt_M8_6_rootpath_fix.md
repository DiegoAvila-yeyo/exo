Small, targeted fix in `~/exo` (module `github.com/DiegoAvila-yeyo/exo`), package `agenthost`. Real
build task, run tests, `go test -race` clean.

## The bug (found during manual end-to-end verification)

`nucleo-base`'s filesystem tools do not anchor relative paths to any configured root — they
resolve purely against whatever the OS process's current working directory happens to be:

- `~/nucleo-base/layer2-runtime-rails/tool/writefile.go`: `WriteFileTool.Execute` calls
  `os.WriteFile(in.Path, ...)` directly with the model-provided path, no root anchoring.
- `~/nucleo-base/layer2-runtime-rails/tool/bash.go`: sets `cmd.Dir = in.Workdir`; when the model
  doesn't supply `Workdir`, Go's `exec.Cmd` defaults `Dir` to the calling process's cwd.

`agenthost.Host` is constructed with a `rootPath` (from `EXO_AGENT_ROOT_PATH` or the user's home
directory, see `~/exo/agenthost/host.go`'s `rootPathFromEnv`) and passes it to
`runtime.NewCoordinator(agent, rootPath)`, but nothing ever makes the **process's actual working
directory** match that `rootPath`. Verified live: with `EXO_AGENT_ROOT_PATH=/Users/x/some-project`
but the `exo serve` process itself launched from a different directory (e.g. `~/exo`), asking the
agent to create a file with a relative path resulted in the file being written inside `~/exo`
instead of the configured project root — confirmed by direct filesystem inspection after the
agent's chat turn reported success. This is a real correctness/scoping gap: the `rootPath`
configuration is currently decorative for any tool that relies on relative paths or an implicit
cwd, not an actual sandbox boundary.

This is not a `nucleo-base` bug to fix there (out of scope — the TUI likely has the same
characteristic and relies on being launched from the right directory by the user). For `exo`'s
merged binary specifically (`launchd`-activated, started from whatever directory `launchd` uses,
not user-chosen), this needs an `exo`-side fix.

## The fix

In `agenthost.New` (`~/exo/agenthost/host.go`), after resolving `rootPath` via
`rootPathFromEnv()` and before constructing the provider/registry/agent/coordinator, call
`os.Chdir(rootPath)` and return an error if it fails (fail-fast, matching the existing pattern for
provider config — an unusable root path should stop startup, not silently proceed with the wrong
cwd). Add a clear comment explaining why: `nucleo-base`'s `write_file`/`bash` tools resolve
relative paths against the process cwd, not against `coordinator.rootPath`, so this is the only way
to make `EXO_AGENT_ROOT_PATH` actually scope where the agent's file operations land.

Confirm this doesn't break anything else in `~/exo` that depends on the process's original working
directory (check `backend/backend.go`, `sessionstore`, `appconfig` — anything using relative paths
for its own config/lock/session-store files should already be using `appconfig.AppSupportDir()`
style absolute paths under `~/Library/Application Support`, not relative paths, but verify this
explicitly by reading those packages rather than assuming).

## Test to add

A test in `agenthost` that constructs a `Host` with `EXO_AGENT_ROOT_PATH` pointed at a temp
directory (`t.TempDir()`), then asserts `os.Getwd()` returns that temp directory after
construction. Use `t.Chdir` or manually restore the original cwd in `t.Cleanup` so this test
doesn't leak process-global state into other tests in the same package (tests in Go can run in the
same process, so a global `os.Chdir` is real cross-test state — be careful here, and if the
existing `agenthost` test file already constructs multiple `Host`s across different tests, make
sure this new test doesn't break their assumptions about cwd).

## What NOT to do

Do not touch `nucleo-base` at all — this fix lives entirely in `~/exo/agenthost`. Do not touch
`m8adapter`, `sessions`, `realpty`, `ptyactor`, or `termserver`.

## When done

Report the exact diff, why you're confident it doesn't break existing `agenthost`/`backend` tests
(cite what you checked), and full `go test -race -count=1 ./...` output from `~/exo`.
