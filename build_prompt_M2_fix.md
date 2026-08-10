I reviewed the M2 `termserver` code directly (not just your report) and ran `go test -race -count=3` — it passes, but I found a real behavioral gap the tests don't catch, plus a minor security nit. Fix both.

## 1. Real gap: other connections go silently dark on takeover, never notified, never resubscribed

In `handleTerminalStream`, when a client sends a `takeover` control message, `handleControlMessage`'s `"takeover"` case calls `s.session.Takeover(msg.Owner)`, which (correctly, per `ptyactor`) closes **every** current subscriber's output channel, not just the initiating connection's. But only the initiating connection resubscribes (via `SubscribeWithLease(nextLease)`) and receives the `{"type":"lease",...}` status message. Every *other* currently-connected WS client (e.g., a second browser tab just observing, or the previous owner) has their `outputCh` close underneath them — in the main loop, `case chunk, ok := <-outputCh: if !ok { outputCh = nil; continue }` just sets it to `nil` and keeps looping forever with no output and **no status message ever sent to that client**. Their terminal view silently freezes with no error, no lease notification, nothing — they only find out something changed if they happen to try writing, which is exactly what your existing test (`TestTakeoverReturnsLeaseAndOldClientLosesOwnershipOnNextWrite`) checks, but it never checks the live-output path, which is why this slipped through.

This contradicts the round 2-4 design principle that observing (reading live output) should never be interrupted or gated by ownership changes — only writes should be epoch-gated.

Fix this: when a takeover happens on a session, every connection currently attached to that session (not just the one that initiated it) needs to (a) learn about the new lease, and (b) get automatically resubscribed under the new epoch so their live view keeps working uninterrupted. Concretely, this likely means:
- The `termserver.Server` needs to track all currently-connected WS handlers for a given session (not just let each `handleTerminalStream` goroutine be independent and unaware of siblings).
- When any connection triggers a takeover, broadcast the new `{"type":"lease","owner":...,"epoch":...}` status message to *all* connected clients for that session, and have each one's loop transparently resubscribe (call `SubscribeWithLease` again with the new lease) so their `outputCh` keeps flowing — the human/agent who lost write ownership should keep *watching* seamlessly, they just can't write anymore until they take control back.
- Write a test that specifically catches the bug you just had: two clients connected, one takes over, and assert the *other* client still receives subsequently-emitted PTY output (not just that it gets `ownership_lost` on its next write attempt). That's the test that would have caught this.

## 2. Minor: use constant-time comparison for the WS auth token

`matchTokenSubprotocol` compares the offered subprotocol against the expected token with a plain `==` string comparison. The CSRF double-submit check can stay as-is (lower stakes, and it's already compared against a cookie value that's equally guessable/unguessable from the same origin), but the WS token is the actual bearer secret protecting the terminal — compare it using `crypto/subtle.ConstantTimeCompare` (or an equivalent constant-time helper) instead of `==`, to avoid a timing side-channel on the token itself.

## What I want back

Fix both, re-run `go build ./...`, `go vet ./...`, and `go test -race ./...`, paste real output, and confirm the new test for issue #1 actually fails against the old code (describe how you verified that, e.g. by temporarily reverting the fix) and passes against the fixed code.
