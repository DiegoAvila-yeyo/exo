You are wiring persistent agent memory into `exo`'s merged agent binary (`~/exo`, module
`github.com/DiegoAvila-yeyo/exo`). Real build task, run tests, `go test -race` clean.

## Context

`nucleo-base` (module `github.com/yeyoos/nucleo-base`) already has a working Layer 4 memory
system that the `runtime.Coordinator` is **already internally wired to use** — but only if someone
sets it up. Read these files first, don't re-derive the facts below:
- `~/nucleo-base/layer2-runtime-rails/runtime/coordinator.go` — `prepareOrientation` (around line
  281) calls `svc := tool.LocalMemoryService; if svc == nil { return ..., nil }` — i.e. the
  Coordinator already calls into memory on every turn, it just silently no-ops today because
  nothing ever sets `tool.LocalMemoryService`.
- `~/nucleo-base/layer2-runtime-rails/tool/memoryservice_binding.go` — the package-level var and
  setter: `var LocalMemoryService *memoryservice.Service` and
  `func SetLocalMemoryService(svc *memoryservice.Service)`. This is a global, set once — not
  per-request, not passed through any constructor you're calling directly.
- `~/nucleo-base/layer4-knowledge-memory/memoryservice/service.go` — `func New(store
  localstore.Store) *Service`, and `(s *Service) Enabled() bool { return s != nil && s.store !=
  nil }` — **nil-safe by design**: if you never call `SetLocalMemoryService`, or pass a `nil`
  service, everything continues to work exactly as it does today with memory silently disabled.
  This is not a feature you're building from scratch, it's a feature that already exists and is
  already safe to leave off — you're just turning it on.
- `~/nucleo-base/layer4-knowledge-memory/localstore/sqlite.go` — `func OpenSQLite(path string)
  (*SQLiteStore, error)` — self-contained: creates the parent directory and the schema
  (`CREATE TABLE IF NOT EXISTS`) itself, no separate migration step needed. `path == ":memory:"`
  is supported for tests (in-memory, no file). `(*SQLiteStore) Close() error` exists.
  `*SQLiteStore` implements `localstore.Store` (confirm this compiles against
  `memoryservice.New`'s parameter type — if there's a mismatch, that's a real finding to report,
  not something to work around).

Also read `~/exo/agenthost/host.go` (current `New`, `Host` struct, `Close()` from piece 9's MCP
wiring — you're extending the same file/pattern) and `~/exo/appconfig/config.go` (existing
`EnvFilePath`/`MCPConfigPath` pattern you're following).

## What to build

### 1. `appconfig.MemoryDBPath()`

Same pattern as `EnvFilePath()`/`MCPConfigPath()`:
```go
func MemoryDBPath() (string, error) {
    dir, err := AppSupportDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(dir, "memory.db"), nil
}
```

### 2. Open the store and wire it in `agenthost.New`

In `~/exo/agenthost/host.go`, resolve `appconfig.MemoryDBPath()` and call
`localstore.OpenSQLite(path)`. **This must be best-effort, not fail-fast** — unlike piece 7's
provider config (which is required for the agent to function at all) and piece 9's malformed-JSON
case (a real config error), a memory DB open failure (disk full, permissions, corruption) means
"memory doesn't work this session," not "the agent can't work at all." On failure: log a clear
warning via `log.Printf` (stderr, same reasoning as MCP's progress logging — don't pollute the
per-turn captured stdout) and continue with memory disabled (pass `nil` to
`tool.SetLocalMemoryService`, or simply don't call it — check which is cleaner given `Enabled()`'s
nil-safety). On success, construct `memoryservice.New(store)` and call
`tool.SetLocalMemoryService(svc)` once, before any turn can run (same ordering requirement piece 3
established for the approval callback — must be installed before the HTTP listener accepts
traffic, so do this during `agenthost.New`, not lazily).

Store the opened `*localstore.SQLiteStore` on `Host` (new field) so it can be closed on shutdown.

### 3. Close the store in `Host.Close()`

Extend the existing `Host.Close()` (added in piece 9 for MCP clients) to also close the memory
store if one was successfully opened, following the same "attempt all closes, don't stop at the
first error" pattern already used there for MCP clients.

### 4. No env var toggle needed for v1

Keep this simple: memory is always attempted, at a fixed path, best-effort. Don't add an
`EXO_AGENT_MEMORY_ENABLED`-style env var or any other configuration surface — if the DB can't
open, it's silently (well, logged-but-not-fatal) disabled, matching the "smallest correct thing"
scope discipline the rest of M8 has followed.

## Tests to write

1. `appconfig`: `TestMemoryDBPathUnderAppSupportDir` — matches the existing style of
   `EnvFilePath`/`MCPConfigPath` tests.
2. `agenthost`: `TestNewEnablesMemoryWhenStoreOpensSuccessfully` — construct a `Host` with an
   isolated app-support dir (same `t.Setenv("HOME", t.TempDir())` pattern piece 9's tests already
   use), assert `tool.LocalMemoryService` is non-nil and `.Enabled()` returns `true` after `New`
   returns (you'll need to import `nucleo-base`'s `tool` package in the test to check the global —
   be careful this test doesn't leak global state into other tests in the same `agenthost` package;
   reset `tool.LocalMemoryService` to its prior value in `t.Cleanup`, following whatever isolation
   pattern the existing tests in this file already use for other globals, if any).
3. `agenthost`: `TestNewContinuesWithMemoryDisabledWhenStoreOpenFails` — force an open failure
   (e.g. point `MemoryDBPath` at a path where the parent directory can't be created — a file
   where a directory is expected, or a permissions-denied path) and assert `New` **still succeeds**
   (does not return an error) — this is the core behavioral contract of "best-effort, not
   fail-fast." You may need a test seam (a package-level var like `openMemoryStore =
   localstore.OpenSQLite`, mirroring the existing `newAgentHost`-style seams already used elsewhere
   in this codebase for testability) if forcing a real filesystem failure deterministically is
   awkward — check how other `agenthost`/`backend` tests already fake out failure paths and match
   that convention rather than inventing a new one.
4. `agenthost`: extend `Host.Close()`'s existing test coverage (or add a new test) to confirm it
   also closes the memory store without error when one was opened, and remains a safe no-op when
   one wasn't.

Run `go test -race -count=1 ./...` in `~/exo` and confirm nothing else regresses. Pay attention to
test isolation — this is the first piece that touches a real *global* mutable variable
(`tool.LocalMemoryService`) shared across the whole `nucleo-base` `tool` package, so get the
reset/cleanup right or you'll get flaky cross-test pollution within `agenthost`'s test suite.

## What NOT to do

- Do not touch `nucleo-base` — `memoryservice`/`localstore`/the `tool.LocalMemoryService` binding
  are already complete and correct, this is pure wiring in `exo`.
- Do not make memory-store-open failures fatal to `agenthost.New` — this is the one piece so far
  in M8 where best-effort degradation is the *correct* design, not a shortcut; don't apply piece
  7/9's fail-fast precedent here, they don't transfer (re-read why above if unsure).
- Do not add a memory-configuration UI, env var toggle, or admin endpoint — fixed path, always
  attempted, that's the full v1 scope.

## When done

Report: exact files touched, confirm `*localstore.SQLiteStore` satisfies whatever interface
`memoryservice.New` expects (or what you found/had to do if it didn't compile cleanly), where in
`agenthost.New` the memory wiring happens relative to the MCP wiring from piece 9, how you isolated
the global `tool.LocalMemoryService` var in tests, and full `go test -race -count=1` output.
