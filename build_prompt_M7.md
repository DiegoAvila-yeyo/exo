You are building milestone M7 of `exo` (module `github.com/DiegoAvila-yeyo/exo`, at `~/exo`). M1-M6 are done, reviewed, and manually verified end-to-end in a real browser. This milestone closes out the accumulated hardening list from earlier reviews — three specific, low-risk-but-real items, not new features. This is a real build task — write actual code, write tests where applicable, run them, paste real output.

## Item 1 (from M1): replace the secret-scrubbing stub with real detection

`ptyactor/session.go` currently has:
```go
var scrubPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret)\s*=\s*[^\s]+`)
```
This was explicitly a placeholder for M1, wired into the single choke point where output gets persisted/delivered (both live stream and ring-buffer replay pass through it — don't change that architecture, just improve what `scrub()` actually detects). Improve it to cover common real-world secret shapes that would plausibly appear in terminal output, not just `key=value` style. At minimum, add patterns for: common cloud provider key formats (e.g. AWS access keys `AKIA[0-9A-Z]{16}`, generic high-entropy bearer-token-looking strings), private key headers (`-----BEGIN ... PRIVATE KEY-----`), and standard env-var-style secret assignments beyond just `api_key`/`token`/`secret` (e.g. `password`, `passwd`, `_key`, `_token`, `_secret` as suffixes, common patterns like `Authorization: Bearer ...`). Don't try to build a perfect universal secret scanner — this is terminal output scrubbing for a personal dev tool, not a compliance product — but make it meaningfully better than the current single-pattern regex. Document exactly what patterns you added and why, and note explicitly what classes of secrets it still won't catch (be honest about the limits, don't oversell it).

Add/update tests in `ptyactor` exercising the new patterns (a table-driven test covering each pattern category is probably the cleanest approach) — verify each triggers redaction and that ordinary non-secret-looking terminal output is NOT redacted (avoid over-matching common words).

## Item 2 (from M2): make `broadcastLease` non-blocking

`termserver/server.go`'s `broadcastLease` currently does:
```go
func (s *Server) broadcastLease(sessionID string, lease ptyactor.Lease, writerClient *wsClient) {
	s.hubsMu.Lock()
	defer s.hubsMu.Unlock()
	hub, ok := s.hubs[sessionID]
	if !ok {
		return
	}
	for client := range hub.clients {
		client.leaseUpdates <- leaseUpdate{lease: lease, canWrite: client == writerClient}
	}
}
```
This blocks while holding `hubsMu` if any client's `leaseUpdates` channel is full. Change this to a non-blocking send (`select` with a `default` case) — if a client's channel is full, don't block the broadcast; instead, either drop that specific client's lease update (acceptable, since a slow client will still get the correct state on its next interaction/reconnect) or mark that client for disconnection, whichever is simpler and consistent with how other parts of this codebase already handle slow consumers (check `ptyactor`'s own fanout logic from M1 for the established pattern — the actor loop already handles slow subscribers there by dropping/disconnecting rather than blocking, mirror that same philosophy here).

Add a test that constructs a client with a full/blocked `leaseUpdates` channel and confirms `broadcastLease` returns promptly (doesn't block) and other clients still get their update.

## Item 3 (from M4): fix the misleading `Boot-out failed` message on fresh install

`cli.Install()` currently does:
```go
_ = c.runner.Run(ctx, "launchctl", launchagent.BootoutArgs(c.launchUID(), plistPath)...)
return c.runner.Run(ctx, "launchctl", launchagent.BootstrapArgs(c.launchUID(), plistPath)...)
```
The bootout call is expected to fail on a fresh install (nothing was loaded yet), and the Go code correctly ignores that error — but `execRunner.Run` pipes the child process's `stderr` straight to `os.Stderr`, so the user sees `launchctl`'s own alarming "Boot-out failed: 5: Input/output error" text even though nothing actually went wrong. Fix this so a fresh install doesn't print a scary, misleading message: either suppress `stderr` specifically for this best-effort bootout-before-install call (capture it instead of piping live, and only surface it if you decide it's actually useful context for a later failure), or print your own clear message instead (e.g. nothing at all, or a quiet "no existing installation found" only if you can distinguish that case from a real unexpected error — check what exit code / stderr pattern `launchctl bootout` actually produces for "wasn't loaded" vs. a genuine problem, don't just blanket-suppress everything). Use your judgment on the exact approach, but the end state must be: running `exo install` for the first time ever should not print anything that looks like a failure.

Also, from the same M4 note: a 0-byte `backend.lock` file is left behind after `exo uninstall` — this is normal `flock` behavior and not a functional bug, but if there's a trivial, safe way to clean it up on uninstall (e.g., `uninstall` also removes the lock file, being careful not to do this while the backend might still be running/holding it), do so; if there's any risk of racing a live process, leave it as-is and just say why you left it.

## Verification

Run `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` and paste real output. For item 3 specifically, since it's about console output rather than a return value, describe how you verified the fix (e.g., actually running `exo install` fresh — remove any existing plist/lock first — and pasting the real terminal output showing no scary message, the same way I did manual verification for M4's `launchd` behavior).

## What I want back

Report covering: exact patterns added for item 1 and their limits, the non-blocking mechanism chosen for item 2 and how it mirrors the M1 actor pattern, the exact fix for item 3 and how you verified it end-to-end, confirmation of `go build`/`go vet`/`go test -race` passing with real output, and any judgment calls made beyond what's specified here.
