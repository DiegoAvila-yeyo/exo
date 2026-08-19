package canvasstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrStaleVersion is returned by Save when the caller's Version doesn't
// match what's currently on disk — the caller must Load again (to see the
// intervening write) and retry its mutation on top of the fresh copy. This
// is the "optimistic concurrency at the aggregate level" the Canvas needs:
// canvasstore has no distributed locks, no CRDT, no per-shape merge — one
// write wins, the other bounces and reapplies.
var ErrStaleVersion = errors.New("canvasstore: stale version — reload and retry")

type Store struct {
	dir string
	mu  sync.Mutex // guards file I/O; CAS conflict detection is the Version compare in Save, not this mutex
}

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("canvasstore: directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Load returns the ProjectCanvas for projectID. Unlike planningstore
// (which requires an explicit Create), a project has no separate creation
// step anywhere else in this app — "project" is just a filesystem path —
// so Load auto-creates an empty, unsaved ProjectCanvas{Version:0} in
// memory when no file exists yet. Nothing is written to disk until the
// caller Saves it.
func (s *Store) Load(projectID string) (ProjectCanvas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pc, err := s.readLocked(projectID)
	if err != nil {
		if os.IsNotExist(err) {
			now := time.Now()
			return ProjectCanvas{
				ProjectID: projectID,
				Objects:   []CanvasObject{},
				Atoms:     []CanvasAtom{},
				Version:   0,
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		}
		return ProjectCanvas{}, err
	}
	return pc, nil
}

// Save persists pc using compare-and-swap on Version: pc.Version must equal
// what's currently on disk (0 if nothing has been saved yet), or Save
// returns ErrStaleVersion without writing. On success the returned
// ProjectCanvas has Version bumped by one and UpdatedAt refreshed — callers
// should keep using that returned copy for any further mutation in the same
// turn/request, not the one they passed in.
func (s *Store) Save(pc ProjectCanvas) (ProjectCanvas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	onDisk, err := s.readLocked(pc.ProjectID)
	if err != nil && !os.IsNotExist(err) {
		return ProjectCanvas{}, err
	}
	currentVersion := 0
	if err == nil {
		currentVersion = onDisk.Version
	}
	if pc.Version != currentVersion {
		return ProjectCanvas{}, ErrStaleVersion
	}

	pc.Version = currentVersion + 1
	pc.UpdatedAt = time.Now()
	if pc.CreatedAt.IsZero() {
		pc.CreatedAt = pc.UpdatedAt
	}
	if err := s.writeLocked(pc); err != nil {
		return ProjectCanvas{}, err
	}
	return pc, nil
}

func (s *Store) writeLocked(pc ProjectCanvas) error {
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(pc.ProjectID)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) readLocked(projectID string) (ProjectCanvas, error) {
	data, err := os.ReadFile(s.path(projectID))
	if err != nil {
		return ProjectCanvas{}, err
	}
	var pc ProjectCanvas
	if err := json.Unmarshal(data, &pc); err != nil {
		return ProjectCanvas{}, err
	}
	return pc, nil
}

// path derives the on-disk filename for projectID. ProjectID is an
// absolute filesystem path (see package doc), so unlike planningstore's
// generated IDs it cannot be used as a filename directly — it's hashed to
// a fixed-length hex name instead.
func (s *Store) path(projectID string) string {
	sum := sha256.Sum256([]byte(projectID))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}
