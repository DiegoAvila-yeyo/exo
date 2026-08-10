You are building milestone M3 of `exo` (module `github.com/DiegoAvila-yeyo/exo`, at `~/exo`). M1 (`ptyactor`) and M2 (`termserver`) are done and reviewed. Read `ptyactor/session.go` and `termserver/server.go` before starting. This is a real build task — write actual Go code, write tests, run them, paste real output.

## Important context: M1/M2 only ever used a fake PTY. This milestone introduces the first real one.

Everything so far (`ptyactor.PTY` interface, the fake used in tests) has been exercised only with an in-memory fake. M3 needs actual PTY sessions running a real shell, plus support for multiple concurrent sessions in one backend process.

I checked `nucleo-base`'s existing terminal manager (`layer2-runtime-rails/terminal/manager.go`) to see if it could just be reused directly here — it can't, cleanly, for this purpose: its `Write` takes a `string` with cursor/wait/maxBytes semantics and its `Read` is cursor-based file tailing (designed for the agent's discrete tool calls), not a raw `io.Reader`/`io.Writer`. It also has **no `Resize` method at all** — it never calls a PTY resize syscall anywhere. Forcing `ptyactor.PTY`'s raw byte-stream interface on top of that mismatched API would fight the existing code rather than reuse it cleanly. So: **build a new, separate real-PTY adapter in `exo`** that implements `ptyactor.PTY` directly by spawning a real shell process attached to a real pseudo-terminal (suggest `github.com/creack/pty` for the actual PTY allocation — a small, well-established library — add it properly to `go.mod`/`go.sum`; pick a different one if you have a good reason, just say why). This is a new package (suggest `realpty/` or similar), independent of `nucleo-base`'s `terminal.Manager` — don't try to wrap or modify that package for this.

## What to build

### 1. Real PTY adapter (`realpty` or your chosen name)

A type implementing `ptyactor.PTY` (`Write([]byte) (int, error)`, `Read([]byte) (int, error)`, `Resize(cols, rows int) error`, `Close() error`) that:
- Spawns a real shell (default `$SHELL` or fall back to `/bin/zsh` or `/bin/bash` if unset — pick one and document it) as a child process attached to a real PTY via the library you chose.
- Accepts a working directory to `cd` into (the shell's `cmd.Dir`, not a `cd` command typed into the shell).
- Supports `Resize` via the real PTY resize syscall (the library you picked should expose this — `github.com/creack/pty` has `pty.Setsize`).
- `Close()` terminates the child process cleanly (send it a signal, wait briefly, escalate if needed — don't leave zombies).

### 2. Multi-session manager

A new type (suggest `SessionManager` in a `sessions/` package) that holds multiple named PTY sessions, each backed by one `ptyactor.Session` wrapping one `realpty` instance. Track per-session metadata:
```go
type SessionInfo struct {
    ID           string
    Name         string
    Workdir      string
    Command      string // what shell/command was launched, for display
    Status       string // e.g. "running", "exited", "closed"
    CreatedAt    time.Time
    LastActiveAt time.Time
}
```
Methods along the lines of: `Create(workdir, name string) (SessionInfo, error)` (spawns a new real PTY + `ptyactor.Session`, auto-generates a name from `filepath.Base(workdir)` if `name` is empty), `List() []SessionInfo`, `Get(id string) (*ptyactor.Session, SessionInfo, bool)`, `Close(id string) error`. Use whatever exact signatures are idiomatic — behavior matters more than exact naming.

Decide and document: what happens if `Create` is called with a `workdir` that doesn't exist or isn't a directory (should fail cleanly, not spawn a broken session). Is there a reasonable cap on concurrent sessions for v1 (pick a small sane default, e.g. 10, and make it configurable) to avoid unbounded PTY spawning — document what you chose and why.

### 3. Wire this into `termserver`

- `GET /api/sessions` — lists current sessions (JSON array of `SessionInfo`), protected by the existing `Origin` allow-list (read-only, so no double-submit needed — GET requests aren't state-changing).
- `POST /api/sessions` — creates a new session, body `{"workdir": "...", "name": "..."}` (name optional), protected by `Origin` allow-list + the existing double-submit cookie check (this is state-changing). Returns the created `SessionInfo`.
- `DELETE /api/sessions/{id}` or `POST /api/sessions/{id}/close` (your choice, document it) — closes a session, same auth as creation.
- The terminal WebSocket endpoint changes from the M2 single-hardcoded-session shape to `GET /api/terminal/{sessionID}/stream` — it must look up the session by ID via the `SessionManager`, return 404 if it doesn't exist, and otherwise behave exactly as M2's endpoint did (auth, subscribe, lease broadcast on takeover, etc.) but scoped to that specific session's `ptyactor.Session`. The multi-connection lease-broadcast tracking from M2 (the `clients` map) needs to become per-session, not global to the whole server, since different sessions have independent ownership/epoch state.

## Tests to write

1. `realpty` adapter: spawn a real shell, write a command (e.g. `echo hello-from-pty` + newline), read back output containing that text (real PTY, real process — this test actually runs a shell, that's fine and expected here, unlike M1/M2's fake-only tests). Verify `Resize` doesn't error. Verify `Close` actually terminates the child (e.g., check the process is no longer running afterward, however you can verify that cleanly in Go).
2. `SessionManager`: `Create` with a valid workdir succeeds and shows up in `List`; `Create` with a nonexistent workdir fails cleanly without leaking a broken session into `List`; `Close` removes it from `List` (or marks it closed, whichever you chose) and actually terminates the underlying PTY; creating more than the configured cap fails with a clear error.
3. `termserver` integration: `GET /api/sessions` reflects created sessions; `POST /api/sessions` creates one end-to-end (real shell, not fake, this time) and a client can then connect to `GET /api/terminal/{id}/stream` for that specific session, write to it, and read real shell output back; connecting to a nonexistent session ID returns 404; two different sessions have fully independent ownership/epoch state (a takeover on session A must not affect session B's lease at all — write a test proving this).

Run `go build ./...`, `go vet ./...`, and `go test -race ./...`. Real PTY tests may be slightly slower/flakier than pure in-memory ones — if you need a longer timeout or a small retry/poll instead of a fixed sleep for waiting on shell output, use polling with a deadline, not a blind `time.Sleep`. Paste real output.

## What I want back

Report covering: package/file layout, the `realpty` library you chose and why, the `SessionInfo` shape and manager API you settled on, the final HTTP/WS route shapes, confirmation of `go build`/`go vet`/`go test -race` all passing with real pasted output, and any judgment calls beyond what's specified (default shell fallback, session cap default, workdir validation behavior, etc.).
