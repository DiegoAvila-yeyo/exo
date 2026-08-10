package singleton

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLeaseAcquireReleaseReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backend.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer func() {
		_ = first.Release()
	}()

	second, err := Acquire(path)
	if !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second acquire error = %v, want %v", err, ErrLeaseHeld)
	}
	if second != nil {
		t.Fatal("second acquire unexpectedly succeeded")
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	third, err := Acquire(path)
	if err != nil {
		t.Fatalf("reacquire failed: %v", err)
	}
	defer func() {
		_ = third.Release()
	}()
}
