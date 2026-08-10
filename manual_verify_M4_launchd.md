You are verifying milestone M4 of a Go project called `exo`, located at `~/exo` on this Mac. This is a real, live manual verification on macOS — actually run the commands, don't simulate or guess outcomes. Report back exactly what happened at each step, including any command output, errors, or unexpected behavior — don't paper over surprises.

## Background

`exo` is a backend that's meant to run as a per-user `launchd` `LaunchAgent`, using on-demand socket activation: `launchd` pre-binds a TCP listener on `127.0.0.1:45873` and only starts the `exo` process when a real connection arrives, rather than keeping it resident all the time. The `exo` binary has these subcommands: `install`, `uninstall`, `status`, `restart`, `serve` (the last one is what `launchd` itself invokes — you shouldn't need to run `serve` directly).

Key facts:
- Plist label: `com.diegoavila.exo`
- Plist gets written to `~/Library/LaunchAgents/com.diegoavila.exo.plist`
- Listening port: `127.0.0.1:45873`
- Lock file: `~/Library/Application Support/exo/backend.lock`
- Idle timeout default: 5 minutes (after 5 minutes of no activity, the running backend should shut itself down, releasing the port back to `launchd`-only ownership)

## What to do

1. **Build the binary.** `cd ~/exo && go build -o exo .` (adjust if the module's main package lives somewhere other than the repo root — check `~/exo/main.go` first). Confirm the build succeeds.

2. **Install.** Run `./exo install`. Report the exact output. Then independently confirm `launchd` actually registered it: run `launchctl print gui/$(id -u)/com.diegoavila.exo` and check that it shows up, is not in an error state, and that the socket configuration appears under it. Also confirm the plist file exists at `~/Library/LaunchAgents/com.diegoavila.exo.plist` and paste its contents.

3. **Confirm it's NOT running yet.** Run `./exo status` — it should report installed=true, running=false (nobody has connected yet). Independently double check with `ps aux | grep -i exo` that no `exo serve` process is alive yet.

4. **Trigger activation with a real connection.** Run `curl -s -H "Origin: http://127.0.0.1:45873" http://127.0.0.1:45873/api/sessions` and confirm you get a JSON response (should be an empty array `[]` if no sessions exist yet). Immediately after, check `ps aux | grep -i exo` again and confirm an `exo serve` (or similar) process is now alive — this proves `launchd` actually activated the process on-demand rather than it already running beforehand. Also run `./exo status` again and confirm it now reports running=true, session count 0.

5. **Create a session and use it.** `curl -s -X POST -H "Origin: http://127.0.0.1:45873" -H "Content-Type: application/json" -d '{"workdir":"'"$HOME"'"}' "http://127.0.0.1:45873/api/sessions"` and confirm it returns session info. Note: this route is double-submit-cookie protected per the design, so this curl call may get rejected (403) — if it does, that's expected given curl doesn't carry a browser cookie/CSRF token; note that in your report and don't treat it as a bug, just confirm the daemon is alive and reachable (the GET in step 4 already proves that).

6. **Test idle shutdown for real.** This takes patience — the default idle timeout is 5 minutes. Wait (you can literally wait, or if you want to avoid a long real-time wait, note that as a limitation in your report rather than fabricating a result) and then check `ps aux | grep -i exo` again to confirm the `exo serve` process is gone after the idle period, while `./exo status` still reports installed=true (agent still registered with `launchd`, just not currently resident).

7. **Test restart behavior.** Run `./exo restart` — since no sessions should be active at this point (or say so if there are), confirm it does NOT prompt for confirmation and just restarts. Then create a session first (if you can get past CSRF for a real test, or note if you can't) and re-run `./exo restart` to confirm it DOES prompt with a warning about losing active sessions when sessions exist.

8. **Uninstall and confirm cleanup.** Run `./exo uninstall`. Confirm the plist file is gone, and `launchctl print gui/$(id -u)/com.diegoavila.exo` now reports it's not found/not loaded.

## What I want back

A clear pass/fail for each of the 8 steps above, with the actual command output pasted for anything surprising, any step that didn't behave as expected, and an overall verdict: does `launchd` socket activation genuinely work as designed (on-demand start, idle shutdown, clean install/uninstall), or did you find a real problem? If step 6 (the 5-minute wait) is impractical to actually sit through, say so explicitly rather than guessing what would happen — an honest "I didn't verify this part" is much more useful than a fabricated pass.
