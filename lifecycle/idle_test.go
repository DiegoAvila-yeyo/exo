package lifecycle

import (
	"errors"
	"testing"
	"time"
)

type fakeTimer struct {
	active bool
	fn     func()
}

func (t *fakeTimer) Stop() bool {
	wasActive := t.active
	t.active = false
	return wasActive
}

func (t *fakeTimer) Fire() {
	if !t.active {
		return
	}
	t.active = false
	t.fn()
}

func TestIdleShutdownHandshakeOrder(t *testing.T) {
	var timers []*fakeTimer
	released := false
	monitor := New(Config{
		IdleTimeout: 10 * time.Second,
		GracePeriod: 3 * time.Second,
		TimerFactory: func(_ time.Duration, fn func()) timer {
			timer := &fakeTimer{active: true, fn: fn}
			timers = append(timers, timer)
			return timer
		},
		OnShutdown: func() {
			released = true
		},
	})

	monitor.Start()
	if len(timers) != 1 {
		t.Fatalf("timers = %d, want 1 idle timer", len(timers))
	}

	timers[0].Fire()
	if !monitor.IsShuttingDown() {
		t.Fatal("monitor should be shutting down after idle timeout")
	}
	if !errors.Is(monitor.RejectNewSessions(), ErrShuttingDown) {
		t.Fatalf("reject new sessions = %v, want %v", monitor.RejectNewSessions(), ErrShuttingDown)
	}
	if released {
		t.Fatal("lease released before grace window elapsed")
	}
	if len(timers) != 2 {
		t.Fatalf("timers = %d, want 2 after grace timer scheduled", len(timers))
	}

	timers[1].Fire()
	if !released {
		t.Fatal("lease was not released after grace window")
	}
}

func TestActivityDuringGraceCancelsShutdown(t *testing.T) {
	var timers []*fakeTimer
	shutdowns := 0
	monitor := New(Config{
		IdleTimeout: 10 * time.Second,
		GracePeriod: 3 * time.Second,
		TimerFactory: func(_ time.Duration, fn func()) timer {
			timer := &fakeTimer{active: true, fn: fn}
			timers = append(timers, timer)
			return timer
		},
		OnShutdown: func() {
			shutdowns++
		},
	})

	monitor.Start()
	timers[0].Fire()
	if !monitor.IsShuttingDown() {
		t.Fatal("monitor should be shutting down after idle timeout")
	}

	monitor.Touch()
	if monitor.IsShuttingDown() {
		t.Fatal("touch during grace should cancel shutdown")
	}

	timers[1].Fire()
	if shutdowns != 0 {
		t.Fatalf("shutdowns = %d, want 0 after canceled grace timer", shutdowns)
	}
}
