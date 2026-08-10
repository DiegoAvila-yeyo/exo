package lifecycle

import (
	"errors"
	"sync"
	"time"
)

var ErrShuttingDown = errors.New("lifecycle: backend is shutting down")

type timer interface {
	Stop() bool
}

type timerFactory func(time.Duration, func()) timer

type Config struct {
	IdleTimeout    time.Duration
	GracePeriod    time.Duration
	OnShuttingDown func()
	OnShutdown     func()
	TimerFactory   timerFactory
}

type IdleMonitor struct {
	mu             sync.Mutex
	idleTimeout    time.Duration
	gracePeriod    time.Duration
	onShuttingDown func()
	onShutdown     func()
	timerFactory   timerFactory
	idleTimer      timer
	graceTimer     timer
	shuttingDown   bool
	closed         bool
}

func New(config Config) *IdleMonitor {
	factory := config.TimerFactory
	if factory == nil {
		factory = func(d time.Duration, f func()) timer {
			return time.AfterFunc(d, f)
		}
	}
	return &IdleMonitor{
		idleTimeout:    config.IdleTimeout,
		gracePeriod:    config.GracePeriod,
		onShuttingDown: config.OnShuttingDown,
		onShutdown:     config.OnShutdown,
		timerFactory:   factory,
	}
}

func (m *IdleMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.scheduleIdleLocked()
}

func (m *IdleMonitor) Touch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if m.shuttingDown {
		m.shuttingDown = false
		if m.graceTimer != nil {
			m.graceTimer.Stop()
			m.graceTimer = nil
		}
	}
	m.scheduleIdleLocked()
}

func (m *IdleMonitor) RejectNewSessions() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shuttingDown {
		return ErrShuttingDown
	}
	return nil
}

func (m *IdleMonitor) IsShuttingDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shuttingDown
}

func (m *IdleMonitor) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.idleTimer != nil {
		m.idleTimer.Stop()
	}
	if m.graceTimer != nil {
		m.graceTimer.Stop()
	}
}

func (m *IdleMonitor) scheduleIdleLocked() {
	if m.idleTimer != nil {
		m.idleTimer.Stop()
	}
	m.idleTimer = m.timerFactory(m.idleTimeout, m.beginShutdown)
}

func (m *IdleMonitor) beginShutdown() {
	var callback func()
	m.mu.Lock()
	if m.closed || m.shuttingDown {
		m.mu.Unlock()
		return
	}
	m.shuttingDown = true
	callback = m.onShuttingDown
	m.graceTimer = m.timerFactory(m.gracePeriod, m.finishShutdown)
	m.mu.Unlock()
	if callback != nil {
		callback()
	}
}

func (m *IdleMonitor) finishShutdown() {
	var callback func()
	m.mu.Lock()
	if m.closed || !m.shuttingDown {
		m.mu.Unlock()
		return
	}
	callback = m.onShutdown
	m.mu.Unlock()
	if callback != nil {
		callback()
	}
}
