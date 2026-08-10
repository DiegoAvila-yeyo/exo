I redid the manual browser pass after your Origin fix. Good news: session listing, creation, and the WS connection all work correctly now — I saw "Ready" / "Agent has control" render correctly on connect, exactly as designed. But I found a second real, blocking bug while continuing the flow (clicking "Take control" and then trying to actually use the terminal).

## The bug

After clicking "Take control": the connection badge correctly updates to "You have control" (confirming the JS state — `state.owner`, the lease flow, all of that is correct). But the terminal overlay (the "Agent has control / Typing and paste stay locked..." panel) **never visually disappears**, even though the JS sets `refs.terminalOverlay.hidden = true` in that state (per `updateOverlay()`'s logic, no branch matches "ready && owner===human" so it should hit the `if (!title) { refs.terminalOverlay.hidden = true; return; }` path).

Root cause, confirmed: in `termserver/assets/app.css`, `.terminal-overlay` has `display: flex` set directly (around line 324), and there is **no** `.terminal-overlay[hidden] { display: none; }` (or equivalent) rule anywhere in the stylesheet. Author CSS with an explicit `display` property always wins over the browser's built-in default `[hidden] { display: none }` UA-stylesheet rule — so setting the `hidden` HTML attribute via JS has no visual effect at all on this element. It just silently doesn't work.

This isn't only cosmetic: because `.terminal-overlay` is `position: absolute; inset: 16px` sitting on top of the terminal, and it never actually gets hidden, **it also keeps intercepting clicks** — I confirmed by clicking directly into the terminal area after taking control and typing "echo hello_from_browser" + Enter: nothing reached the shell, because the click never focused xterm.js's underlying input (the overlay div was still there capturing the click instead). So after a human successfully takes control, **the terminal is completely unusable** — you can see the connection state say "you have control," but you can't actually interact with the terminal at all. This is a full break of the core feature, not a visual nit.

## What I need fixed

1. Add the missing CSS rule so `hidden` actually works for this element: `.terminal-overlay[hidden] { display: none; }` (this needs to come after/override the `.terminal-overlay { display: flex; ... }` rule, or use higher specificity — check that ordering/specificity actually works, don't just add it blindly and assume). Alternatively, if you prefer not to rely on the `hidden` attribute at all, switch to toggling a class (e.g. `.terminal-overlay.is-hidden { display: none; }`) and update the JS to toggle that class instead of setting `.hidden` — either approach is fine, pick one and be consistent.
2. After fixing, verify pointer-events are also correctly restored — i.e., once actually hidden, clicking into the terminal area must reach xterm.js and focus it (test this, don't just check visual disappearance).
3. Check whether any *other* UI states in `updateOverlay()`/`app.css` have the same class of bug (something that sets `hidden` via JS but has an explicit CSS `display` override that silently defeats it) — audit rather than just patching this one instance if the same pattern exists elsewhere.
4. I don't expect you to have a full browser-automation test suite for this, but if there's a cheap way to add even a minimal regression check (e.g., a simple Go test that renders/serves the CSS and asserts the `[hidden]` override rule is present in the served stylesheet, as a tripwire against this exact regression) that's worth doing since this exact bug class isn't caught by Go-level tests at all — say if that's not worth the effort and why, rather than skipping silently.

## Verification I want

After the fix, I'll redo my manual pass again: create a session, take control, click into the terminal, type a real command, and confirm output actually appears. Don't tell me this is done until you've at least reasoned through why the CSS specificity now actually works (ordering, selector weight) — I got burned by an unverified assumption once already in this exact file, so double check it rather than assuming the fix works.

Run `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` and paste real output as usual.
