package termserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/DiegoAvila-yeyo/exo/projects"
	"github.com/DiegoAvila-yeyo/exo/ptyactor"
	"github.com/DiegoAvila-yeyo/exo/sessionrecall"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/gorilla/websocket"
	"github.com/yeyoos/nucleo-base/shared/api"
)

const (
	csrfCookieName   = "nucleo_csrf"
	tokenMetaName    = "nucleo-token"
	csrfMetaName     = "nucleo-csrf"
	tokenSubprotocol = "nucleo-term."
)

type sessionStore interface {
	Create(workdir, name string) (sessions.SessionInfo, error)
	List() []sessions.SessionInfo
	Get(id string) (*ptyactor.Session, sessions.SessionInfo, bool)
	Close(id string) error
	Touch(id string) bool
}

type statusMessage struct {
	Type  string `json:"type"`
	Owner string `json:"owner,omitempty"`
	Epoch uint64 `json:"epoch,omitempty"`
	Error string `json:"error,omitempty"`
}

type controlMessage struct {
	Type  string `json:"type"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Owner string `json:"owner,omitempty"`
}

type createSessionRequest struct {
	Workdir string `json:"workdir"`
	Name    string `json:"name,omitempty"`
}

type Server struct {
	port            string
	token           string
	csrfToken       string
	store           sessionStore
	onActivity      func()
	createGuard     func() error
	runner          AgentRunner
	mux             *http.ServeMux
	hubsMu          sync.Mutex
	hubs            map[string]*sessionHub
	agentMu         sync.Mutex
	chatBroadcaster *chatBroadcaster
	approval        pendingApproval
	chatStore       chatSessionStore
	projectRoot     string
	scanProjects    projectScanner
	planning        planningStore
	canvas          canvasStore
	sessionRecall   sessionRecallStore
	summarizer      SessionSummarizer
}

// sessionRecallStore is the subset of *sessionrecall.Store the server
// needs — mirrors chatSessionStore/canvasStore's decoupling pattern.
type sessionRecallStore interface {
	Load(projectID string) (sessionrecall.ProjectRecall, error)
	Save(pr sessionrecall.ProjectRecall) (sessionrecall.ProjectRecall, error)
}

// SessionSummarizer runs the separate, minimal summarization call
// (build_prompt_SESSION_RECALL.md, piece 6, step 1) over a session's title
// + transcript, producing {title, description, summary_body}. Matches
// agenthost.Host.SummarizeSession's exact method signature so backend.go
// can wire host.SummarizeSession in directly as a method value.
type SessionSummarizer func(ctx context.Context, title string, messages []api.Message) (string, string, string, error)

type sessionHub struct {
	clients map[*wsClient]struct{}
}

type wsClient struct {
	leaseUpdates chan leaseUpdate
}

type leaseUpdate struct {
	lease    ptyactor.Lease
	canWrite bool
}

type Option func(*Server)

func WithActivityHook(hook func()) Option {
	return func(server *Server) {
		server.onActivity = hook
	}
}

func WithCreateGuard(guard func() error) Option {
	return func(server *Server) {
		server.createGuard = guard
	}
}

func WithAgentRunner(runner AgentRunner) Option {
	return func(server *Server) {
		server.runner = runner
	}
}

// WithChatStore enables persisted, named, multi-session chat. Without it,
// POST /api/chat and the sidebar's session list behave exactly as before —
// a single unnamed, in-memory-only conversation.
func WithChatStore(store chatSessionStore) Option {
	return func(server *Server) {
		server.chatStore = store
	}
}

// WithProjectRoot enables GET /api/projects, scanning root live on every
// request. Without it, the endpoint returns an empty list.
func WithProjectRoot(root string) Option {
	return WithProjectScanner(root, projects.Scan)
}

// WithProjectScanner is WithProjectRoot with the scan function injectable —
// tests use this to avoid touching the real filesystem.
func WithProjectScanner(root string, scan projectScanner) Option {
	return func(server *Server) {
		server.projectRoot = root
		server.scanProjects = scan
	}
}

// WithPlanningStore enables the Planning section's HTTP API
// (/api/plannings...). Without it, GET endpoints return empty results and
// POST/write endpoints respond 503 "planning is not configured" — same
// optional-store shape as WithChatStore.
func WithPlanningStore(store planningStore) Option {
	return func(server *Server) {
		server.planning = store
	}
}

// WithCanvasStore enables the Canvas section's HTTP API (/api/canvases...).
// Without it, GET /api/canvases returns an empty ProjectCanvas and
// write endpoints respond 503 "canvas is not configured" — same
// optional-store shape as WithPlanningStore.
func WithCanvasStore(store canvasStore) Option {
	return func(server *Server) {
		server.canvas = store
	}
}

// WithSessionRecallStore enables the "Sesiones" close-and-summarize
// endpoint (POST /api/chat/sessions/{id}/close). Without it (or without
// WithSessionSummarizer), that endpoint responds 503 "session recall is not
// configured" — same optional-store shape as WithCanvasStore.
func WithSessionRecallStore(store sessionRecallStore) Option {
	return func(server *Server) {
		server.sessionRecall = store
	}
}

// WithSessionSummarizer supplies the close endpoint's summarization call —
// see SessionSummarizer's doc comment.
func WithSessionSummarizer(fn SessionSummarizer) Option {
	return func(server *Server) {
		server.summarizer = fn
	}
}

func New(port int, store sessionStore, opts ...Option) (*Server, error) {
	if store == nil {
		return nil, errors.New("termserver: session store is required")
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrfToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	s := &Server{
		port:            strconv.Itoa(port),
		token:           token,
		csrfToken:       csrfToken,
		store:           store,
		mux:             http.NewServeMux(),
		hubs:            make(map[string]*sessionHub),
		chatBroadcaster: newChatBroadcaster(),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Token() string {
	return s.token
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.Handle("/assets/", http.StripPrefix("/assets/", staticHandler()))
	s.mux.HandleFunc("/api/echo", s.handleEcho)
	s.mux.HandleFunc("/api/chat", s.handleChat)
	s.mux.HandleFunc("/api/chat/stream", s.handleChatStream)
	s.mux.HandleFunc("/api/chat/sessions", s.handleChatSessions)
	s.mux.HandleFunc("/api/chat/sessions/", s.handleChatSessionDetail)
	s.mux.HandleFunc("/api/approve", s.handleApprove)
	s.mux.HandleFunc("/api/projects", s.handleProjects)
	s.mux.HandleFunc("/api/sessions", s.handleSessions)
	s.mux.HandleFunc("/api/sessions/", s.handleSessionAction)
	s.mux.HandleFunc("/api/terminal/", s.handleTerminalStream)
	s.mux.HandleFunc("/api/plannings", s.handlePlannings)
	s.mux.HandleFunc("/api/plannings/", s.handlePlanningDetail)
	s.mux.HandleFunc("/api/canvases", s.handleCanvas)
	s.mux.HandleFunc("/api/canvases/objects/", s.handleCanvasObjectDetail)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    s.csrfToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	if err := s.renderIndex(w); err != nil {
		http.Error(w, "failed to render index", http.StatusInternalServerError)
	}
}

func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if err := ValidateDoubleSubmit(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	s.recordActivity()

	var payload struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": payload.Message})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusOK, s.store.List())
	case http.MethodPost:
		if !ValidOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if err := ValidateDoubleSubmit(r); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if s.createGuard != nil {
			if err := s.createGuard(); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		var req createSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		info, err := s.store.Create(req.Workdir, req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusCreated, info)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	if !ValidOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := ValidateDoubleSubmit(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	sessionID, ok := sessionIDFromPath(r.URL.Path, "/api/sessions/", "/close")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.Close(sessionID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.recordActivity()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	sessionID, ok := sessionIDFromPath(r.URL.Path, "/api/terminal/", "/stream")
	if !ok {
		http.NotFound(w, r)
		return
	}

	session, _, found := s.store.Get(sessionID)
	if !found {
		http.NotFound(w, r)
		return
	}

	matchedProtocol, ok := matchTokenSubprotocol(r, s.token)
	if !ok {
		http.Error(w, "forbidden websocket token", http.StatusForbidden)
		return
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, http.Header{
		"Sec-WebSocket-Protocol": []string{matchedProtocol},
	})
	if err != nil {
		return
	}
	defer conn.Close()

	client := &wsClient{leaseUpdates: make(chan leaseUpdate, 8)}
	s.registerClient(sessionID, client)
	defer s.unregisterClient(sessionID, client)

	readLease := session.Lease()
	writeLease := readLease
	outputCh, unsubscribe, err := session.SubscribeWithLease(readLease)
	if err != nil {
		_ = conn.WriteJSON(statusMessage{Type: "error", Error: err.Error()})
		return
	}
	defer unsubscribe()
	s.store.Touch(sessionID)
	s.recordActivity()

	if err := conn.WriteJSON(statusMessage{Type: "ready", Owner: readLease.Owner, Epoch: readLease.Epoch}); err != nil {
		return
	}

	incoming := make(chan wsIncoming)
	readErrs := make(chan error, 1)
	go readLoop(conn, incoming, readErrs)

	for {
		select {
		case chunk, ok := <-outputCh:
			if !ok {
				outputCh = nil
				continue
			}
			s.store.Touch(sessionID)
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
			s.recordActivity()
		case err := <-readErrs:
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			if errors.Is(err, io.EOF) || websocket.IsUnexpectedCloseError(err) {
				return
			}
			return
		case msg, ok := <-incoming:
			if !ok {
				return
			}
			switch msg.messageType {
			case websocket.BinaryMessage:
				err := session.WriteWithLease(writeLease, msg.payload)
				if errors.Is(err, ptyactor.ErrOwnershipLost) {
					if writeErr := conn.WriteJSON(statusMessage{Type: "ownership_lost"}); writeErr != nil {
						return
					}
					continue
				}
				if err != nil {
					_ = conn.WriteJSON(statusMessage{Type: "error", Error: err.Error()})
					return
				}
				s.store.Touch(sessionID)
				s.recordActivity()
			case websocket.TextMessage:
				nextReadLease, nextWriteLease, nextOutput, nextUnsub, keepGoing := s.handleControlMessage(conn, sessionID, session, client, writeLease, msg.payload)
				if !keepGoing {
					return
				}
				if nextReadLease.Epoch != 0 {
					readLease = nextReadLease
				}
				if nextWriteLease.Epoch != 0 {
					writeLease = nextWriteLease
				}
				if nextOutput != nil {
					unsubscribe()
					outputCh = nextOutput
					unsubscribe = nextUnsub
				}
			default:
				_ = conn.WriteJSON(statusMessage{Type: "error", Error: "unsupported frame type"})
				return
			}
		case update := <-client.leaseUpdates:
			unsubscribe()
			nextOutput, nextUnsub, err := session.SubscribeWithLease(update.lease)
			if err != nil {
				_ = conn.WriteJSON(statusMessage{Type: "error", Error: err.Error()})
				return
			}
			outputCh = nextOutput
			unsubscribe = nextUnsub
			readLease = update.lease
			if update.canWrite {
				writeLease = update.lease
			}
			s.store.Touch(sessionID)
			s.recordActivity()
			if err := conn.WriteJSON(statusMessage{Type: "lease", Owner: update.lease.Owner, Epoch: update.lease.Epoch}); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleControlMessage(
	conn *websocket.Conn,
	sessionID string,
	session *ptyactor.Session,
	client *wsClient,
	writeLease ptyactor.Lease,
	payload []byte,
) (ptyactor.Lease, ptyactor.Lease, <-chan []byte, func(), bool) {
	var msg controlMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		_ = conn.WriteJSON(statusMessage{Type: "error", Error: "invalid control payload"})
		return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, false
	}

	switch msg.Type {
	case "resize":
		err := session.ResizeWithLease(writeLease, msg.Cols, msg.Rows)
		if errors.Is(err, ptyactor.ErrOwnershipLost) {
			if writeErr := conn.WriteJSON(statusMessage{Type: "ownership_lost"}); writeErr != nil {
				return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, false
			}
			return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, true
		}
		if err != nil {
			_ = conn.WriteJSON(statusMessage{Type: "error", Error: err.Error()})
			return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, false
		}
		s.store.Touch(sessionID)
		s.recordActivity()
		return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, true
	case "takeover":
		nextLease, err := session.Takeover(msg.Owner)
		if err != nil {
			_ = conn.WriteJSON(statusMessage{Type: "error", Error: err.Error()})
			return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, false
		}
		s.store.Touch(sessionID)
		s.recordActivity()
		s.broadcastLease(sessionID, nextLease, client)
		return nextLease, nextLease, nil, nil, true
	default:
		_ = conn.WriteJSON(statusMessage{Type: "error", Error: fmt.Sprintf("unsupported control type %q", msg.Type)})
		return ptyactor.Lease{}, ptyactor.Lease{}, nil, nil, false
	}
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func matchTokenSubprotocol(r *http.Request, token string) (string, bool) {
	want := tokenSubprotocol + token
	for _, offered := range websocket.Subprotocols(r) {
		if constantTimeEqual(strings.TrimSpace(offered), want) {
			return want, true
		}
	}
	return "", false
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

type wsIncoming struct {
	messageType int
	payload     []byte
}

func readLoop(conn *websocket.Conn, incoming chan<- wsIncoming, errs chan<- error) {
	defer close(incoming)
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			errs <- err
			return
		}
		incoming <- wsIncoming{messageType: messageType, payload: payload}
	}
}

func (s *Server) registerClient(sessionID string, client *wsClient) {
	s.hubsMu.Lock()
	defer s.hubsMu.Unlock()

	hub, ok := s.hubs[sessionID]
	if !ok {
		hub = &sessionHub{clients: make(map[*wsClient]struct{})}
		s.hubs[sessionID] = hub
	}
	hub.clients[client] = struct{}{}
}

func (s *Server) unregisterClient(sessionID string, client *wsClient) {
	s.hubsMu.Lock()
	defer s.hubsMu.Unlock()

	hub, ok := s.hubs[sessionID]
	if !ok {
		return
	}
	delete(hub.clients, client)
	if len(hub.clients) == 0 {
		delete(s.hubs, sessionID)
	}
}

func (s *Server) broadcastLease(sessionID string, lease ptyactor.Lease, writerClient *wsClient) {
	s.hubsMu.Lock()
	defer s.hubsMu.Unlock()

	hub, ok := s.hubs[sessionID]
	if !ok {
		return
	}
	for client := range hub.clients {
		select {
		case client.leaseUpdates <- leaseUpdate{lease: lease, canWrite: client == writerClient}:
		default:
		}
	}
}

func sessionIDFromPath(path, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return "", false
	}
	return trimmed, true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) recordActivity() {
	if s.onActivity != nil {
		s.onActivity()
	}
}
