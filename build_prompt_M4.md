You are building milestone M4 of `exo` (module `github.com/DiegoAvila-yeyo/exo`, at `~/exo`). M1 (`ptyactor`), M2 (`termserver`), M3 (`realpty`, `sessions`) are done and reviewed. This milestone is the macOS daemon lifecycle: `launchd` socket activation, single-instance safety, and the CLI to install/manage it. This is a real build task — write actual Go code, write tests, run them, paste real output. Some of this milestone is genuine macOS-specific systems programming — investigate rigorously rather than approximating, the same way you correctly investigated the `Setpgid`/session-leader issue in M3 rather than just reverting to a weaker workaround.

## Design context (from `~/tesla/DASHBOARD_TERMINAL_DESIGN.md`, rounds 5-10, closed)

- **Shape A (round 8, reconfirmed with Apple documentation)**: no separate supervisor process. A per-user `launchd` `LaunchAgent` with `Sockets` configured does on-demand socket activation directly on the backend binary — `launchd` owns and pre-binds the listening socket, and only starts the backend process on the first real incoming connection. The backend receives the already-bound socket from `launchd` at startup (this is the part that needs careful, correct implementation — see below) and serves HTTP/WS on it immediately, applying its own auth/Origin checks (already built in `termserver`).
- **Single-instance lease (round 10)**: because session state lives in-memory in one process, exactly one backend instance must ever be running. Don't rely solely on `launchd`'s own job semantics — add an explicit lease: a lock file (e.g. `flock` on `~/Library/Application Support/exo/backend.lock`) that the active instance holds. If a second instance is ever launched and can't acquire the lease, it must not create any state or open any sessions — it exits immediately with a distinct, identifiable exit condition.
- **Idle-shutdown handshake (round 10)**: before the backend exits due to inactivity, it must (a) mark itself `shutting_down` and stop accepting new logical sessions, (b) briefly wait to confirm no new connection is already in flight, before actually releasing the lease and exiting — to shrink the race window where `launchd` might start a second instance for a connection that arrived right as the first was exiting.
- **`restart` is destructive and must warn (round 10)**: `nucleo-base restart` (or whatever you name the exo CLI — pick a binary/command name and be consistent, e.g. `exo`) must check current session count via the sessions API before restarting; if there are active sessions, it must warn explicitly that they'll be lost (no reattach in v1) and require confirmation, rather than restarting silently.
- CLI subcommands needed: `install`, `uninstall`, `status`, `restart`.

## What to build

### 1. Receiving the `launchd`-activated socket (investigate this carefully)

Research how a Go program on macOS actually receives a socket that `launchd` pre-bound via a `LaunchAgent`'s `Sockets` key. The canonical mechanism Apple documents is calling `launch_activate_socket()` (declared in `<launch.h>`), which is a C library function, not an environment-variable convention like systemd's `LISTEN_FDS` — meaning a pure-Go implementation likely needs either (a) cgo to call it directly, or (b) some existing well-maintained Go library that already wraps this for you (check if one exists — search for something like a Go `launchd` socket activation package before assuming you must write raw cgo yourself). Investigate and tell me plainly which path you took and why, the same way you explained the `Setpgid`/session-leader finding in M3 — if cgo turns out to be genuinely required, that's fine and expected, just be explicit about it (and note any build-tag/`CGO_ENABLED` implications for the rest of the build). Implement a small package (suggest `launchdsocket/`) exposing something like `func ActivateNamedSocket(name string) ([]net.Listener, error)` (the exact shape is your call — document it).

Since you can't literally test the `launchd`-activation path without `launchd` actually invoking the process (that requires real installation, which the sandboxed test environment can't fully exercise) — for the parts you *can* unit test without a live `launchd`, do so; for the parts that inherently require a real `launchd` to verify, say so explicitly in your report rather than claiming full test coverage you don't have. It's fine and expected for this specific integration point to be manually-verified-later rather than unit-testable in isolation.

### 2. Single-instance lease

A package (suggest `singleton/`) providing lease acquisition backed by an advisory file lock (`flock` via `golang.org/x/sys/unix` or similar) at a fixed path under `~/Library/Application Support/<app-name>/backend.lock`. A second attempt to acquire while the first holds it must fail cleanly and distinguishably (a typed error), not block indefinitely or silently succeed.

### 3. Idle-shutdown handshake

Building on the existing session/activity tracking in `termserver`/`sessions` (there's already a `LastActiveAt`/`Touch` concept from M3) — implement an idle monitor that, after N minutes of no activity across all sessions, initiates the shutdown sequence described above (mark `shutting_down`, stop accepting new session creation, brief grace window, then release lease + exit). Make the idle timeout and grace window configurable (with sane defaults, document what you picked).

### 4. `LaunchAgent` plist generation + CLI subcommands

A `main` package (or wherever your CLI entrypoint lives) with subcommands:
- `install`: writes a `LaunchAgent` plist to `~/Library/LaunchAgents/`, pointing at the current binary's stable path, configured with the `Sockets` key for on-demand activation, then runs `launchctl bootstrap`/`load` (whichever is the currently-correct non-deprecated invocation — check this, `launchctl load` is legacy on modern macOS in favor of `bootstrap`/`bootout`, use the correct modern one and say which).
- `uninstall`: reverses `install` (`bootout`/`unload`, remove the plist).
- `status`: reports whether the agent is installed, and if the backend is currently running, queries it (e.g. hit `/api/sessions` locally) to report session count.
- `restart`: checks current session count first (via the same local query as `status`); if `N > 0`, print a clear warning ("This will terminate N active terminal session(s). Session reattach after restart is not supported in v1. Continue? [y/N]") and require explicit confirmation before proceeding; if `N == 0`, restart without prompting. The actual restart mechanism is `bootout` + `bootstrap` (or whatever you determined is correct above).

## Tests to write

1. Single-instance lease: acquiring succeeds; a second acquisition attempt while the first is held fails with a distinguishable error; releasing and reacquiring afterward succeeds.
2. Idle-shutdown handshake: with a short configured idle timeout for testing, verify the sequence actually happens in order (shutting_down state set, new session creation rejected during the grace window, lease released only after the grace window) — you'll likely need to expose enough hooks/state to assert on this deterministically, similar to how M1's actor tests used injected hooks instead of `time.Sleep`-based guessing.
3. CLI `restart` confirmation logic: given a fake/injectable session-count source, verify it prompts and blocks on confirmation when count > 0, and proceeds immediately when count == 0 (you don't need to test the real `launchctl` invocation end-to-end in CI — that's a manual verification step, same caveat as the socket activation piece).
4. Plist generation: given a binary path and port/socket config, the generated plist XML is well-formed and contains the expected keys (`Sockets`, `ProgramArguments` or `Program`, label, etc.) — a structural/content test, not a live install test.

Run `go build ./...`, `go vet ./...`, and `go test -race ./...`. Paste real output.

## What I want back

Report covering: package/file layout, which mechanism you used for receiving the `launchd` socket and why (cgo vs. a library vs. something else — be explicit, like you were about `Setpgid`), the exact CLI command names and plist shape, confirmation of `go build`/`go vet`/`go test -race` passing with real output, and an explicit list of anything in this milestone that could only be partially verified without a real `launchd` install (don't hide this — call it out clearly, same as you'd want me to trust your `Setpgid` investigation because you were upfront about what you checked and how).
