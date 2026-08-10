package m8adapter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DiegoAvila-yeyo/exo/ptyactor"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/terminal"
)

func TestOpenCapturesLeaseAndClassifierMetadata(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	shellEnv, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "bash",
		Workdir:     t.TempDir(),
		Name:        "shell",
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open shell failed: %v", err)
	}
	shellState := adapter.sessions[shellEnv.SessionID]
	if shellState == nil {
		t.Fatalf("adapter session %q not found", shellEnv.SessionID)
	}
	if shellState.agentLease.Owner != "agent" {
		t.Fatalf("agent lease owner = %q, want %q", shellState.agentLease.Owner, "agent")
	}
	if shellState.kind != terminal.SessionKindShellLike {
		t.Fatalf("kind = %q, want %q", shellState.kind, terminal.SessionKindShellLike)
	}
	if shellState.approvalMode != "prompt_on_write" {
		t.Fatalf("approval mode = %q, want %q", shellState.approvalMode, "prompt_on_write")
	}

	cmdEnv, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "echo hello",
		Workdir:     t.TempDir(),
		Name:        "cmd",
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open command failed: %v", err)
	}
	cmdState := adapter.sessions[cmdEnv.SessionID]
	if cmdState.kind != terminal.SessionKindCommand {
		t.Fatalf("kind = %q, want %q", cmdState.kind, terminal.SessionKindCommand)
	}
	if cmdState.approvalMode != "default" {
		t.Fatalf("approval mode = %q, want %q", cmdState.approvalMode, "default")
	}
}

func TestWriteReturnsOwnershipLostAfterHumanTakeover(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "bash",
		Workdir:     t.TempDir(),
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	state := manager.mustSession(env.SessionID)
	if _, err := state.session.Takeover("human"); err != nil {
		t.Fatalf("Takeover failed: %v", err)
	}

	writeEnv, err := adapter.Write(env.SessionID, "echo nope", true, 0, 0)
	if !errors.Is(err, ptyactor.ErrOwnershipLost) {
		t.Fatalf("Write error = %v, want %v", err, ptyactor.ErrOwnershipLost)
	}
	if writeEnv.Error != "terminal ownership lost (session taken over by another client)" {
		t.Fatalf("error string = %q", writeEnv.Error)
	}
	if writeEnv.Status != terminal.SessionStatusRunning {
		t.Fatalf("status = %q, want %q", writeEnv.Status, terminal.SessionStatusRunning)
	}
	if writeEnv.AlreadyExited {
		t.Fatal("ownership-lost envelope should not mark already_exited")
	}
}

func TestReadTimeoutReturnsSuccessWithEmptyOutput(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "echo idle",
		Workdir:     t.TempDir(),
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	readEnv, err := adapter.Read(env.SessionID, terminal.ReadOptions{
		Since: 0,
		Wait:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !readEnv.OK {
		t.Fatalf("Read returned non-ok envelope: %+v", readEnv)
	}
	if readEnv.Output != "" {
		t.Fatalf("output = %q, want empty", readEnv.Output)
	}
	if readEnv.Truncated {
		t.Fatal("timeout read should not be truncated")
	}
}

func TestCollectorResubscribesAfterTakeoverAndCursorContinues(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "bash",
		Workdir:     t.TempDir(),
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	state := manager.mustSession(env.SessionID)
	if _, err := state.session.Takeover("human"); err != nil {
		t.Fatalf("Takeover failed: %v", err)
	}
	state.pty.EmitOutput([]byte("after-takeover"))

	readEnv, err := adapter.Read(env.SessionID, terminal.ReadOptions{
		Since: 0,
		Wait:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if readEnv.Output != "after-takeover" {
		t.Fatalf("output = %q, want %q", readEnv.Output, "after-takeover")
	}
	if readEnv.Cursor == 0 {
		t.Fatal("cursor should advance after resumed collection")
	}
}

func TestWriteAndKillAgainstExitedSessionPreserveAlreadyExitedSemantics(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "echo exit",
		Workdir:     t.TempDir(),
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	state := manager.mustSession(env.SessionID)
	if err := state.pty.Close(); err != nil {
		t.Fatalf("Close fake PTY failed: %v", err)
	}

	writeEnv, err := adapter.Write(env.SessionID, "echo hi", true, 0, 0)
	if err == nil {
		t.Fatal("expected write to exited session to fail")
	}
	if !writeEnv.AlreadyExited {
		t.Fatalf("write envelope = %+v, want already_exited", writeEnv)
	}
	if writeEnv.Error != "session is not running" {
		t.Fatalf("write error = %q", writeEnv.Error)
	}

	killEnv, err := adapter.Kill(env.SessionID, "")
	if err != nil {
		t.Fatalf("Kill should succeed idempotently: %v", err)
	}
	if !killEnv.AlreadyExited {
		t.Fatalf("kill envelope = %+v, want already_exited", killEnv)
	}
	if killEnv.Status != terminal.SessionStatusExited {
		t.Fatalf("kill status = %q, want %q", killEnv.Status, terminal.SessionStatusExited)
	}
}

func TestListOnlyExposesAdapterOwnedSessions(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	manager.createExternal("external", sessions.CreateOptions{
		Workdir: t.TempDir(),
		Name:    "external",
		Command: "echo outside",
	})

	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "echo inside",
		Workdir:     t.TempDir(),
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	listed, err := adapter.List(true)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != env.SessionID {
		t.Fatalf("listed sessions = %+v, want only adapter-owned session %q", listed, env.SessionID)
	}
}

func TestSessionMetaFieldMapping(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager)

	workdir := t.TempDir()
	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "echo field-map",
		Workdir:     workdir,
		Name:        "mapper",
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	state := manager.mustSession(env.SessionID)
	state.pty.EmitOutput([]byte("chunk"))

	readEnv, err := adapter.Read(env.SessionID, terminal.ReadOptions{Since: 0, Wait: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if readEnv.Output != "chunk" {
		t.Fatalf("output = %q, want %q", readEnv.Output, "chunk")
	}

	meta, err := adapter.SessionMeta(env.SessionID)
	if err != nil {
		t.Fatalf("SessionMeta failed: %v", err)
	}
	if meta.ID != env.SessionID || meta.Name != "mapper" || meta.Command != "echo field-map" || meta.Workdir != workdir {
		t.Fatalf("unexpected direct mapping: %+v", meta)
	}
	if meta.Kind != terminal.SessionKindCommand || meta.ApprovalMode != "default" {
		t.Fatalf("unexpected classification mapping: %+v", meta)
	}
	if meta.Status != terminal.SessionStatusRunning {
		t.Fatalf("status = %q, want %q", meta.Status, terminal.SessionStatusRunning)
	}
	if meta.StartedAt.IsZero() {
		t.Fatal("StartedAt should be populated")
	}
	if meta.LastOutputAt.IsZero() {
		t.Fatal("LastOutputAt should be populated after output")
	}
	if meta.RunID != "" || meta.OwnerPID != 0 || meta.PID != 0 || meta.ExitedAt != nil || meta.ExitCode != nil {
		t.Fatalf("deferred fields should be zero-valued: %+v", meta)
	}
}

func TestReadCursorTruncationOnBufferEviction(t *testing.T) {
	manager := newFakeManager()
	adapter := NewWithManager(manager, WithBufferSize(8))

	env, err := adapter.Open(context.Background(), terminal.OpenOptions{
		Command:     "echo evict",
		Workdir:     t.TempDir(),
		StartupWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	state := manager.mustSession(env.SessionID)
	state.pty.EmitOutput([]byte("abcdefghij"))

	readEnv, err := adapter.Read(env.SessionID, terminal.ReadOptions{
		Since:    0,
		MaxBytes: 32,
		Wait:     200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !readEnv.Truncated {
		t.Fatalf("expected truncation, got %+v", readEnv)
	}
	if readEnv.Output != "cdefghij" {
		t.Fatalf("output = %q, want %q", readEnv.Output, "cdefghij")
	}
	if readEnv.Cursor != 10 {
		t.Fatalf("cursor = %d, want %d", readEnv.Cursor, 10)
	}
}

type fakeManager struct {
	mu       sync.RWMutex
	nextID   int
	sessions map[string]*fakeManagedSession
}

type fakeManagedSession struct {
	session *ptyactor.Session
	pty     *fakePTY
	info    sessions.SessionInfo
	removed bool
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		sessions: make(map[string]*fakeManagedSession),
	}
}

func (m *fakeManager) CreateWithOptions(opts sessions.CreateOptions) (sessions.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := "session-" + time.Now().Format("150405") + "-" + string(rune('a'+m.nextID))
	pty := newFakePTY()
	sessionOpts := make([]ptyactor.Option, 0, 1)
	if opts.InitialOwner != "" {
		sessionOpts = append(sessionOpts, ptyactor.WithInitialOwner(opts.InitialOwner))
	}
	session := ptyactor.NewSession(pty, sessionOpts...)
	now := time.Now()
	command := opts.Command
	if command == "" {
		command = "fake-shell"
	}
	info := sessions.SessionInfo{
		ID:           id,
		Name:         opts.Name,
		Workdir:      opts.Workdir,
		Command:      command,
		Status:       "running",
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if info.Name == "" {
		info.Name = "session"
	}
	m.sessions[id] = &fakeManagedSession{session: session, pty: pty, info: info}
	return info, nil
}

func (m *fakeManager) Get(id string) (*ptyactor.Session, sessions.SessionInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.sessions[id]
	if !ok || state.removed {
		return nil, sessions.SessionInfo{}, false
	}
	info := state.info
	if state.pty.isClosed() {
		info.Status = "exited"
	}
	return state.session, info, true
}

func (m *fakeManager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.sessions[id]
	if !ok || state.removed {
		return os.ErrNotExist
	}
	state.removed = true
	state.info.Status = "closed"
	state.info.LastActiveAt = time.Now()
	return state.session.Close()
}

func (m *fakeManager) mustSession(id string) *fakeManagedSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := m.sessions[id]
	if state == nil {
		panic("missing fake session " + id)
	}
	return state
}

func (m *fakeManager) createExternal(id string, opts sessions.CreateOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pty := newFakePTY()
	session := ptyactor.NewSession(pty, ptyactor.WithInitialOwner("human"))
	now := time.Now()
	m.sessions[id] = &fakeManagedSession{
		session: session,
		pty:     pty,
		info: sessions.SessionInfo{
			ID:           id,
			Name:         opts.Name,
			Workdir:      opts.Workdir,
			Command:      opts.Command,
			Status:       "running",
			CreatedAt:    now,
			LastActiveAt: now,
		},
	}
}

type fakePTY struct {
	mu      sync.Mutex
	writes  bytes.Buffer
	readCh  chan []byte
	closeCh chan struct{}
	closed  bool
}

func newFakePTY() *fakePTY {
	return &fakePTY{
		readCh:  make(chan []byte, 16),
		closeCh: make(chan struct{}),
	}
}

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, io.EOF
	}
	return f.writes.Write(p)
}

func (f *fakePTY) Read(p []byte) (int, error) {
	select {
	case <-f.closeCh:
		return 0, io.EOF
	case chunk := <-f.readCh:
		n := copy(p, chunk)
		return n, nil
	}
}

func (f *fakePTY) Resize(cols, rows int) error { return nil }

func (f *fakePTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.closeCh)
	return nil
}

func (f *fakePTY) EmitOutput(data []byte) {
	f.readCh <- append([]byte(nil), data...)
}

func (f *fakePTY) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
