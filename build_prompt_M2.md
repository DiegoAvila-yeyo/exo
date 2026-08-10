You are building milestone M2 of `exo` (module `github.com/DiegoAvila-yeyo/exo`, at `~/exo`). M1 is done: package `ptyactor` (see `ptyactor/session.go`, `ptyactor/ring.go`) implements an actor-goroutine-per-PTY-session model with `Session`, `Lease`, `Write`/`WriteWithLease`, `Subscribe`/`SubscribeWithLease`, `Resize`, `Takeover`, `ErrOwnershipLost`. It's already reviewed and passing `go test -race`. Read it before starting — M2 builds the browser-facing transport layer on top of it. This is a real build task — write actual Go code, write tests, run them, paste real output in your report.

## Design context (from `~/tesla/DASHBOARD_TERMINAL_DESIGN.md`, rounds 1-4, closed — don't re-litigate)

- WebSocket auth: the client passes a bearer token via the `Sec-WebSocket-Protocol` header, formatted as `nucleo-term.<token>` (this is the mechanism browsers actually support for WS auth, since custom headers aren't settable on `WebSocket`/`EventSource`). The server must echo back exactly one of the client-offered subprotocols to complete the handshake per the WS spec — echo back the full matched value only if the token inside it is valid; otherwise reject the upgrade with 403 before completing the handshake.
- Strict `Origin` allow-list on every route that matters (the WS upgrade, and any state-changing HTTP route): accept exactly `http://127.0.0.1:<port>` and `http://localhost:<port>` for the server's own listening port, reject everything else. No wildcard CORS anywhere.
- For plain HTTP routes (not the WS), auth is a double-submit cookie: server sets an `HttpOnly`, `SameSite=Strict` cookie with a random value on first page load; the served HTML embeds that same value (e.g. in a `<meta>` tag); the page's own JS is expected to read the meta tag and send that value back as a normal body/query field on state-changing requests (not a custom header — avoids CORS preflight complications); the server checks that the submitted value matches the cookie value.
- This is deliberately "local bearer-style" auth for a single trusted user on their own Mac, not a full multi-user auth system — don't over-build beyond what's specified.

## What to build

A new package, suggest `termserver/` (pick a clear name, say what you picked), providing an `http.Handler`-based server with:

1. **Token issuance**: on server construction, generate a random token (e.g. 32 bytes, hex-encoded) held in memory for the process lifetime. Expose it internally for wiring into a served HTML page (a minimal one is fine for this milestone — full dashboard UI is a later milestone) via a `<meta name="nucleo-token" content="...">` tag, alongside setting the double-submit cookie described above on that same response.

2. **Origin allow-list helper**: a small reusable function/middleware that, given the server's own port, validates an incoming `Origin` header against `http://127.0.0.1:<port>` and `http://localhost:<port>`, used by both the WS upgrade path and any state-changing HTTP route.

3. **Double-submit cookie helper**: middleware/helper that verifies a submitted value (from request body or query, your choice, document which) against the cookie value, for POST-style routes. Build one demo protected route (e.g. `POST /api/echo` that just echoes back a JSON body) to prove the mechanism works end-to-end — this doesn't need to be a real feature, just a testable demonstration of the double-submit check functioning.

4. **The terminal WebSocket endpoint**: something like `GET /api/terminal/stream` (single hardcoded session for this milestone — multi-session support is a later milestone, so it's fine to have exactly one `ptyactor.Session` instance wired to the server for now, backed by a fake/in-memory `PTY` implementation for testing, same interface as M1's `PTY`). On a valid connection (passes Origin check + WS subprotocol token check):
   - Subscribe to the session's output via `Subscribe()`, forward scrubbed output bytes to the client as **binary** WS frames.
   - Accept **binary** WS frames from the client as raw input, call `Write()` on the session.
   - Accept **text** WS frames as JSON control messages: `{"type":"resize","cols":N,"rows":N}` → calls `Resize()`; `{"type":"takeover","owner":"human"}` → calls `Takeover()`. Send back JSON status frames to the client: `{"type":"ready"}` right after a successful subscribe, `{"type":"ownership_lost"}` if a write/resize comes back with `ErrOwnershipLost`, `{"type":"lease","owner":"...","epoch":N}` after a successful takeover.
   - On WS disconnect, unsubscribe cleanly (call the unsubscribe func from `Subscribe()`).

## What I want you to decide and document (there's no single right answer, pick one and justify)

- Which WebSocket library you use (the repo has no existing WS dependency — pick a well-maintained one, e.g. `github.com/gorilla/websocket` or `nhooyr.io/websocket`, add it properly to `go.mod`/`go.sum`).
- Exact JSON message shapes for control/status frames (the ones above are illustrative, refine as needed, just document the final wire format clearly).
- How you structure the origin/token validation so it's easily reusable once multi-session support (M3) and the real dashboard (M6) land on top of this.

## Tests to write

Using `httptest.NewServer` (or equivalent) and a real WS client (from whatever library you picked) connecting to it:

1. WS upgrade succeeds when Origin and token are both valid.
2. WS upgrade is rejected (should fail before/during handshake, not silently accept then error) when Origin is missing or not in the allow-list, even with a valid token.
3. WS upgrade is rejected when the token in `Sec-WebSocket-Protocol` is missing or wrong, even with a valid Origin.
4. End-to-end: after a valid connection, a resize control message actually reaches the underlying fake PTY's `Resize()` call; a binary input frame reaches the fake PTY's `Write()`; output emitted by the fake PTY arrives at the client as a binary frame.
5. A `takeover` control message changes the session's owner/epoch and the client receives the `{"type":"lease",...}` confirmation; a subsequent write attempt from a second, still-connected client using the old lease gets `{"type":"ownership_lost"}` (you'll need to expose enough of the session/lease plumbing to construct this test — reuse `ptyactor.Session` directly from the test if that's easier than routing it through the WS layer for the "old lease" side).
6. The double-submit demo route (`/api/echo` or whatever you named it) rejects a request with a missing/mismatched submitted value even when the cookie is present, and accepts one where they match.

Run `go build ./...`, `go vet ./...`, and `go test -race ./...` in `exo` before reporting done — paste the real output.

## What I want back

Report covering: package/file layout, the exact WS message wire format you settled on, which WS library you chose and why, confirmation of `go build`/`go vet`/`go test -race` all passing with real pasted output, and any judgment calls you made beyond what's specified here.
