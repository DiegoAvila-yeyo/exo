You are building milestone M5 of `exo` (module `github.com/DiegoAvila-yeyo/exo`, at `~/exo`). M1-M4 are done, reviewed, and (M4) manually verified live on macOS with real `launchd`. Read `sessions/manager.go`, `realpty/realpty.go`, and `backend/backend.go` before starting — M5 builds crash recovery on top of the process-group foundation M3 already laid down (recall: M3's `Close()` was fixed to kill a shell's whole process-group tree, not just its PID, specifically so M5 would have something to build on). This is a real build task — write actual Go code, write tests, run them, paste real output.

## Design context (from `~/tesla/DASHBOARD_TERMINAL_DESIGN.md`, round 10, closed)

- Treat a backend crash as **session loss, not reattachable recovery** — no real PTY reattachment in v1. The goal of M5 is narrower: when the backend crashes (not a clean shutdown) and a *new* backend instance starts up later, that new instance must find and kill any leftover shell process groups from the dead instance, so they don't linger as orphaned, silently-running processes forever.
- Mechanism: each session gets persisted metadata (`session_id`, `backend_instance_id`, `shell_pid`, `process_group_id`, `started_at`, `status`), and the child process gets environment markers (`NUCLEO_SESSION_ID`, `NUCLEO_BACKEND_INSTANCE_ID`) so a later reconciliation pass can confidently identify "is this still-alive process group actually one of ours from a previous run, or just a PID/PGID that got reused by something unrelated." On next backend startup, reconciliation loads persisted sessions that weren't marked cleanly closed, checks if their process group is still alive **and** carries the expected env marker, and if so kills it and marks it `stale_reaped` (or `stale_orphaned` if, for whatever reason, it couldn't be killed).
- This mechanism was explicitly chosen over heuristic scanning by command line without a marker ("too fragile" — round 10 rejected that).

## What to build

### 1. Environment markers on spawned shells

Extend `realpty` so `New` (or a new variant) accepts extra environment variables to inject into the spawned shell, so `sessions.Manager` can set `NUCLEO_SESSION_ID=<id>` and `NUCLEO_BACKEND_INSTANCE_ID=<instance-id>` on every real PTY it creates. Keep backward compatibility with however `realpty.New`/`sessions.Manager.Create` are currently called elsewhere (update all call sites as needed).

### 2. A backend instance ID

`backend.Run()` needs a per-process-lifetime instance ID (random, e.g. a UUID or hex token — generated once per `Run()` call, not persisted/reused across restarts). Thread it through to wherever sessions get created.

### 3. Persisted session metadata (new package, suggest `sessionstore/`)

A store that persists the metadata described above to disk under `~/Library/Application Support/exo/sessions/` (one JSON file per session ID is a reasonable shape, or a single JSONL log — your call, document it and justify briefly). Needs at least:
- `Record(info SessionMetadata) error` — called on session creation.
- `MarkClosed(id string) error` — called when a session is cleanly closed by the running backend itself (normal `Close()` path), so reconciliation on a *future* run knows this one doesn't need cleanup.
- `MarkExited(id string) error` — called when the underlying shell process exits on its own (there's already a `watchExit` concept in `sessions.Manager` from M3 — hook into it), similarly not needing cleanup later.
- `ListUnreconciled(currentInstanceID string) ([]SessionMetadata, error)` — returns persisted records that belong to a *different* (i.e. previous/dead) `backend_instance_id` and were never marked closed/exited — these are the candidates for reconciliation.
- `MarkReconciled(id, status string) error` — updates a record's status to `stale_reaped` or `stale_orphaned` after reconciliation processes it.

Wire this into `sessions.Manager`: `Create` records a new entry, `Close` marks it closed, the existing `watchExit` goroutine marks it exited (whichever happens first "wins" — a session that's explicitly `Close()`'d shouldn't also need to be marked exited, handle that sensibly).

### 4. Reconciliation pass

A function (suggest living in `sessionstore/` or a new `reconcile/` package, your call) that, given the store and the current instance ID, does at backend startup, **before the backend starts accepting new session-creation requests** (but this must stay fast — this runs on every `launchd`-triggered cold start, so don't let it meaningfully slow down startup for the common case where there's nothing to reconcile):
- Calls `ListUnreconciled(currentInstanceID)`.
- For each stale record, checks whether `process_group_id` is still alive. Since macOS has no `/proc`, use the same `ps`-based approach `realpty` already uses (or `syscall.Kill(-pgid, 0)` to test liveness without actually signaling — check what that returns for a nonexistent vs. existing process group on macOS and use whichever check is actually reliable).
- If alive, additionally confirm via `ps` (or however you access process environment/command info — note macOS doesn't trivially expose another process's environment variables to unprivileged callers the way Linux's `/proc/<pid>/environ` does, so investigate whether checking the env marker is actually feasible here, the same way you investigated `Setpgid` in M3 and the `launchd` socket mechanism in M4 — if reading another process's env isn't feasible on macOS without elevated privileges, say so plainly and propose what confirmation signal *is* actually available and reasonably reliable instead, e.g. matching on `pgid` plus recorded `shell_pid` plus a recorded start-time proximity check, rather than silently dropping the "confirm identity" requirement).
- If confirmed as one of ours and alive, kill the process group (SIGTERM, wait briefly, SIGKILL — same escalation pattern `realpty.Close()` already uses) and mark `stale_reaped`.
- If not alive at all, mark `stale_reaped` too (nothing to clean up, it already died on its own).
- If alive but couldn't be confirmed/killed for some reason, mark `stale_orphaned` and move on — don't let one problematic record block reconciliation of the others or block backend startup.

Wire this into `backend.Run()`: generate the instance ID, construct the store, run reconciliation once at startup (after acquiring the singleton lease, before opening the listener), then proceed as before.

## Tests to write

1. `sessionstore`: record → mark closed → `ListUnreconciled` for a *different* instance ID excludes it (it was cleanly closed); record → don't mark closed/exited → `ListUnreconciled` for a different instance ID includes it (simulating a crash).
2. Reconciliation, using a real process group you spawn in the test (not the full `realpty`/shell stack necessarily — a plain `exec.Command` with `Setpgid` is fine for this test's purposes) to simulate "a previous instance's leftover process group still alive": seed the store with a stale record pointing at that real, still-alive process group, run reconciliation with a *different* instance ID, and verify the process group is actually killed afterward and the record ends up `stale_reaped`.
3. Reconciliation correctly leaves alone a stale record whose process group is already dead (verify it's marked `stale_reaped` without erroring, and doesn't try to signal a nonexistent group in a way that blows up).
4. End-to-end via `backend.Run()`: start a backend instance, create a real session (same double-submit-cookie dance the M4 manual verification used, or expose a test-only bypass if that's cleaner for this specific test — your call), forcibly kill that backend process's own Go process *without* it going through its normal `Close()` path (simulating an actual crash, not a clean shutdown) while leaving the underlying shell process group alive, then start a *second* `backend.Run()` instance pointed at the same store/lock path and confirm the leftover shell process group from the first instance is gone after the second instance's startup reconciliation runs.

Run `go build ./...`, `go vet ./...`, and `go test -race ./...`. Paste real output.

## What I want back

Report covering: package/file layout, the exact `SessionMetadata` shape and store file format you settled on, how you solved (or couldn't solve, and what you did instead) the "confirm this process is actually ours" identity-check problem on macOS, confirmation of `go build`/`go vet`/`go test -race` passing with real pasted output, and any judgment calls beyond what's specified here.
