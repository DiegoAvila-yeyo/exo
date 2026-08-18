package planningstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// PlanningSummary is the lightweight view for a Planning list screen — no
// boards/knowledge payload, so listing many Plannings stays cheap.
type PlanningSummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	BoardCount int       `json:"board_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var ErrNotFound = errors.New("planningstore: planning not found")

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("planningstore: directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Create writes a new, empty Planning (no boards, no knowledge) and
// returns it.
func (s *Store) Create(name string) (Planning, error) {
	now := time.Now()
	p := Planning{
		ID:        newID("planning"),
		Name:      name,
		Boards:    []Board{},
		Knowledge: []Knowledge{},
		Projects:  []ProjectLink{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(p); err != nil {
		return Planning{}, err
	}
	return p, nil
}

// Load returns the full Planning, including boards and knowledge.
func (s *Store) Load(id string) (Planning, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(id)
}

// Save persists a Planning, bumping UpdatedAt.
func (s *Store) Save(p Planning) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.UpdatedAt = time.Now()
	return s.writeLocked(p)
}

// List returns all Plannings as lightweight summaries, most recently
// updated first.
func (s *Store) List() ([]PlanningSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]PlanningSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		p, err := s.readLocked(id)
		if err != nil {
			continue // skip unreadable/partial files rather than failing the whole list
		}
		out = append(out, PlanningSummary{
			ID:         p.ID,
			Name:       p.Name,
			BoardCount: len(p.Boards),
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Store) writeLocked(p Planning) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(p.ID)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) readLocked(id string) (Planning, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return Planning{}, ErrNotFound
		}
		return Planning{}, err
	}
	var p Planning
	if err := json.Unmarshal(data, &p); err != nil {
		return Planning{}, err
	}
	return p, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
