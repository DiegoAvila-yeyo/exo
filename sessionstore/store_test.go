package sessionstore

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DiegoAvila-yeyo/exo/realpty"
)

func TestListUnreconciledHonorsClosedAndRunningStates(t *testing.T) {
	store := newTestStore(t)

	closed := SessionMetadata{
		SessionID:         "session-closed",
		BackendInstanceID: "instance-a",
		ShellPID:          101,
		ProcessGroupID:    101,
		ShellStartTime:    "Fri Jul 31 10:00:00 2026",
		StartedAt:         time.Now(),
		Status:            StatusRunning,
	}
	if err := store.Record(closed); err != nil {
		t.Fatalf("record closed failed: %v", err)
	}
	if err := store.MarkClosed(closed.SessionID); err != nil {
		t.Fatalf("mark closed failed: %v", err)
	}

	running := SessionMetadata{
		SessionID:         "session-running",
		BackendInstanceID: "instance-a",
		ShellPID:          202,
		ProcessGroupID:    202,
		ShellStartTime:    "Fri Jul 31 10:01:00 2026",
		StartedAt:         time.Now(),
		Status:            StatusRunning,
	}
	if err := store.Record(running); err != nil {
		t.Fatalf("record running failed: %v", err)
	}

	records, err := store.ListUnreconciled("instance-b")
	if err != nil {
		t.Fatalf("list unreconciled failed: %v", err)
	}
	if len(records) != 1 || records[0].SessionID != running.SessionID {
		t.Fatalf("records = %+v, want only running session", records)
	}
}

func TestReconcileKillsStaleProcessGroup(t *testing.T) {
	store := newTestStore(t)
	cmd := exec.Command("/bin/sh", "-lc", "sleep 100")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stale process failed: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	snapshot, err := realpty.ProcessSnapshot(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("process snapshot failed: %v", err)
	}

	record := SessionMetadata{
		SessionID:         "session-stale",
		BackendInstanceID: "instance-old",
		ShellPID:          snapshot.PID,
		ProcessGroupID:    snapshot.PGID,
		ShellStartTime:    snapshot.StartTime,
		StartedAt:         time.Now(),
		Status:            StatusRunning,
	}
	if err := store.Record(record); err != nil {
		t.Fatalf("record stale session failed: %v", err)
	}

	results, err := Reconcile(store, "instance-new")
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(results) != 1 || results[0].Status != StatusStaleReaped {
		t.Fatalf("results = %+v, want stale_reaped", results)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		return !realpty.ProcessGroupAlive(snapshot.PGID)
	})

	updated, err := store.Load(record.SessionID)
	if err != nil {
		t.Fatalf("load updated record failed: %v", err)
	}
	if updated.Status != StatusStaleReaped {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusStaleReaped)
	}
}

func TestReconcileMarksAlreadyDeadGroupReaped(t *testing.T) {
	store := newTestStore(t)
	record := SessionMetadata{
		SessionID:         "session-dead",
		BackendInstanceID: "instance-old",
		ShellPID:          999999,
		ProcessGroupID:    999999,
		ShellStartTime:    "Fri Jul 31 10:02:00 2026",
		StartedAt:         time.Now(),
		Status:            StatusRunning,
	}
	if err := store.Record(record); err != nil {
		t.Fatalf("record dead session failed: %v", err)
	}

	results, err := Reconcile(store, "instance-new")
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(results) != 1 || results[0].Status != StatusStaleReaped {
		t.Fatalf("results = %+v, want stale_reaped", results)
	}

	updated, err := store.Load(record.SessionID)
	if err != nil {
		t.Fatalf("load updated record failed: %v", err)
	}
	if updated.Status != StatusStaleReaped {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusStaleReaped)
	}
}

func TestReconcileReapsAliveGroupWhenRecordedLeaderIsGone(t *testing.T) {
	store := newTestStore(t)

	leader := exec.Command("/bin/sleep", "100")
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Start(); err != nil {
		t.Fatalf("start leader failed: %v", err)
	}
	t.Cleanup(func() {
		_ = leader.Process.Kill()
		_, _ = leader.Process.Wait()
	})

	leaderSnapshot, err := realpty.ProcessSnapshot(leader.Process.Pid)
	if err != nil {
		t.Fatalf("leader snapshot failed: %v", err)
	}

	child := exec.Command("/bin/sleep", "100")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: leaderSnapshot.PGID}
	if err := child.Start(); err != nil {
		t.Fatalf("start child failed: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	})

	waitForCondition(t, time.Second, func() bool {
		snapshot, err := realpty.ProcessSnapshot(child.Process.Pid)
		return err == nil && snapshot.PGID == leaderSnapshot.PGID
	})

	if err := leader.Process.Kill(); err != nil {
		t.Fatalf("kill leader failed: %v", err)
	}
	_, _ = leader.Process.Wait()

	waitForCondition(t, time.Second, func() bool {
		_, err := realpty.ProcessSnapshot(leader.Process.Pid)
		return err != nil
	})
	if !realpty.ProcessGroupAlive(leaderSnapshot.PGID) {
		t.Fatalf("expected process group %d to stay alive after leader death", leaderSnapshot.PGID)
	}

	record := SessionMetadata{
		SessionID:         "session-leader-gone",
		BackendInstanceID: "instance-old",
		ShellPID:          leaderSnapshot.PID,
		ProcessGroupID:    leaderSnapshot.PGID,
		ShellStartTime:    leaderSnapshot.StartTime,
		StartedAt:         time.Now(),
		Status:            StatusRunning,
	}
	if err := store.Record(record); err != nil {
		t.Fatalf("record stale session failed: %v", err)
	}

	results, err := Reconcile(store, "instance-new")
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(results) != 1 || results[0].Status != StatusStaleReaped {
		t.Fatalf("results = %+v, want stale_reaped", results)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return !realpty.ProcessGroupAlive(leaderSnapshot.PGID)
	})

	updated, err := store.Load(record.SessionID)
	if err != nil {
		t.Fatalf("load updated record failed: %v", err)
	}
	if updated.Status != StatusStaleReaped {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusStaleReaped)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return store
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
