package sessions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/DiegoAvila-yeyo/exo/ptyactor"
	"github.com/DiegoAvila-yeyo/exo/realpty"
	"github.com/DiegoAvila-yeyo/exo/sessionstore"
)

const defaultMaxSessions = 10

var ErrSessionCapReached = errors.New("sessions: session cap reached")

type SessionInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Workdir      string    `json:"workdir"`
	Command      string    `json:"command"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

type CreateOptions struct {
	Workdir      string
	Name         string
	Command      string
	InitialOwner string
}

type Option func(*Manager)

type Manager struct {
	mu                sync.RWMutex
	nextID            uint64
	maxSessions       int
	sessions          map[string]*managedSession
	store             *sessionstore.Store
	backendInstanceID string
}

type managedSession struct {
	session *ptyactor.Session
	info    SessionInfo
	pty     terminalProcess
}

type terminalProcess interface {
	ptyactor.PTY
	Command() string
	PID() int
	PGID() int
	StartTime() string
	Done() <-chan struct{}
}

type terminalCreateOptions struct {
	workdir           string
	sessionID         string
	backendInstanceID string
	command           string
}

var newTerminalProcess = func(opts terminalCreateOptions) (terminalProcess, error) {
	realptyOpts := []realpty.Option{
		realpty.WithEnv(map[string]string{
			"NUCLEO_SESSION_ID":          opts.sessionID,
			"NUCLEO_BACKEND_INSTANCE_ID": opts.backendInstanceID,
		}),
	}
	if opts.command != "" {
		realptyOpts = append(realptyOpts, realpty.WithCommand(opts.command))
	}
	return realpty.New(opts.workdir, realptyOpts...)
}

func New(opts ...Option) *Manager {
	manager := &Manager{
		maxSessions: defaultMaxSessions,
		sessions:    make(map[string]*managedSession),
	}
	for _, opt := range opts {
		opt(manager)
	}
	if manager.maxSessions <= 0 {
		manager.maxSessions = defaultMaxSessions
	}
	return manager
}

func WithMaxSessions(limit int) Option {
	return func(manager *Manager) {
		manager.maxSessions = limit
	}
}

func WithSessionStore(store *sessionstore.Store) Option {
	return func(manager *Manager) {
		manager.store = store
	}
}

func WithBackendInstanceID(instanceID string) Option {
	return func(manager *Manager) {
		manager.backendInstanceID = instanceID
	}
}

func (m *Manager) Create(workdir, name string) (SessionInfo, error) {
	return m.CreateWithOptions(CreateOptions{
		Workdir:      workdir,
		Name:         name,
		InitialOwner: "human",
	})
}

func (m *Manager) CreateWithOptions(opts CreateOptions) (SessionInfo, error) {
	cleanWorkdir, err := validateWorkdir(opts.Workdir)
	if err != nil {
		return SessionInfo{}, err
	}

	m.mu.Lock()
	if len(m.sessions) >= m.maxSessions {
		m.mu.Unlock()
		return SessionInfo{}, ErrSessionCapReached
	}
	m.nextID++
	id := fmt.Sprintf("session-%04d", m.nextID)
	m.mu.Unlock()

	pty, err := newTerminalProcess(terminalCreateOptions{
		workdir:           cleanWorkdir,
		sessionID:         id,
		backendInstanceID: m.backendInstanceID,
		command:           opts.Command,
	})
	if err != nil {
		return SessionInfo{}, err
	}

	now := time.Now()
	name := opts.Name
	if name == "" {
		name = filepath.Base(cleanWorkdir)
	}

	sessionOpts := make([]ptyactor.Option, 0, 1)
	if opts.InitialOwner != "" {
		sessionOpts = append(sessionOpts, ptyactor.WithInitialOwner(opts.InitialOwner))
	}
	session := ptyactor.NewSession(pty, sessionOpts...)

	command := pty.Command()
	if opts.Command != "" {
		command = opts.Command
	}
	info := SessionInfo{
		ID:           id,
		Name:         name,
		Workdir:      cleanWorkdir,
		Command:      command,
		Status:       "running",
		CreatedAt:    now,
		LastActiveAt: now,
	}

	managed := &managedSession{
		session: session,
		info:    info,
		pty:     pty,
	}

	m.mu.Lock()
	if len(m.sessions) >= m.maxSessions {
		m.mu.Unlock()
		_ = session.Close()
		return SessionInfo{}, ErrSessionCapReached
	}
	m.sessions[id] = managed
	m.mu.Unlock()

	if m.store != nil {
		err := m.store.Record(sessionstore.SessionMetadata{
			SessionID:         id,
			BackendInstanceID: m.backendInstanceID,
			ShellPID:          pty.PID(),
			ProcessGroupID:    pty.PGID(),
			ShellStartTime:    pty.StartTime(),
			StartedAt:         now,
			Status:            sessionstore.StatusRunning,
		})
		if err != nil {
			m.mu.Lock()
			delete(m.sessions, id)
			m.mu.Unlock()
			_ = session.Close()
			return SessionInfo{}, err
		}
	}

	go m.watchExit(id, pty)

	return info, nil
}

func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]SessionInfo, 0, len(m.sessions))
	for _, session := range m.sessions {
		out = append(out, session.info)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (m *Manager) Get(id string) (*ptyactor.Session, SessionInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	if !ok {
		return nil, SessionInfo{}, false
	}
	return session.session, session.info, true
}

func (m *Manager) Touch(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[id]
	if !ok {
		return false
	}
	session.info.LastActiveAt = time.Now()
	return true
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return os.ErrNotExist
	}
	delete(m.sessions, id)
	session.info.Status = "closed"
	session.info.LastActiveAt = time.Now()
	m.mu.Unlock()

	err := session.session.Close()
	if err == nil && m.store != nil {
		_ = m.store.MarkClosed(id)
	}
	return err
}

func (m *Manager) watchExit(id string, pty terminalProcess) {
	<-pty.Done()

	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[id]
	if !ok {
		return
	}
	if session.info.Status != "running" {
		return
	}
	session.info.Status = "exited"
	session.info.LastActiveAt = time.Now()
	if m.store != nil {
		_ = m.store.MarkExited(id)
	}
}

func validateWorkdir(workdir string) (string, error) {
	if workdir == "" {
		return "", errors.New("sessions: workdir is required")
	}
	clean := filepath.Clean(workdir)
	info, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("sessions: workdir is not a directory")
	}
	return clean, nil
}
