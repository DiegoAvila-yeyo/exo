package sessionstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	StatusRunning       = "running"
	StatusClosed        = "closed"
	StatusExited        = "exited"
	StatusStaleReaped   = "stale_reaped"
	StatusStaleOrphaned = "stale_orphaned"
)

type SessionMetadata struct {
	SessionID         string    `json:"session_id"`
	BackendInstanceID string    `json:"backend_instance_id"`
	ShellPID          int       `json:"shell_pid"`
	ProcessGroupID    int       `json:"process_group_id"`
	ShellStartTime    string    `json:"shell_start_time"`
	StartedAt         time.Time `json:"started_at"`
	Status            string    `json:"status"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("sessionstore: directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Record(info SessionMetadata) error {
	if info.Status == "" {
		info.Status = StatusRunning
	}
	return s.write(info)
}

func (s *Store) MarkClosed(id string) error {
	return s.transition(id, StatusClosed)
}

func (s *Store) MarkExited(id string) error {
	return s.transition(id, StatusExited)
}

func (s *Store) MarkReconciled(id, status string) error {
	return s.transition(id, status)
}

func (s *Store) ListUnreconciled(currentInstanceID string) ([]SessionMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []SessionMetadata
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := s.readLocked(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if info.BackendInstanceID == currentInstanceID {
			continue
		}
		if info.Status == StatusRunning {
			out = append(out, info)
		}
	}
	return out, nil
}

func (s *Store) Load(id string) (SessionMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(s.path(id))
}

func (s *Store) transition(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path(id)
	info, err := s.readLocked(path)
	if err != nil {
		return err
	}
	if info.Status != StatusRunning {
		return nil
	}
	info.Status = status
	return s.writeLocked(path, info)
}

func (s *Store) write(info SessionMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(s.path(info.SessionID), info)
}

func (s *Store) writeLocked(path string, info SessionMetadata) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) readLocked(path string) (SessionMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionMetadata{}, err
	}
	var info SessionMetadata
	if err := json.Unmarshal(data, &info); err != nil {
		return SessionMetadata{}, err
	}
	return info, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
