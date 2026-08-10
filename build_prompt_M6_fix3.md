I redid the full manual browser pass after your overlay/CSS fix. The overlay fix works correctly — I confirmed the real shell prompt renders after taking control and the overlay properly disappears and stops blocking clicks. But I found a third bug, and this one is severe: **typing into the terminal has never actually worked at all**, not even once, in any of the manual passes so far (the overlay bug was masking this — I could never get far enough to actually type until now).

## The bug

After taking control and getting a real, focused terminal with a live shell prompt, I typed a real command (`echo hello_from_browser_verified` + Enter) via real keyboard events. Nothing appeared in the terminal. Instead:
- A banner appeared: `invalid control payload`
- The terminal then showed `Reconnecting to tmp...` — the WebSocket connection was closed and the frontend's reconnect logic kicked in.

## Root cause, confirmed by reading the code

In `termserver/assets/app.js`, `terminal.onData`:

```js
terminal.onData(function (data) {
  ...
  if (state.socket && state.socket.readyState === WebSocket.OPEN) {
    state.socket.send(data);
  }
});
```

`data` here is a plain JavaScript **string** (that's what xterm.js's `onData` callback provides — the raw UTF-8 characters typed). Calling `WebSocket.prototype.send()` with a **string** argument always sends a **text** WebSocket frame — there is no way around this in the browser WS API; to send a binary frame you must pass an `ArrayBuffer`, `Blob`, or typed array, never a plain string.

But on the server side, `termserver/server.go`'s `handleTerminalStream` only treats `websocket.BinaryMessage` frames as terminal input (routed to `session.WriteWithLease(...)`). A `websocket.TextMessage` frame is *always* interpreted as a JSON control message (`handleControlMessage`, expecting `{"type":"resize",...}` or `{"type":"takeover",...}`). Since every keystroke the frontend sends arrives as a **text** frame (per the JS `WebSocket.send(string)` behavior above), the server tries to `json.Unmarshal` raw typed characters like `"e"` or `"echo hello_from_browser_verified\r"`, fails, responds with `{"type":"error","error":"invalid control payload"}`, and — critically — `handleControlMessage`'s JSON-parse-failure path returns `keepGoing=false`, which makes `handleTerminalStream` return and **close the connection entirely**. That's the reconnect loop I observed.

This means: **no keystroke has ever reached a real shell through this UI, in any state, since M6 was built.** The control-message plumbing (resize, takeover) all uses JSON text frames correctly and works (I confirmed takeover works end-to-end). Only the actual terminal-input path is broken, and it's broken 100% of the time, not intermittently.

## What I need fixed

In `terminal.onData`, encode the string as bytes before sending, so it goes out as a **binary** WS frame matching what the server expects — e.g.:

```js
state.socket.send(new TextEncoder().encode(data));
```

(`TextEncoder().encode(...)` returns a `Uint8Array`, which `WebSocket.send()` sends as a binary frame.) Verify this is actually the right fix by checking how the server decodes binary input on the other end (`session.WriteWithLease(writeLease, msg.payload)` — confirm `msg.payload` is raw bytes and that writing UTF-8-encoded bytes into the PTY is exactly equivalent to what the shell expects, which it should be, but double check rather than assume).

## What I need you to also check, given how this slipped through

Every existing automated test for this app talks to the WS using a real WS client library server-side-to-server-side (Go `gorilla/websocket` in your tests), and I'd guess your tests explicitly send binary frames when simulating terminal input (matching the server's expectation), which is why no test caught that the **actual browser-side JS** sends the wrong frame type. This is a real gap in test coverage for the JS layer specifically. I know M6's own scope said browser-behavior testing is manual-only, and that's fine, but given this bug went through two previous "fixed, please re-verify" cycles without being caught, I want you to:
1. Re-verify this fix by literally reasoning through the browser `WebSocket.send()` API contract for string vs. `Uint8Array`/`ArrayBuffer`/`Blob` arguments (cite the spec/MDN behavior, don't just assert it), since I got a class of "should work" claim wrong twice already in this milestone (the overlay CSS one) and want to avoid a third round of "looks right but isn't."
2. Tell me plainly whether there's any other place in `app.js` where a string vs. binary WS payload type mismatch could exist (e.g., double check that resize/takeover control messages are sent as **text** — they should be, since the server expects `websocket.TextMessage` for those, via `JSON.stringify(...)` which produces a string, so `socket.send(JSON.stringify(...))` is correct as-is for those — just confirm this explicitly rather than assuming only the input path had this bug).

## Verification I want

After the fix, build the binary, run it, and this time actually verify the full loop yourself if you're able to script it end-to-end (a Go WS test client sending a **string** the way a browser would construct a binary frame from is hard to simulate perfectly outside a real browser, so I understand if full parity isn't achievable in your test environment — but at minimum add/update a Go-level test that sends a binary frame with typed-looking content and confirms it reaches the fake PTY, if one doesn't already exist, so the server side of this contract stays pinned). I will do the actual real-browser keystroke test myself again afterward — don't tell me this is done until you've either verified it directly or explained clearly why you couldn't and what remains manual-only.

Run `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` and paste real output as usual.
