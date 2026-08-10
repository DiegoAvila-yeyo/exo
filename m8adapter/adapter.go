package m8adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DiegoAvila-yeyo/exo/ptyactor"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/terminal"
	toolpkg "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
)

const (
	defaultReadBytes       = 4000
	maxReadBytes           = 12000
	defaultCollectorBuffer = 64 * 1024
)

var ErrSessionNotFound = errors.New("session not found")

type sessionManager interface {
	CreateWithOptions(opts sessions.CreateOptions) (sessions.SessionInfo, error)
	Get(id string) (*ptyactor.Session, sessions.SessionInfo, bool)
	Close(id string) error
}

type Option func(*Adapter)

type Adapter struct {
	manager    sessionManager
	bufferSize int

	mu       sync.RWMutex
	sessions map[string]*adapterSession
}

type adapterSession struct {
	id           string
	session      *ptyactor.Session
	agentLease   ptyactor.Lease
	kind         terminal.SessionKind
	approvalMode string
	collector    *collector

	mu     sync.RWMutex
	info   sessions.SessionInfo
	killed bool
}

type collector struct {
	bufferSize int

	mu             sync.Mutex
	baseCursor     int64
	data           []byte
	lastOutputTime time.Time
	updateCh       chan struct{}
}

func New(manager *sessions.Manager, opts ...Option) *Adapter {
	return NewWithManager(manager, opts...)
}

func NewWithManager(manager sessionManager, opts ...Option) *Adapter {
	adapter := &Adapter{
		manager:    manager,
		bufferSize: defaultCollectorBuffer,
		sessions:   make(map[string]*adapterSession),
	}
	for _, opt := range opts {
		opt(adapter)
	}
	if adapter.bufferSize <= 0 {
		adapter.bufferSize = defaultCollectorBuffer
	}
	return adapter
}

func WithBufferSize(size int) Option {
	return func(adapter *Adapter) {
		adapter.bufferSize = size
	}
}

var _ toolpkg.TerminalBackend = (*Adapter)(nil)

func (a *Adapter) Open(ctx context.Context, opts terminal.OpenOptions) (terminal.ToolEnvelope, error) {
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		return terminal.ToolEnvelope{OK: false, Error: "command is required"}, fmt.Errorf("command is required")
	}

	workdir := strings.TrimSpace(opts.Workdir)
	if workdir == "" {
		workdir = "."
	}

	info, err := a.manager.CreateWithOptions(sessions.CreateOptions{
		Workdir:      workdir,
		Name:         strings.TrimSpace(opts.Name),
		Command:      command,
		InitialOwner: "agent",
	})
	if err != nil {
		return terminal.ToolEnvelope{OK: false, Error: err.Error()}, err
	}

	session, currentInfo, ok := a.manager.Get(info.ID)
	if !ok {
		return terminal.ToolEnvelope{OK: false, Error: ErrSessionNotFound.Error()}, ErrSessionNotFound
	}

	state := &adapterSession{
		id:           info.ID,
		session:      session,
		agentLease:   session.Lease(),
		kind:         classifyCommand(command),
		approvalMode: approvalModeFor(command),
		collector:    newCollector(a.bufferSize),
		info:         currentInfo,
	}

	a.mu.Lock()
	a.sessions[info.ID] = state
	a.mu.Unlock()

	go state.runCollector()

	wait := opts.StartupWait
	if wait <= 0 {
		wait = 3 * time.Second
	}

	ctxForWait := ctx
	if ctxForWait == nil {
		ctxForWait = context.Background()
	}

	output, cursor, truncated := state.waitForRead(ctxForWait, 0, maxReadBytes, wait)
	meta := a.sessionMetaFromState(state)
	return envelopeFromMeta(meta, cursor, output, truncated), nil
}

func (a *Adapter) Read(sessionID string, opts terminal.ReadOptions) (terminal.ToolEnvelope, error) {
	state, err := a.lookup(sessionID)
	if err != nil {
		return notFoundEnvelope(sessionID), err
	}

	maxBytes := clampMaxBytes(opts.MaxBytes)
	var (
		output    string
		cursor    int64
		truncated bool
	)
	if opts.Wait > 0 {
		output, cursor, truncated = state.waitForRead(context.Background(), opts.Since, maxBytes, opts.Wait)
	} else {
		output, cursor, truncated = state.collector.snapshot(opts.Since, maxBytes)
	}

	meta := a.sessionMetaFromState(state)
	return envelopeFromMeta(meta, cursor, output, truncated), nil
}

func (a *Adapter) Write(sessionID, input string, appendNewline bool, wait time.Duration, maxBytes int) (terminal.ToolEnvelope, error) {
	state, err := a.lookup(sessionID)
	if err != nil {
		return notFoundEnvelope(sessionID), err
	}

	meta := a.sessionMetaFromState(state)
	if !isLiveStatus(meta.Status) {
		return alreadyExitedEnvelope(meta), fmt.Errorf("session is not running")
	}

	payload := input
	if appendNewline {
		payload += "\n"
	}
	if err := state.session.WriteWithLease(state.agentLease, []byte(payload)); err != nil {
		if errors.Is(err, ptyactor.ErrOwnershipLost) {
			return terminal.ToolEnvelope{
				OK:            false,
				SessionID:     sessionID,
				Status:        terminal.SessionStatusRunning,
				SessionKind:   state.kind,
				ApprovalMode:  state.approvalMode,
				AlreadyExited: false,
				Error:         "terminal ownership lost (session taken over by another client)",
			}, err
		}

		meta = a.sessionMetaFromState(state)
		if !isLiveStatus(meta.Status) {
			return alreadyExitedEnvelope(meta), fmt.Errorf("session is not running")
		}

		return terminal.ToolEnvelope{
			OK:           false,
			SessionID:    sessionID,
			Status:       meta.Status,
			SessionKind:  state.kind,
			ApprovalMode: state.approvalMode,
			Error:        err.Error(),
		}, err
	}

	if wait <= 0 {
		cursor := state.collector.currentCursor()
		return envelopeFromMeta(meta, cursor, "", false), nil
	}

	output, cursor, truncated := state.waitForRead(context.Background(), state.collector.currentCursor(), clampMaxBytes(maxBytes), wait)
	meta = a.sessionMetaFromState(state)
	return envelopeFromMeta(meta, cursor, output, truncated), nil
}

func (a *Adapter) Kill(sessionID, signal string) (terminal.ToolEnvelope, error) {
	state, err := a.lookup(sessionID)
	if err != nil {
		return notFoundEnvelope(sessionID), err
	}

	meta := a.sessionMetaFromState(state)
	if !isLiveStatus(meta.Status) {
		return terminal.ToolEnvelope{
			OK:            true,
			SessionID:     sessionID,
			Status:        meta.Status,
			SessionKind:   state.kind,
			ApprovalMode:  state.approvalMode,
			ExitCode:      meta.ExitCode,
			AlreadyExited: true,
		}, nil
	}

	state.markKilled()
	if err := a.manager.Close(sessionID); err != nil && !errors.Is(err, os.ErrNotExist) {
		return terminal.ToolEnvelope{OK: false, SessionID: sessionID, Error: err.Error()}, err
	}

	meta = a.sessionMetaFromState(state)
	return terminal.ToolEnvelope{
		OK:            true,
		SessionID:     sessionID,
		Name:          meta.Name,
		Status:        meta.Status,
		SessionKind:   state.kind,
		ApprovalMode:  state.approvalMode,
		Workdir:       meta.Workdir,
		Command:       meta.Command,
		ExitCode:      meta.ExitCode,
		AlreadyExited: true,
	}, nil
}

func (a *Adapter) List(includeExited bool) ([]terminal.SessionMeta, error) {
	a.mu.RLock()
	states := make([]*adapterSession, 0, len(a.sessions))
	for _, state := range a.sessions {
		states = append(states, state)
	}
	a.mu.RUnlock()

	out := make([]terminal.SessionMeta, 0, len(states))
	for _, state := range states {
		meta := a.sessionMetaFromState(state)
		if !includeExited && !isLiveStatus(meta.Status) {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out, nil
}

func (a *Adapter) SessionMeta(sessionID string) (terminal.SessionMeta, error) {
	state, err := a.lookup(sessionID)
	if err != nil {
		return terminal.SessionMeta{}, err
	}
	return a.sessionMetaFromState(state), nil
}

func (a *Adapter) lookup(sessionID string) (*adapterSession, error) {
	a.mu.RLock()
	state, ok := a.sessions[sessionID]
	a.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	return state, nil
}

func (a *Adapter) sessionMetaFromState(state *adapterSession) terminal.SessionMeta {
	_, info, ok := a.manager.Get(state.id)
	if ok {
		state.setInfo(info)
	}

	cached := state.snapshotInfo()
	status := mapStatus(cached.Status, state.isKilled())
	return terminal.SessionMeta{
		ID:           cached.ID,
		Name:         cached.Name,
		Command:      cached.Command,
		Workdir:      cached.Workdir,
		Kind:         state.kind,
		ApprovalMode: state.approvalMode,
		Status:       status,
		StartedAt:    cached.CreatedAt,
		LastOutputAt: state.collector.lastOutputAt(),
	}
}

func (s *adapterSession) setInfo(info sessions.SessionInfo) {
	s.mu.Lock()
	s.info = info
	s.mu.Unlock()
}

func (s *adapterSession) snapshotInfo() sessions.SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

func (s *adapterSession) markKilled() {
	s.mu.Lock()
	s.killed = true
	s.info.Status = "closed"
	s.info.LastActiveAt = time.Now()
	s.mu.Unlock()
}

func (s *adapterSession) isKilled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.killed
}

func (s *adapterSession) waitForRead(ctx context.Context, since int64, maxBytes int, wait time.Duration) (string, int64, bool) {
	deadline := time.Now().Add(wait)
	for {
		output, cursor, truncated := s.collector.snapshot(since, maxBytes)
		if output != "" || cursor > since || time.Now().After(deadline) || !isLiveStatus(mapStatus(s.snapshotInfo().Status, s.isKilled())) {
			return output, cursor, truncated
		}

		updateCh := s.collector.updateChannel()
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return s.collector.snapshot(since, maxBytes)
		case <-updateCh:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			return s.collector.snapshot(since, maxBytes)
		}
	}
}

func (s *adapterSession) runCollector() {
	subscribe := func() (<-chan []byte, func(), error) {
		return s.session.Subscribe()
	}

	ch, unsubscribe, err := subscribe()
	if err != nil {
		return
	}
	defer unsubscribe()

	for {
		chunk, ok := <-ch
		if !ok {
			lease := s.session.Lease()
			nextCh, nextUnsub, err := s.session.SubscribeWithLease(lease)
			if err != nil {
				return
			}
			unsubscribe()
			ch = nextCh
			unsubscribe = nextUnsub
			continue
		}
		s.collector.append(chunk)
	}
}

func newCollector(bufferSize int) *collector {
	return &collector{
		bufferSize: bufferSize,
		updateCh:   make(chan struct{}),
	}
}

func (c *collector) append(chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data := append([]byte(nil), chunk...)
	if len(data) >= c.bufferSize {
		c.baseCursor += int64(len(c.data)) + int64(len(data)-c.bufferSize)
		c.data = append([]byte(nil), data[len(data)-c.bufferSize:]...)
	} else {
		c.data = append(c.data, data...)
		if overflow := len(c.data) - c.bufferSize; overflow > 0 {
			c.data = append([]byte(nil), c.data[overflow:]...)
			c.baseCursor += int64(overflow)
		}
	}
	c.lastOutputTime = time.Now()
	close(c.updateCh)
	c.updateCh = make(chan struct{})
}

func (c *collector) snapshot(since int64, maxBytes int) (string, int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if maxBytes <= 0 {
		maxBytes = defaultReadBytes
	}
	if maxBytes > maxReadBytes {
		maxBytes = maxReadBytes
	}

	currentCursor := c.baseCursor + int64(len(c.data))
	if since < 0 {
		since = 0
	}
	truncated := false
	if since < c.baseCursor {
		since = c.baseCursor
		truncated = true
	}
	if since > currentCursor {
		since = currentCursor
	}

	offset := int(since - c.baseCursor)
	if offset < 0 {
		offset = 0
	}
	toRead := len(c.data) - offset
	if toRead <= 0 {
		return "", since, false
	}
	if toRead > maxBytes {
		toRead = maxBytes
		truncated = true
	}
	out := string(c.data[offset : offset+toRead])
	return out, since + int64(toRead), truncated
}

func (c *collector) currentCursor() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseCursor + int64(len(c.data))
}

func (c *collector) lastOutputAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastOutputTime
}

func (c *collector) updateChannel() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.updateCh
}

func notFoundEnvelope(sessionID string) terminal.ToolEnvelope {
	return terminal.ToolEnvelope{
		OK:        false,
		SessionID: sessionID,
		Error:     ErrSessionNotFound.Error(),
	}
}

func alreadyExitedEnvelope(meta terminal.SessionMeta) terminal.ToolEnvelope {
	return terminal.ToolEnvelope{
		OK:            false,
		SessionID:     meta.ID,
		Status:        meta.Status,
		SessionKind:   meta.Kind,
		ApprovalMode:  meta.ApprovalMode,
		ExitCode:      meta.ExitCode,
		AlreadyExited: true,
		Error:         "session is not running",
	}
}

func envelopeFromMeta(meta terminal.SessionMeta, cursor int64, output string, truncated bool) terminal.ToolEnvelope {
	return terminal.ToolEnvelope{
		OK:           true,
		SessionID:    meta.ID,
		Name:         meta.Name,
		Status:       meta.Status,
		SessionKind:  meta.Kind,
		ApprovalMode: meta.ApprovalMode,
		PID:          meta.PID,
		Workdir:      meta.Workdir,
		Command:      meta.Command,
		Cursor:       cursor,
		Output:       output,
		Truncated:    truncated,
		ExitCode:     meta.ExitCode,
	}
}

func clampMaxBytes(maxBytes int) int {
	if maxBytes <= 0 {
		return defaultReadBytes
	}
	if maxBytes > maxReadBytes {
		return maxReadBytes
	}
	return maxBytes
}

func mapStatus(status string, killed bool) terminal.SessionStatus {
	if killed {
		return terminal.SessionStatusKilled
	}
	switch status {
	case "running":
		return terminal.SessionStatusRunning
	case "exited":
		return terminal.SessionStatusExited
	case "closed":
		return terminal.SessionStatusKilled
	default:
		return terminal.SessionStatusKilled
	}
}

func isLiveStatus(status terminal.SessionStatus) bool {
	return status == terminal.SessionStatusRunning || status == terminal.SessionStatusStarting
}

// classifyCommand intentionally duplicates terminal.classifyCommand from
// github.com/yeyoos/nucleo-base/layer2-runtime-rails/terminal/manager.go
// to keep M8's nucleo-base change surface limited to the six-method interface swap.
func classifyCommand(command string) terminal.SessionKind {
	cmd := strings.ToLower(strings.TrimSpace(command))
	shellLikes := []string{
		"bash", "zsh", "sh", "fish", "python", "python3", "node", "irb", "psql", "mysql", "sqlite3", "rails console",
	}
	for _, item := range shellLikes {
		if cmd == item || strings.HasPrefix(cmd, item+" ") {
			return terminal.SessionKindShellLike
		}
	}
	if strings.HasPrefix(cmd, "python -i") || strings.HasPrefix(cmd, "python3 -i") || strings.HasPrefix(cmd, "node ") && !strings.Contains(cmd, ".js") {
		return terminal.SessionKindShellLike
	}
	if cmd == "rails c" || strings.HasPrefix(cmd, "rails c ") {
		return terminal.SessionKindShellLike
	}
	return terminal.SessionKindCommand
}

func approvalModeFor(command string) string {
	if classifyCommand(command) == terminal.SessionKindShellLike {
		return "prompt_on_write"
	}
	return "default"
}
