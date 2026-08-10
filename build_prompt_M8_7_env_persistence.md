You are building a new piece of `exo` (module `github.com/DiegoAvila-yeyo/exo`) — persistent
configuration for the merged agent binary. Real build task, write actual Go code, run tests,
`go test -race` clean.

## Context and why this is needed

M8 (already built and manually verified — see `~/exo/M8_INTEGRATION_DESIGN.md`) added `agenthost`,
which reads provider credentials and agent config purely from process environment variables
(`ANTHROPIC_API_KEY`/`LITELLM_API_KEY`/`OPENAI_API_KEY`, `EXO_AGENT_ROOT_PATH`, etc. — read
`~/exo/agenthost/provider.go` and `~/exo/agenthost/host.go` for the full list of `os.Getenv` calls
this needs to keep working). During manual testing this was fine because the env vars were passed
by hand on the shell command line. But `exo`'s real deployment model is a `launchd`-activated
`LaunchAgent` (see `~/exo/launchagent/plist.go`, `~/exo/cli/cli.go`'s `Install`) — and the generated
plist today has **no `EnvironmentVariables` support at all**, so on a real reboot/reinstall,
`launchd` starts the backend with none of this configured, and `agenthost.New`/`ValidateEnv`
correctly fails fast with "no provider configured" — but there's currently no way to fix that
without going back to manually exporting env vars in a shell and launching the binary directly,
defeating the whole point of `launchd` activation.

## What to build

### 1. A persistent config file, not baked into the plist

Do **not** put secrets into the `LaunchAgent` plist itself (`~/Library/LaunchAgents/*.plist` is
more discoverable/less appropriately-permissioned than an app-support file, and baking it into
`RenderPlist` would require re-running `exo install` every time a key changes). Instead:

Add `EnvFilePath()` to `~/exo/appconfig/config.go`, following the exact existing pattern of
`LockPath()`/`SessionStoreDir()`:
```go
func EnvFilePath() (string, error) {
    dir, err := AppSupportDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(dir, "agent.env"), nil
}
```

### 2. A simple env-file loader

New file, e.g. `~/exo/appconfig/envfile.go` (or a new small package if that reads cleaner — your
call, state which). Format: plain `KEY=VALUE` lines, `#`-prefixed lines and blank lines ignored,
no quoting/escaping needed (keep it simple — this isn't a general-purpose dotenv parser, just
enough for API keys and a few config strings). Function:
```go
func LoadEnvFile(path string) error
```
Behavior:
- If the file doesn't exist, return `nil` (not an error) — a completely fresh install with no env
  file yet should fall through to whatever's already in the process environment (manual testing
  convenience) and let the existing fail-fast in `buildProviderFromEnv` catch a truly missing
  config.
- For each `KEY=VALUE` line, call `os.Setenv(key, value)` **only if `os.Getenv(key)` is currently
  empty** — ambient process environment (e.g. someone testing with exported shell vars) must take
  precedence over the persisted file, not be silently overridden by it.
- Malformed lines (no `=`) should be skipped, not fatal — don't let one bad line break startup.

### 3. Wire it into startup, before anything reads env vars

In `~/exo/backend/backend.go`'s `Run`, call the env-file loader (via `appconfig.EnvFilePath()` +
`appconfig.LoadEnvFile(path)`) as the very first thing, before `singleton.Acquire` or any other
setup — it must run before `agenthost.New`/`agenthost.ValidateEnv` are invoked anywhere in the
call chain (check where those get called from — likely not directly in `backend.Run` today per
piece 4, confirm the actual call site and make sure the env file loads before it). If the file
exists but is unreadable (permissions error, not "doesn't exist"), fail fast with a clear error —
same fail-fast principle already used for missing provider config.

### 4. Bootstrap a template on `exo install`

In `~/exo/cli/cli.go`'s `Install` (read the existing function first, follow its exact style): after
resolving the env file path, if it does **not** already exist, write a template file with `0600`
permissions (not `0644` — this can hold API keys) containing commented-out examples for every env
var `agenthost` reads, e.g.:
```
# exo agent configuration — uncomment and fill in what you need.
# At least one provider key is required.
# ANTHROPIC_API_KEY=
# ANTHROPIC_MODEL=
# LITELLM_API_KEY=
# LITELLM_BASE_URL=
# LITELLM_MODEL=
# OPENAI_API_KEY=
# OPENAI_MODEL=
# EXO_AGENT_ROOT_PATH=
# EXO_AGENT_MAX_TOKENS=
# EXO_AGENT_SYSTEM_PROMPT=
```
If the file already exists (e.g. a reinstall), leave it untouched — never overwrite user-edited
config. Print (to stdout, matching `Install`'s existing user-facing output style — check what it
already prints, if anything) the resolved path so the user knows where to edit it.

## Tests to write

1. `appconfig`: `TestLoadEnvFileSetsUnsetVars` — write a temp file with a couple `KEY=VALUE` lines,
   call `LoadEnvFile`, assert `os.Getenv` reflects them (use `t.Setenv` idiom / cleanup properly so
   this doesn't leak into other tests in the package).
2. `appconfig`: `TestLoadEnvFileDoesNotOverrideAmbientEnv` — pre-set an env var via `t.Setenv`,
   have the file contain a different value for the same key, assert the ambient value wins.
3. `appconfig`: `TestLoadEnvFileMissingFileIsNotAnError` — nonexistent path, assert `nil` error.
4. `appconfig`: `TestLoadEnvFileSkipsMalformedLines` — a file with one good line and one line with
   no `=`, assert the good line still loads and no error is returned.
5. `cli`: a test that `Install` creates the template env file with `0600` permissions when absent,
   and does **not** touch it when it already exists with different content (assert the existing
   content survives byte-for-byte) — follow the existing test patterns in `cli/cli_test.go` for how
   `Install` is already tested (likely with a fake command runner / temp `HOME`).

Run `go test -race -count=1 ./...` in `~/exo` and confirm nothing else regresses.

## What NOT to do

- Do not put any secrets or env values into the `LaunchAgent` plist / `launchagent.RenderPlist`.
- Do not touch `nucleo-base`, `m8adapter`, `sessions`, `realpty`, `ptyactor`, `termserver`,
  `agenthost`'s actual env-reading logic (`provider.go`, `rootPathFromEnv`, etc.) — those keep
  reading via `os.Getenv` exactly as today; this piece only makes sure the right values are *in*
  the environment before they're read, via `os.Setenv` from the loaded file.
- Do not overwrite an existing env file on `exo install`.

## When done

Report: exact files touched, the exact call site in `backend.Run` where the env file now loads and
why you picked that point, full `go test -race -count=1` output, and confirm the template file's
permissions are `0600` not `0644`.
