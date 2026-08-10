I reviewed M5's code directly and ran the test suite multiple times (Go was caching results the first time, which hid this). With `go test -race -count=1 -run TestRunReconcilesCrashedInstanceSessions -v ./backend/...`, the new end-to-end test fails **about 4 out of 5 runs** with:

```
backend_test.go:192: updated status = "stale_orphaned", want "stale_reaped"
```

Reproduce it yourself with that exact command (run it 5 times in a loop, not just once — it's intermittent, not deterministic, so a single passing run doesn't mean it's fixed).

## My hypothesis on the root cause

Look at `reconcileOne` in `sessionstore/reconcile.go`:

```go
func reconcileOne(record SessionMetadata) string {
	if !realpty.ProcessGroupAlive(record.ProcessGroupID) {
		return StatusStaleReaped
	}
	snapshot, err := realpty.ProcessSnapshot(record.ShellPID)
	if err != nil {
		return StatusStaleOrphaned
	}
	if snapshot.PGID != record.ProcessGroupID || snapshot.StartTime != record.ShellStartTime {
		return StatusStaleOrphaned
	}
	...
}
```

The identity check requires `ProcessSnapshot(record.ShellPID)` to succeed **and** exactly match the recorded `ProcessGroupID` and `ShellStartTime`. The test's crash-guard command (in `holdShellAliveAcrossCrash`) is:

```
echo CRASH_GUARD_READY; trap '' HUP; exec /bin/sh -c 'trap "" HUP; while :; do sleep 100; done'
```

That `exec /bin/sh -c '...'` **replaces the shell's process image in-place** (same PID, same PGID — `exec` doesn't fork), but it's plausible that on this macOS, `ps`'s `lstart`/start-time reporting for that PID changes after the `exec` (e.g., if the kernel or `ps` treats the exec as resetting some notion of "start" it reports), or there's some other timing/formatting mismatch between the `ShellStartTime` recorded at original spawn time (via `realpty.ProcessSnapshot` right after `pty.StartWithSize`) and what a *later* `ProcessSnapshot` call on the same PID reports after the `exec` and after time has passed. Investigate which one it actually is — don't guess, check directly (e.g., add temporary debug output comparing the recorded `ShellStartTime` against what `ProcessSnapshot` returns for that same PID moments before the assertion fails, or write a small standalone reproduction: spawn a real shell via `realpty.New`, capture its snapshot, have the shell `exec` into another process image, take another snapshot of the same PID, and diff the two).

## What I need

1. Find the actual, confirmed root cause (not a guess) — is it the `exec` changing what `ps lstart` reports, a race between when the crash-guard shell finishes its `exec` and when the test reads back `metadata.ShellStartTime` vs. re-snapshotting, formatting/timezone nondeterminism in how `lstart` gets parsed and compared as a string, or something else?
2. Fix it so the test passes reliably. Depending on root cause, the fix might be: comparing start time with some tolerance instead of exact string equality, using a different/more stable identity signal, capturing the snapshot differently, or something else — your call once you know the real cause.
3. Prove the fix by running `go test -race -count=1 -run TestRunReconcilesCrashedInstanceSessions -v ./backend/...` **at least 10 times in a loop** and pasting the real output showing it passes consistently, not just once. A single pass is not sufficient evidence given how flaky this currently is.
4. Also re-run the full suite (`go test -race -count=3 ./...`) since Go's test caching can hide exactly this kind of flakiness (it did for me on the first pass) — paste that output too, and make sure you're not relying on cached results (use `-count=1` or higher, never a bare `go test` with no count flag, when checking for flakiness).

This is a real correctness bug in the crash-recovery identity check, not a test-only artifact — if this fires in production the way it fired in the test, a genuinely-alive leftover shell from a crashed instance would get marked `stale_orphaned` and left running instead of being cleaned up, which defeats the actual purpose of M5.
