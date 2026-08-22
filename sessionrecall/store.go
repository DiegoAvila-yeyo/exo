package sessionrecall

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

// ErrStaleVersion mirrors canvasstore.ErrStaleVersion exactly — Save's
// caller must Load again and retry its mutation on top of the fresh copy.
var ErrStaleVersion = errors.New("sessionrecall: stale version — reload and retry")

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("sessionrecall: directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Load returns the ProjectRecall for projectID, auto-creating an empty,
// unsaved ProjectRecall{Version:0} in memory when no file exists yet — same
// contract as canvasstore.Store.Load.
func (s *Store) Load(projectID string) (ProjectRecall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, err := s.readLocked(projectID)
	if err != nil {
		if os.IsNotExist(err) {
			now := time.Now()
			return ProjectRecall{
				ProjectID: projectID,
				Entries:   []SessionSummary{},
				Version:   0,
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		}
		return ProjectRecall{}, err
	}
	return pr, nil
}

// Save persists pr using compare-and-swap on Version, identical shape to
// canvasstore.Store.Save. Before writing, it upserts by SessionID against
// whatever's already in pr.Entries: a repeat close for a SessionID that
// already has a non-superseded entry is a no-op (the existing entry is left
// exactly as-is, not duplicated) — this is what makes the close sequence in
// termserver/chat.go idempotent under retry after a partial failure.
//
// Callers pass a ProjectRecall whose Entries already contains the entry
// they want persisted (typically: Load, append/replace one entry, Save) —
// this method does not merge new entries in, it only de-duplicates by
// SessionID within what's given, keeping the first occurrence.
func (s *Store) Save(pr ProjectRecall) (ProjectRecall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	onDisk, err := s.readLocked(pr.ProjectID)
	if err != nil && !os.IsNotExist(err) {
		return ProjectRecall{}, err
	}
	currentVersion := 0
	if err == nil {
		currentVersion = onDisk.Version
	}
	if pr.Version != currentVersion {
		return ProjectRecall{}, ErrStaleVersion
	}

	pr.Entries = dedupeBySessionID(pr.Entries)
	pr.Version = currentVersion + 1
	pr.UpdatedAt = time.Now()
	if pr.CreatedAt.IsZero() {
		pr.CreatedAt = pr.UpdatedAt
	}
	if err := s.writeLocked(pr); err != nil {
		return ProjectRecall{}, err
	}
	return pr, nil
}

// dedupeBySessionID keeps the last entry seen for each SessionID — the
// close handler upserts by replacing an existing entry in-place in the
// slice it builds, so "last wins" and "first wins" agree in the normal
// path; this only matters as a defensive backstop against a caller that
// appended instead of replacing.
func dedupeBySessionID(entries []SessionSummary) []SessionSummary {
	seen := make(map[string]int, len(entries))
	out := make([]SessionSummary, 0, len(entries))
	for _, e := range entries {
		if idx, ok := seen[e.SessionID]; ok {
			out[idx] = e
			continue
		}
		seen[e.SessionID] = len(out)
		out = append(out, e)
	}
	return out
}

func (s *Store) writeLocked(pr ProjectRecall) error {
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(pr.ProjectID)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) readLocked(projectID string) (ProjectRecall, error) {
	data, err := os.ReadFile(s.path(projectID))
	if err != nil {
		return ProjectRecall{}, err
	}
	var pr ProjectRecall
	if err := json.Unmarshal(data, &pr); err != nil {
		return ProjectRecall{}, err
	}
	return pr, nil
}

func (s *Store) path(projectID string) string {
	sum := sha256.Sum256([]byte(projectID))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}
