I did a real manual browser pass on M6 (not just Go-level tests) and found a blocking functional bug: the app doesn't actually work in a real browser.

## The bug

I ran the built binary (`go build -o exo . && ./exo serve`, fallback listener since no `launchd`) and opened `http://127.0.0.1:45873/` in a real browser. Confirmed:

- `fetch('/api/sessions')` (GET, called by `refreshSessions()` on page load and by the Refresh button) returns **403 forbidden origin**.
- `fetch('/api/sessions?csrf_token=...', {method: 'POST', ...})` (session creation) returns **201**, works fine.
- Confirmed directly with curl: `curl http://127.0.0.1:45873/api/sessions` (no `Origin` header) → 403. `curl -H "Origin: http://127.0.0.1:45873" http://127.0.0.1:45873/api/sessions` → 200.

Root cause: real browsers do **not** reliably send an `Origin` header on same-origin, simple `GET` requests (they do send it for cross-origin requests and for state-changing methods like `POST`, which is why session creation worked). `ValidOrigin` in `termserver/security.go` treats a missing/empty `Origin` header as automatically invalid (`if origin == "" || port == "" { return false }`), and `handleSessions`'s `GET` case calls `ValidOrigin` unconditionally. So the very first thing the frontend does on page load — list existing sessions — gets rejected, and the Refresh button is permanently broken. This isn't a corner case, it's the primary path, and it means M6 as shipped doesn't actually work end-to-end in a real browser despite all the Go-level tests passing (they passed because Go's `http.Client` in the tests explicitly sets an `Origin` header, which masked this).

This is also something discussed back in round 4 of the original design doc (`~/tesla/DASHBOARD_TERMINAL_DESIGN.md`): Codex's own answer then was "requests with no Origin header are not a bypass in the browser threat model... a malicious cross-origin page cannot suppress Origin on a fetch it sends. So 'no Origin => no CORS header' is fine" — meaning the intent was always that a missing Origin on a GET is an ordinary same-origin browser request, not an attack, and should be allowed through. That principle just never got implemented for the actual `GET /api/sessions` route until this real-browser test surfaced it.

## What I need fixed

Adjust the Origin-checking policy so real same-origin browser requests actually work:

- For **read-only GET routes** (`GET /api/sessions`, and any other pure-read route), treat a **missing** `Origin` header as fine (ordinary same-origin request), and only reject when an `Origin` header **is present and doesn't match** the allow-list (that's the actual cross-origin attack case — a hostile page's `fetch` will include `Origin`, and that's exactly what should get blocked).
- For **state-changing routes** (`POST /api/sessions`, `POST /api/sessions/{id}/close`, `POST /api/echo`) and the **WebSocket upgrade**, keep the current strict behavior (these already work correctly per my test, and they're higher-stakes) — don't weaken those. If you determine those should also tolerate a missing Origin for some legitimate reason, justify it explicitly rather than doing it as a blanket change; my instinct is they should stay strict since they mutate state and browsers reliably send Origin for them anyway (confirmed by my POST test working).
- Update `ValidOrigin` (or add a variant) to express this distinction clearly, and update the call sites in `termserver/server.go` accordingly.

## Verification I want

1. Re-run my exact repro after the fix: build the binary, run `./exo serve` (or run the existing Go test suite plus a new one — see below), and confirm `curl http://127.0.0.1:45873/api/sessions` (no Origin header) now returns 200, while `curl -H "Origin: http://evil.example" http://127.0.0.1:45873/api/sessions` (a real cross-origin value) still returns 403.
2. Add an actual Go test for this in `termserver/server_test.go`: a GET request to `/api/sessions` with no `Origin` header at all must succeed (this is the case that was broken and that none of the existing tests caught — check why the existing Origin-rejection tests didn't catch this: I'd guess they only tested "wrong Origin present," never "Origin absent," which is exactly the gap).
3. Run `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` and paste real output.
4. After that, tell me explicitly once you're confident I can redo my manual browser pass — I'll re-verify the full UI flow (session list loading, create, connect, take control) live again before we close M6, since this is exactly the kind of gap that only shows up with a real browser and I don't want to sign off on M6 without seeing it actually work end-to-end this time.
