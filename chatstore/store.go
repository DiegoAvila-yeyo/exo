// Package chatstore persists chat sessions (title + full message transcript)
// as one JSON file per session, mirroring the sessionstore package's
// write-tmp-then-rename pattern. Sessions survive `exo restart` and process
// crashes because they live on disk, not in the agent's in-memory history.
package chatstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/yeyoos/nucleo-base/shared/api"
)

// ChatEntry is one bubble in the UI transcript — the same {text, kind} shape
// the frontend already keeps in chat-log's dataset.entries, so a reopened
// session renders identically to a live one.
type ChatEntry struct {
	Text string `json:"text"`
	Kind string `json:"kind,omitempty"`
}

// ChatSession is the full persisted record: metadata, the exact message
// history handed to agent.Agent.SetMessages when this session is active,
// and the display transcript shown in the UI.
type ChatSession struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []api.Message `json:"messages"`
	Entries   []ChatEntry   `json:"entries"`
	// ProjectPath is the folder the agent works in for this session's turns
	// — set by picking a project in the composer. Empty means "wherever
	// EXO_AGENT_ROOT_PATH already points", i.e. no per-session override.
	ProjectPath string `json:"project_path,omitempty"`
	// PlanningID/BoardID scope this session to a Planning Board — set only
	// while the chat is docked there. Unlike ProjectPath, these two are
	// atomic (both set or both empty, never one alone) and get explicitly
	// cleared (not just left stale) the moment the chat leaves a Board —
	// see termserver/chat.go's resolvePlanningContext.
	PlanningID string `json:"planning_id,omitempty"`
	BoardID    string `json:"board_id,omitempty"`

	// LastTurnTokens/ContextWindowTokens/ModelID are the Sesiones token
	// meter's raw inputs — set on every Save from the most recent turn's
	// TurnUsage (termserver/chat.go). LastTurnTokens is this turn's own
	// token delta, never a cumulative session total. ContextPct (shown to
	// the human) is computed from these rather than persisted separately.
	LastTurnTokens      int    `json:"last_turn_tokens,omitempty"`
	ContextWindowTokens int    `json:"context_window_tokens,omitempty"`
	ModelID             string `json:"model_id,omitempty"`

	// Status is "" (open) or StatusClosed. Closed is terminal: no
	// "reopen and continue" on the same ID — see chat.go's handleChat and
	// chatsessions.go's handleCloseChatSession.
	Status string `json:"status,omitempty"`
}

// StatusClosed is the only non-empty value ChatSession.Status holds.
const StatusClosed = "closed"

// ChatSessionSummary is the lightweight view used for sidebar listings —
// no transcript, so listing many sessions stays cheap. ProjectPath is
// included so the sidebar can filter "Recent" down to one project's
// sessions without a detail fetch per row.
type ChatSessionSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ProjectPath string    `json:"project_path,omitempty"`
	// Status lets the sidebar sort closed sessions below open ones and show
	// a "Closed" badge — see the 2026-08-21 UI round's buildSessionsListElement.
	Status string `json:"status,omitempty"`
}

const DefaultTitle = "New chat"

var ErrNotFound = errors.New("chatstore: session not found")

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("chatstore: directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Create writes a new, empty chat session and returns it.
func (s *Store) Create() (ChatSession, error) {
	id, err := newSessionID()
	if err != nil {
		return ChatSession{}, err
	}
	now := time.Now()
	session := ChatSession{
		ID:        id,
		Title:     DefaultTitle,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []api.Message{},
		Entries:   []ChatEntry{},
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(session); err != nil {
		return ChatSession{}, err
	}
	return session, nil
}

// Load returns the full session, including its message transcript.
func (s *Store) Load(id string) (ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(id)
}

// Save persists a session's title and messages, bumping UpdatedAt.
func (s *Store) Save(session ChatSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session.UpdatedAt = time.Now()
	return s.writeLocked(session)
}

// List returns all sessions as lightweight summaries, most recently
// updated first — the shape the sidebar's Recent list needs.
func (s *Store) List() ([]ChatSessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]ChatSessionSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		session, err := s.readLocked(id)
		if err != nil {
			continue // skip unreadable/partial files rather than failing the whole list
		}
		out = append(out, ChatSessionSummary{
			ID:          session.ID,
			Title:       session.Title,
			CreatedAt:   session.CreatedAt,
			UpdatedAt:   session.UpdatedAt,
			ProjectPath: session.ProjectPath,
			Status:      session.Status,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Store) writeLocked(session ChatSession) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(session.ID)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) readLocked(id string) (ChatSession, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return ChatSession{}, ErrNotFound
		}
		return ChatSession{}, err
	}
	var session ChatSession
	if err := json.Unmarshal(data, &session); err != nil {
		return ChatSession{}, err
	}
	return session, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func newSessionID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "chat-" + hex.EncodeToString(buf), nil
}
