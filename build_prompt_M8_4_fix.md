Small, targeted fix to piece 4 of M8 in `~/exo` (module `github.com/DiegoAvila-yeyo/exo`). Real
build task, run tests, `go test -race` clean.

## The bug

`agenthost.Host.Run` (in `~/exo/agenthost/host.go`) captures the agent's raw `os.Stdout` output
(via `redirectStdout` in `~/exo/agenthost/stdout.go`) and feeds it directly into
`server.ChatOutputWriter()`, which is `termserver`'s `chatBroadcaster` (`~/exo/termserver/chat.go`,
`chatBroadcaster.Write`). The agent's real output (`~/nucleo-base/layer2-runtime-rails/agent/
agent.go`) is colored with ANSI escape codes for terminal display (`assistantColor`,
`toolLogColor`, `resetColor` constants — read that file to see them). `chatBroadcaster.Write` does
**not** strip these before broadcasting to the browser SSE stream, so the chat UI will show raw
ANSI escape sequences as visible garbage text instead of clean output.

The original pattern this was supposed to follow, `~/nucleo-base/layer1-harness-shell/dashboard/
broadcaster.go`, already does this correctly — read it: it has an `ansiRe` regexp
(`\x1b\[[0-9;]*[a-zA-Z]`) and a `stripANSI` function, applied to every write before broadcasting
(`clean := stripANSI(string(p))`). `termserver/chat.go`'s `chatBroadcaster.Write` was supposed to
reuse this pattern (per the M8 design doc and piece 3's build prompt) but the ANSI-stripping part
was missed.

## The fix

In `~/exo/termserver/chat.go`, add the same regexp/strip function (reimplemented locally, matching
the existing project convention of not importing `nucleo-base/layer1-harness-shell/dashboard` into
`termserver`) and apply it in `chatBroadcaster.Write` before storing into `replay` and before
fanning out to subscribers — i.e., strip once, at the single point both live output and
reconnect-replay read from, matching the same "scrub once at the one point that matters" principle
already used for the terminal secrets-scrubber in `ptyactor` (M1/M7).

## Test to add

A test in `termserver` (extend `chat_test.go`) that writes a string containing real ANSI escape
sequences (e.g. `"\x1b[36mhello\x1b[0m world\n"`) to `chatBroadcaster` (or through the full
`ChatOutputWriter()` → SSE path if that's cleaner to assert against, matching the existing test
style in that file) and asserts the resulting `output` SSE event's `text` field is clean
(`"hello world\n"`), with no escape bytes present.

Run `go test -race -count=1 ./...` in `~/exo` and confirm nothing else regresses.

## What NOT to do

Do not touch `agenthost/`, `m8adapter`, `sessions`, `realpty`, `ptyactor`, or `nucleo-base` at all
— this is a one-file fix in `termserver/chat.go` plus its test.

## When done

Report the exact diff and full `go test -race -count=1` output.
