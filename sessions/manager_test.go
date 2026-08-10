package sessions

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/ptyactor"
)

func TestManagerCreateListCloseAndCap(t *testing.T) {
	manager := New(WithMaxSessions(1))
	workdir := t.TempDir()

	info, err := manager.Create(workdir, "")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(info.ID)
	})

	if info.Name != filepath.Base(workdir) {
		t.Fatalf("name = %q, want %q", info.Name, filepath.Base(workdir))
	}

	list := manager.List()
	if len(list) != 1 || list[0].ID != info.ID {
		t.Fatalf("list = %+v, want one session %q", list, info.ID)
	}

	if _, err := manager.Create(filepath.Join(workdir, "missing"), "bad"); err == nil {
		t.Fatal("expected create with missing workdir to fail")
	}
	if len(manager.List()) != 1 {
		t.Fatalf("missing workdir leaked session into list: %+v", manager.List())
	}

	if _, err := manager.Create(t.TempDir(), "overflow"); !errors.Is(err, ErrSessionCapReached) {
		t.Fatalf("cap error = %v, want %v", err, ErrSessionCapReached)
	}

	if err := manager.Close(info.ID); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if got := manager.List(); len(got) != 0 {
		t.Fatalf("list after close = %+v, want empty", got)
	}
	if _, _, ok := manager.Get(info.ID); ok {
		t.Fatalf("closed session %q still retrievable", info.ID)
	}
}

func TestManagerCloseMissingSessionFails(t *testing.T) {
	manager := New()
	if err := manager.Close("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("close missing error = %v, want %v", err, os.ErrNotExist)
	}
}

func TestCreateWithOptionsStartsCommandBackedSession(t *testing.T) {
	original := newTerminalProcess
	t.Cleanup(func() {
		newTerminalProcess = original
	})

	var got terminalCreateOptions
	newTerminalProcess = func(opts terminalCreateOptions) (terminalProcess, error) {
		got = opts
		return newStubTerminal("stub-shell"), nil
	}

	manager := New()
	workdir := t.TempDir()
	info, err := manager.CreateWithOptions(CreateOptions{
		Workdir: workdir,
		Name:    "runner",
		Command: "npm run dev",
	})
	if err != nil {
		t.Fatalf("CreateWithOptions failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(info.ID)
	})

	if got.command != "npm run dev" {
		t.Fatalf("constructor command = %q, want %q", got.command, "npm run dev")
	}
	if info.Command != "npm run dev" {
		t.Fatalf("SessionInfo.Command = %q, want %q", info.Command, "npm run dev")
	}
}

func TestCreateWrapperStillCreatesInteractiveShellWithHumanOwner(t *testing.T) {
	manager := New()
	workdir := t.TempDir()

	info, err := manager.Create(workdir, "shell")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(info.ID)
	})

	session, _, ok := manager.Get(info.ID)
	if !ok {
		t.Fatalf("session %q not found", info.ID)
	}
	if owner := session.Lease().Owner; owner != "human" {
		t.Fatalf("Lease().Owner = %q, want %q", owner, "human")
	}
	if info.Command == "" {
		t.Fatal("interactive shell session should record a command")
	}
}

func TestCreateWithOptionsPlumbsInitialOwner(t *testing.T) {
	manager := New()
	workdir := t.TempDir()

	agentInfo, err := manager.CreateWithOptions(CreateOptions{
		Workdir:      workdir,
		Name:         "agent-shell",
		InitialOwner: "agent",
	})
	if err != nil {
		t.Fatalf("CreateWithOptions with explicit owner failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(agentInfo.ID)
	})

	agentSession, _, ok := manager.Get(agentInfo.ID)
	if !ok {
		t.Fatalf("session %q not found", agentInfo.ID)
	}
	if owner := agentSession.Lease().Owner; owner != "agent" {
		t.Fatalf("explicit owner = %q, want %q", owner, "agent")
	}

	defaultInfo, err := manager.CreateWithOptions(CreateOptions{
		Workdir: t.TempDir(),
		Name:    "default-owner",
	})
	if err != nil {
		t.Fatalf("CreateWithOptions with default owner failed: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(defaultInfo.ID)
	})

	defaultSession, _, ok := manager.Get(defaultInfo.ID)
	if !ok {
		t.Fatalf("session %q not found", defaultInfo.ID)
	}

	expectedDefault := ptyactor.NewSession(newStubTerminal("default"))
	t.Cleanup(func() {
		_ = expectedDefault.Close()
	})
	if owner := defaultSession.Lease().Owner; owner != expectedDefault.Lease().Owner {
		t.Fatalf("default owner = %q, want %q", owner, expectedDefault.Lease().Owner)
	}
}

type stubTerminal struct {
	mu      sync.Mutex
	command string
	done    chan struct{}
	closed  bool
}

func newStubTerminal(command string) *stubTerminal {
	return &stubTerminal{
		command: command,
		done:    make(chan struct{}),
	}
}

func (s *stubTerminal) Write(p []byte) (int, error) { return len(p), nil }

func (s *stubTerminal) Read(p []byte) (int, error) { return 0, io.EOF }

func (s *stubTerminal) Resize(cols, rows int) error { return nil }

func (s *stubTerminal) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	return nil
}

func (s *stubTerminal) Command() string { return s.command }

func (s *stubTerminal) PID() int { return 1 }

func (s *stubTerminal) PGID() int { return 1 }

func (s *stubTerminal) StartTime() string { return "stub-start" }

func (s *stubTerminal) Done() <-chan struct{} { return s.done }
