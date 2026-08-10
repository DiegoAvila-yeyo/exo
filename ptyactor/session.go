package ptyactor

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
)

var (
	// ErrOwnershipLost is returned when a write or subscription uses a stale epoch.
	ErrOwnershipLost = errors.New("ptyactor: ownership lost")

	errSessionClosed = errors.New("ptyactor: session closed")
)

const (
	defaultOwner             = "agent"
	defaultRingBufferSize    = 4096
	defaultSubscriberBufSize = 8
	defaultReadBufferSize    = 4096
)

type scrubRule struct {
	pattern     *regexp.Regexp
	replacement []byte
}

var scrubRules = []scrubRule{
	{
		// PEM private key blocks are sensitive even when only the header is visible in terminal output.
		pattern:     regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
		replacement: []byte("[REDACTED PRIVATE KEY]"),
	},
	{
		// Catch partial PEM output when the terminal chunk only contains the header line.
		pattern:     regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
		replacement: []byte("-----BEGIN [REDACTED PRIVATE KEY]-----"),
	},
	{
		// Authorization bearer headers frequently carry API tokens or JWTs.
		pattern:     regexp.MustCompile(`(?i)(Authorization\s*:\s*Bearer\s+)[A-Za-z0-9._~+/=-]{12,}`),
		replacement: []byte("${1}[REDACTED]"),
	},
	{
		// AWS access key ids are recognizable static formats that should never be echoed back.
		pattern:     regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
		replacement: []byte("[REDACTED AWS ACCESS KEY]"),
	},
	{
		// JWTs commonly show up in terminal output and are bearer credentials in practice.
		pattern:     regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9._-]{10,}\.[A-Za-z0-9._-]{10,}\b`),
		replacement: []byte("[REDACTED JWT]"),
	},
	{
		// Broad assignment matcher for common secret variable names and suffixes.
		pattern: regexp.MustCompile(
			`(?i)(\b(?:[A-Z0-9_]*?(?:API[_-]?KEY|TOKEN|SECRET|PASSWORD|PASSWD|PASSPHRASE|PRIVATE[_-]?KEY|CLIENT[_-]?SECRET|SESSION[_-]?TOKEN|AUTH[_-]?TOKEN|REFRESH[_-]?TOKEN|ACCESS[_-]?KEY(?:_ID)?|SECRET[_-]?ACCESS[_-]?KEY)|[A-Z0-9]+(?:_KEY|_TOKEN|_SECRET|_PASSWORD|_PASSWD))\b\s*[:=]\s*)(?:"[^"\n]*"|'[^'\n]*'|[^\s]+)`,
		),
		replacement: []byte("${1}[REDACTED]"),
	},
}

// PTY abstracts the underlying terminal device so the actor can be tested in memory.
type PTY interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
}

// Lease captures the owner and epoch visible to a caller at one point in time.
type Lease struct {
	Owner string
	Epoch uint64
}

// Option customizes session behavior.
type Option func(*config)

// Session serializes PTY state behind a single actor goroutine.
type Session struct {
	pty        PTY
	requests   chan any
	output     chan []byte
	done       chan struct{}
	closeOnce  sync.Once
	readerDone sync.WaitGroup
	epoch      atomic.Uint64
	owner      atomic.Value
	testHooks  testHooks
}

type config struct {
	initialOwner      string
	ringBufferSize    int
	subscriberBufSize int
	readBufferSize    int
	testHooks         testHooks
}

type testHooks struct {
	afterWriteLeaseCaptured     func()
	afterSubscribeLeaseCaptured func()
	afterOutputHandled          func([]byte)
}

type subscriber struct {
	epoch uint64
	ch    chan []byte
}

type writeRequest struct {
	lease Lease
	data  []byte
	reply chan error
}

type resizeRequest struct {
	lease Lease
	cols  int
	rows  int
	reply chan error
}

type subscribeRequest struct {
	lease Lease
	reply chan subscribeResult
}

type unsubscribeRequest struct {
	id uint64
}

type takeoverRequest struct {
	newOwner string
	reply    chan takeoverResult
}

type closeRequest struct {
	reply chan error
}

type ptyOutput struct {
	data []byte
}

type ptyReadFailure struct {
	err error
}

type subscribeResult struct {
	id  uint64
	ch  <-chan []byte
	err error
}

type takeoverResult struct {
	lease Lease
	err   error
}

// NewSession starts a new actor-backed PTY session.
func NewSession(pty PTY, opts ...Option) *Session {
	cfg := config{
		initialOwner:      defaultOwner,
		ringBufferSize:    defaultRingBufferSize,
		subscriberBufSize: defaultSubscriberBufSize,
		readBufferSize:    defaultReadBufferSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.initialOwner == "" {
		cfg.initialOwner = defaultOwner
	}
	if cfg.ringBufferSize <= 0 {
		cfg.ringBufferSize = defaultRingBufferSize
	}
	if cfg.subscriberBufSize <= 0 {
		cfg.subscriberBufSize = defaultSubscriberBufSize
	}
	if cfg.readBufferSize <= 0 {
		cfg.readBufferSize = defaultReadBufferSize
	}

	session := &Session{
		pty:       pty,
		requests:  make(chan any),
		output:    make(chan []byte, cfg.subscriberBufSize),
		done:      make(chan struct{}),
		testHooks: cfg.testHooks,
	}
	session.epoch.Store(1)
	session.owner.Store(cfg.initialOwner)
	session.readerDone.Add(1)

	go session.run(cfg)
	go session.readLoop(cfg.readBufferSize)

	return session
}

// Lease returns the owner and epoch currently visible to callers.
func (s *Session) Lease() Lease {
	return Lease{
		Owner: s.owner.Load().(string),
		Epoch: s.epoch.Load(),
	}
}

// Write captures the current lease and writes to the PTY.
func (s *Session) Write(data []byte) error {
	lease := s.Lease()
	if s.testHooks.afterWriteLeaseCaptured != nil {
		s.testHooks.afterWriteLeaseCaptured()
	}
	return s.WriteWithLease(lease, data)
}

// WriteWithLease writes only if the captured lease still matches the active epoch.
func (s *Session) WriteWithLease(lease Lease, data []byte) error {
	reply := make(chan error, 1)
	req := writeRequest{
		lease: lease,
		data:  append([]byte(nil), data...),
		reply: reply,
	}
	if err := s.send(req); err != nil {
		return err
	}
	return <-reply
}

// Subscribe captures the current lease and attaches to replay plus live output.
func (s *Session) Subscribe() (<-chan []byte, func(), error) {
	lease := s.Lease()
	if s.testHooks.afterSubscribeLeaseCaptured != nil {
		s.testHooks.afterSubscribeLeaseCaptured()
	}
	return s.SubscribeWithLease(lease)
}

// SubscribeWithLease subscribes only if the captured lease still matches the active epoch.
func (s *Session) SubscribeWithLease(lease Lease) (<-chan []byte, func(), error) {
	reply := make(chan subscribeResult, 1)
	if err := s.send(subscribeRequest{lease: lease, reply: reply}); err != nil {
		return nil, nil, err
	}
	result := <-reply
	if result.err != nil {
		return nil, nil, result.err
	}

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			_ = s.send(unsubscribeRequest{id: result.id})
		})
	}
	return result.ch, unsubscribe, nil
}

// Resize forwards a PTY resize through the actor loop.
func (s *Session) Resize(cols, rows int) error {
	return s.ResizeWithLease(s.Lease(), cols, rows)
}

// ResizeWithLease resizes only if the captured lease still matches the active epoch.
func (s *Session) ResizeWithLease(lease Lease, cols, rows int) error {
	reply := make(chan error, 1)
	if err := s.send(resizeRequest{lease: lease, cols: cols, rows: rows, reply: reply}); err != nil {
		return err
	}
	return <-reply
}

// Takeover changes owner, bumps the epoch, and closes old-epoch subscriptions.
func (s *Session) Takeover(newOwner string) (Lease, error) {
	reply := make(chan takeoverResult, 1)
	if err := s.send(takeoverRequest{newOwner: newOwner, reply: reply}); err != nil {
		return Lease{}, err
	}
	result := <-reply
	return result.lease, result.err
}

// Close shuts down the PTY session and all subscribers.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		reply := make(chan error, 1)
		sendErr := s.send(closeRequest{reply: reply})
		if sendErr != nil && !errors.Is(sendErr, errSessionClosed) {
			err = sendErr
			return
		}
		if sendErr == nil {
			err = <-reply
		}
		s.readerDone.Wait()
	})
	return err
}

func (s *Session) send(req any) error {
	select {
	case <-s.done:
		return errSessionClosed
	case s.requests <- req:
		return nil
	}
}

func (s *Session) readLoop(readBufferSize int) {
	defer s.readerDone.Done()

	buf := make([]byte, readBufferSize)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			select {
			case <-s.done:
				return
			case s.output <- chunk:
			}
		}
		if err != nil {
			select {
			case <-s.done:
			case s.requests <- ptyReadFailure{err: err}:
			}
			return
		}
	}
}

func (s *Session) run(cfg config) {
	defer close(s.done)

	owner := cfg.initialOwner
	epoch := uint64(1)
	subscribers := make(map[uint64]subscriber)
	var nextSubscriberID uint64
	ring := newRingBuffer(cfg.ringBufferSize)
	closed := false

	closeSubscribers := func() {
		for id, sub := range subscribers {
			close(sub.ch)
			delete(subscribers, id)
		}
	}

	for !closed {
		select {
		case chunk := <-s.output:
			scrubbed := scrub(chunk)
			if len(scrubbed) == 0 {
				continue
			}
			ring.Write(scrubbed)
			for id, sub := range subscribers {
				select {
				case sub.ch <- append([]byte(nil), scrubbed...):
				default:
					close(sub.ch)
					delete(subscribers, id)
				}
			}
			if s.testHooks.afterOutputHandled != nil {
				s.testHooks.afterOutputHandled(append([]byte(nil), scrubbed...))
			}
		case rawReq := <-s.requests:
			switch req := rawReq.(type) {
			case writeRequest:
				if req.lease.Epoch != epoch {
					req.reply <- ErrOwnershipLost
					continue
				}
				_, err := s.pty.Write(req.data)
				req.reply <- err
			case resizeRequest:
				if req.lease.Epoch != epoch {
					req.reply <- ErrOwnershipLost
					continue
				}
				req.reply <- s.pty.Resize(req.cols, req.rows)
			case subscribeRequest:
				if req.lease.Epoch != epoch {
					req.reply <- subscribeResult{err: ErrOwnershipLost}
					continue
				}
				nextSubscriberID++
				ch := make(chan []byte, cfg.subscriberBufSize)
				replay := ring.Snapshot()
				if len(replay) > 0 {
					ch <- replay
				}
				subscribers[nextSubscriberID] = subscriber{
					epoch: epoch,
					ch:    ch,
				}
				req.reply <- subscribeResult{id: nextSubscriberID, ch: ch}
			case unsubscribeRequest:
				if sub, ok := subscribers[req.id]; ok {
					close(sub.ch)
					delete(subscribers, req.id)
				}
			case takeoverRequest:
				owner = req.newOwner
				epoch++
				s.owner.Store(owner)
				s.epoch.Store(epoch)
				closeSubscribers()
				req.reply <- takeoverResult{lease: Lease{Owner: owner, Epoch: epoch}}
			case closeRequest:
				closeSubscribers()
				req.reply <- s.pty.Close()
				closed = true
			case ptyReadFailure:
				if !errors.Is(req.err, errSessionClosed) {
					closeSubscribers()
					_ = s.pty.Close()
					closed = true
				}
			default:
				panic(fmt.Sprintf("ptyactor: unsupported request type %T", rawReq))
			}
		}
	}
}

func scrub(data []byte) []byte {
	scrubbed := append([]byte(nil), data...)
	for _, rule := range scrubRules {
		scrubbed = rule.pattern.ReplaceAll(scrubbed, rule.replacement)
	}
	return scrubbed
}

// WithInitialOwner sets the first owner label.
func WithInitialOwner(owner string) Option {
	return func(cfg *config) {
		cfg.initialOwner = owner
	}
}

// WithRingBufferSize overrides the replay capacity in bytes.
func WithRingBufferSize(size int) Option {
	return func(cfg *config) {
		cfg.ringBufferSize = size
	}
}

// WithSubscriberBufferSize overrides the per-subscriber channel size.
func WithSubscriberBufferSize(size int) Option {
	return func(cfg *config) {
		cfg.subscriberBufSize = size
	}
}

func withTestHooks(hooks testHooks) Option {
	return func(cfg *config) {
		cfg.testHooks = hooks
	}
}
