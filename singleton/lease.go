package singleton

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var ErrLeaseHeld = errors.New("singleton: lease already held")

type Lease struct {
	file *os.File
	path string
}

func Acquire(path string) (*Lease, error) {
	if path == "" {
		return nil, errors.New("singleton: lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrLeaseHeld
		}
		return nil, err
	}
	return &Lease{file: file, path: path}, nil
}

func (l *Lease) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}

func (l *Lease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}
