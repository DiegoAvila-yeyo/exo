package ptyactor

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
)

func TestWriteWithCurrentEpochSucceeds(t *testing.T) {
	pty := newFakePTY()
	session := NewSession(pty)
	t.Cleanup(func() {
		_ = session.Close()
	})

	if err := session.WriteWithLease(session.Lease(), []byte("echo hi")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if got := pty.Written(); string(got) != "echo hi" {
		t.Fatalf("written bytes = %q, want %q", got, "echo hi")
	}
}

func TestWriteWithStaleEpochFailsAfterTakeover(t *testing.T) {
	pty := newFakePTY()
	session := NewSession(pty)
	t.Cleanup(func() {
		_ = session.Close()
	})

	stale := session.Lease()
	if _, err := session.Takeover("human"); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}

	err := session.WriteWithLease(stale, []byte("blocked"))
	if !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("write error = %v, want %v", err, ErrOwnershipLost)
	}

	if got := pty.Written(); len(got) != 0 {
		t.Fatalf("stale write reached PTY: %q", got)
	}
}

func TestSubscriptionClosesOnEpochBump(t *testing.T) {
	pty := newFakePTY()
	session := NewSession(pty)
	t.Cleanup(func() {
		_ = session.Close()
	})

	ch, unsubscribe, err := session.Subscribe()
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer unsubscribe()

	if _, err := session.Takeover("human"); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}

	if _, ok := <-ch; ok {
		t.Fatal("subscription remained open after takeover")
	}
}

func TestInflightWriteReturnsOwnershipLostAfterTakeover(t *testing.T) {
	pty := newFakePTY()
	captured := make(chan struct{})
	release := make(chan struct{})
	session := NewSession(pty, withTestHooks(testHooks{
		afterWriteLeaseCaptured: func() {
			close(captured)
			<-release
		},
	}))
	t.Cleanup(func() {
		_ = session.Close()
	})

	result := make(chan error, 1)
	go func() {
		result <- session.Write([]byte("blocked"))
	}()

	<-captured
	if _, err := session.Takeover("human"); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}
	close(release)

	err := <-result
	if !errors.Is(err, ErrOwnershipLost) {
		t.Fatalf("write error = %v, want %v", err, ErrOwnershipLost)
	}
	if got := pty.Written(); len(got) != 0 {
		t.Fatalf("in-flight stale write reached PTY: %q", got)
	}
}

func TestSlowSubscriberGetsDroppedWithoutBlockingActor(t *testing.T) {
	pty := newFakePTY()
	handled := make(chan []byte, 2)
	session := NewSession(pty,
		WithSubscriberBufferSize(1),
		withTestHooks(testHooks{
			afterOutputHandled: func(data []byte) {
				handled <- data
			},
		}),
	)
	t.Cleanup(func() {
		_ = session.Close()
	})

	slow, slowUnsub, err := session.Subscribe()
	if err != nil {
		t.Fatalf("slow subscribe failed: %v", err)
	}
	defer slowUnsub()

	fast, fastUnsub, err := session.Subscribe()
	if err != nil {
		t.Fatalf("fast subscribe failed: %v", err)
	}
	defer fastUnsub()

	pty.EmitOutput([]byte("one"))
	<-handled
	if got := <-fast; string(got) != "one" {
		t.Fatalf("fast subscriber first chunk = %q, want %q", got, "one")
	}

	pty.EmitOutput([]byte("two"))
	<-handled
	if got := <-fast; string(got) != "two" {
		t.Fatalf("fast subscriber second chunk = %q, want %q", got, "two")
	}

	if err := session.Write([]byte("still-running")); err != nil {
		t.Fatalf("write failed while slow subscriber stalled: %v", err)
	}
	if got := pty.Written(); string(got) != "still-running" {
		t.Fatalf("write payload = %q, want %q", got, "still-running")
	}

	if got := <-slow; string(got) != "one" {
		t.Fatalf("slow subscriber buffered chunk = %q, want %q", got, "one")
	}
	if _, ok := <-slow; ok {
		t.Fatal("slow subscriber was not disconnected")
	}
}

func TestRingBufferReplayKeepsMostRecentBytes(t *testing.T) {
	pty := newFakePTY()
	handled := make(chan []byte, 3)
	session := NewSession(pty,
		WithRingBufferSize(5),
		withTestHooks(testHooks{
			afterOutputHandled: func(data []byte) {
				handled <- data
			},
		}),
	)
	t.Cleanup(func() {
		_ = session.Close()
	})

	pty.EmitOutput([]byte("abc"))
	<-handled
	pty.EmitOutput([]byte("def"))
	<-handled
	pty.EmitOutput([]byte("ghi"))
	<-handled

	ch, unsubscribe, err := session.Subscribe()
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer unsubscribe()

	if got := <-ch; string(got) != "efghi" {
		t.Fatalf("replay chunk = %q, want %q", got, "efghi")
	}
}

func TestScrubRunsBeforeReplayAndFanout(t *testing.T) {
	pty := newFakePTY()
	handled := make(chan []byte, 1)
	session := NewSession(pty, withTestHooks(testHooks{
		afterOutputHandled: func(data []byte) {
			handled <- data
		},
	}))
	t.Cleanup(func() {
		_ = session.Close()
	})

	ch, unsubscribe, err := session.Subscribe()
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer unsubscribe()

	pty.EmitOutput([]byte("token=supersecret"))
	<-handled

	if got := <-ch; string(got) != "token=[REDACTED]" {
		t.Fatalf("fanout chunk = %q, want %q", got, "token=[REDACTED]")
	}
}

func TestScrubRedactsCommonSecretPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "env token assignment",
			input: "API_TOKEN=supersecretvalue",
			want:  "API_TOKEN=[REDACTED]",
		},
		{
			name:  "password assignment with colon",
			input: "password: hunter2",
			want:  "password: [REDACTED]",
		},
		{
			name:  "authorization bearer header",
			input: "Authorization: Bearer abcdefghijklmnopqrstuv123456",
			want:  "Authorization: Bearer [REDACTED]",
		},
		{
			name:  "aws access key",
			input: "Using key AKIAIOSFODNN7EXAMPLE for deploy",
			want:  "Using key [REDACTED AWS ACCESS KEY] for deploy",
		},
		{
			name:  "jwt token",
			input: "token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlX2Jsb2Nr",
			want:  "token [REDACTED JWT]",
		},
		{
			name:  "private key header only",
			input: "-----BEGIN OPENSSH PRIVATE KEY-----",
			want:  "-----BEGIN [REDACTED PRIVATE KEY]-----",
		},
		{
			name:  "private key block",
			input: "-----BEGIN RSA PRIVATE KEY-----\nabc123\n-----END RSA PRIVATE KEY-----",
			want:  "[REDACTED PRIVATE KEY]",
		},
		{
			name:  "ordinary output preserved",
			input: "build finished successfully in 2.3s",
			want:  "build finished successfully in 2.3s",
		},
		{
			name:  "token word without secret value preserved",
			input: "token bucket rate limiter enabled",
			want:  "token bucket rate limiter enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(scrub([]byte(tc.input)))
			if got != tc.want {
				t.Fatalf("scrub(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

type fakePTY struct {
	mu      sync.Mutex
	writes  bytes.Buffer
	readCh  chan []byte
	closeCh chan struct{}
	closed  bool
}

func newFakePTY() *fakePTY {
	return &fakePTY{
		readCh:  make(chan []byte),
		closeCh: make(chan struct{}),
	}
}

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errSessionClosed
	}
	return f.writes.Write(p)
}

func (f *fakePTY) Read(p []byte) (int, error) {
	select {
	case <-f.closeCh:
		return 0, io.EOF
	case chunk := <-f.readCh:
		n := copy(p, chunk)
		return n, nil
	}
}

func (f *fakePTY) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errSessionClosed
	}
	return nil
}

func (f *fakePTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.closeCh)
	return nil
}

func (f *fakePTY) EmitOutput(data []byte) {
	f.readCh <- append([]byte(nil), data...)
}

func (f *fakePTY) Written() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.writes.Bytes()...)
}
