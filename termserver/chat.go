package termserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/chatstore"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// AgentRunner runs one turn against the given conversation history and
// returns the agent's updated history afterward, so the caller can persist
// it per chat session. history may be nil when session persistence isn't
// configured (Server.chatStore == nil) — the runner must tolerate that.
// projectPath, when non-empty, is the folder the session picked in the
// composer — the runner is expected to move the agent there (and reread
// that project's own rules) before running the turn. planningID/boardID
// scope the turn to a Planning, optionally a specific Board within it — see
// resolvePlanningContext for how they're validated before reaching here.
// By the time AgentRunner sees them, exactly one of three shapes holds:
// both empty (no Planning), planningID set with boardID empty (in a
// Planning, no Board open — e.g. a Planning with no Boards yet), or both
// set (in a specific Board). boardID is never set with planningID empty.
//
// The returned *NavigateAction (Round 3) is non-nil when a navigation tool
// committed a destination during the turn — see NavigateAction's doc
// comment for the committed-vs-delivered distinction. It must be checked
// and, if non-nil, delivered to the frontend regardless of whether err is
// also non-nil: a tool's persisted side effect (a Board/Planning actually
// created) already happened by the time it committed, and a later,
// unrelated failure in the rest of the turn must not make it disappear.
// The returned *CanvasSuggestion (Canvas build) is non-nil when this turn's
// tool activity left exactly one new draft Canvas object the agent's own
// reply signals as well-defined enough to materialize — see
// CanvasSuggestion's doc comment. Like NavigateAction, it must be checked
// and delivered regardless of whether err is also non-nil.
//
// canvasObjectID is "" for an ordinary main-composer turn, or a specific
// object_id when this turn came from the floating panel's mini-chat for
// that object (handleChat reads it from the request body's
// canvas_object_id, sent only by the mini-chat — see app.js). The runner is
// expected to thread it into Host.BeginTurn so canvas_edit_object/
// canvas_activate_object/canvas_deactivate_object/canvas_create_draft/
// canvas_materialize_draft refuse to act on any other object for the
// duration of this turn — see agenthost's canvasCell.checkScope for why:
// with more than one Canvas object anchored at once, the model can't
// otherwise be trusted to infer which one a mini-chat message is about.
type AgentRunner func(ctx context.Context, input string, history []api.Message, projectPath string, planningID string, boardID string, canvasObjectID string) ([]api.Message, *NavigateAction, *CanvasSuggestion, error)

// CanvasSuggestion is the contextual "materialize this?" signal — the
// button affordance from the Canvas build spec is never the only way to
// materialize a draft (natural language and /materialize both work
// independent of it), it only appears once the backend has itself detected
// a draft worth surfacing. Detection lives in the concrete AgentRunner
// (backend.go), not here: termserver stays decoupled from how "well-defined
// enough" is decided, the same way it already is from agenthost via the
// AgentRunner contract.
type CanvasSuggestion struct {
	ObjectID string
	Name     string
}

// NavigateAction mirrors agenthost.NavigateAction's shape (deliberately not
// reused directly — termserver stays decoupled from agenthost the same way
// it already is from every other concrete backend, via the AgentRunner
// contract) plus the request-level identity fields a navigation tool has no
// way to know: which browser tab asked, and which of that tab's turns this
// was. handleChat fills ClientID/SessionID/TurnID in from the HTTP request
// that produced this turn; the runner only ever fills the rest.
type NavigateAction struct {
	ClientID     string
	SessionID    string
	TurnID       string
	PlanningID   string
	PlanningName string
	BoardID      string
	BoardName    string
}

// resolvePlanningContext implements the atomic (planning_id, board_id)
// state machine:
//
//	both omitted                  -> preserve existing session context
//	both explicitly ""            -> clear context entirely
//	planning_id set, board_id ""  -> "in a Planning, no Board open" —
//	                                  valid on its own: creating a
//	                                  Planning's first Board doesn't
//	                                  require already being in one
//	both non-empty                -> "in a specific Board" — verified to
//	                                  actually belong to that planning
//	board_id set, planning_id ""  -> invalid: a Board can't exist without
//	                                  its Planning
//	anything else (one nil, one   -> reject: 400, caller must leave
//	  non-nil)                       existing session context untouched
//
// planningID/boardID are nil when the request body omitted the JSON key
// entirely, and non-nil (possibly pointing at "") when the client sent it
// explicitly — that distinction is exactly what this function exists to
// make, since a plain string can't tell "omitted" apart from "sent empty."
func resolvePlanningContext(store planningStore, existingPlanningID, existingBoardID string, planningID, boardID *string) (string, string, error) {
	switch {
	case planningID == nil && boardID == nil:
		return existingPlanningID, existingBoardID, nil
	case planningID == nil || boardID == nil:
		return "", "", errors.New("planning_id and board_id must both be sent or both be omitted")
	}

	p := strings.TrimSpace(*planningID)
	b := strings.TrimSpace(*boardID)
	if p == "" && b == "" {
		return "", "", nil
	}
	if p == "" && b != "" {
		return "", "", errors.New("board_id without planning_id is invalid")
	}
	if store == nil {
		return "", "", errors.New("planning is not configured")
	}
	planning, err := store.Load(p)
	if err != nil {
		return "", "", errors.New("planning not found")
	}
	if b == "" {
		// In the Planning, no specific Board open — e.g. picking a Planning
		// that has no Boards yet. Valid on its own so planning_create_board
		// can bootstrap the first one without already being inside a Board.
		return p, "", nil
	}
	for _, board := range planning.Boards {
		if board.ID == b {
			return p, b, nil
		}
	}
	return "", "", errors.New("board does not belong to that planning")
}

type chatStateEvent struct {
	Type string `json:"type"`
}

type chatOutputEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type chatApprovalEvent struct {
	Type      string `json:"type"`
	Prompt    string `json:"prompt"`
	Detail    string `json:"detail"`
	SessionID string `json:"session_id"`
}

// chatNavigateEvent is the wire shape of a NavigateAction, delivered over
// SSE. client_id is what every tab's frontend filters on — the broadcaster
// still fans this out to every connected tab (Round 3 explicitly doesn't
// add server-side per-client routing), so a tab whose client_id doesn't
// match must ignore it; turn_id additionally lets a tab ignore a navigate
// event that arrives for a turn it no longer considers current (e.g. after
// a reconnect).
type chatNavigateEvent struct {
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	SessionID    string `json:"session_id,omitempty"`
	TurnID       string `json:"turn_id"`
	PlanningID   string `json:"planning_id"`
	PlanningName string `json:"planning_name"`
	BoardID      string `json:"board_id,omitempty"`
	BoardName    string `json:"board_name,omitempty"`
}

// chatCanvasSuggestEvent is the wire shape of a CanvasSuggestion, delivered
// over SSE — fanned out to every connected tab the same way navigate is
// (Round 3 explicitly didn't add per-client routing, and there's no
// per-tab identity reason to add it here either: any tab looking at this
// project's Canvas benefits from knowing a draft is ready).
type chatCanvasSuggestEvent struct {
	Type     string `json:"type"`
	ObjectID string `json:"object_id"`
	Name     string `json:"name"`
}

type chatSubscription struct {
	output        chan string
	done          chan struct{}
	navigate      chan chatNavigateEvent
	canvasSuggest chan chatCanvasSuggestEvent
}

type chatBroadcaster struct {
	mu     sync.RWMutex
	subs   map[chan string]*chatSubscription
	replay []string
}

const maxChatReplay = 200

var chatANSIRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripChatANSI(s string) string {
	return chatANSIRe.ReplaceAllString(s, "")
}

func newChatBroadcaster() *chatBroadcaster {
	return &chatBroadcaster{
		subs:   make(map[chan string]*chatSubscription),
		replay: make([]string, 0, maxChatReplay),
	}
}

func (b *chatBroadcaster) Write(p []byte) (int, error) {
	clean := stripChatANSI(string(p))
	if clean == "" {
		return len(p), nil
	}
	b.mu.Lock()
	b.replay = append(b.replay, clean)
	if len(b.replay) > maxChatReplay {
		b.replay = b.replay[len(b.replay)-maxChatReplay:]
	}
	for _, sub := range b.subs {
		select {
		case sub.output <- clean:
		default:
		}
	}
	b.mu.Unlock()
	return len(p), nil
}

// Snapshot returns a copy of the buffered output chunks for the turn that
// just ran (replay is cleared at the start of every handleChat call, so this
// is exactly that turn's chunks). Used to append the display transcript to a
// persisted chat session once the turn completes.
func (b *chatBroadcaster) Snapshot() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, len(b.replay))
	copy(out, b.replay)
	return out
}

func (b *chatBroadcaster) ClearReplay() {
	b.mu.Lock()
	b.replay = b.replay[:0]
	b.mu.Unlock()
}

func (b *chatBroadcaster) Replay(output chan string) {
	b.mu.RLock()
	lines := make([]string, len(b.replay))
	copy(lines, b.replay)
	b.mu.RUnlock()
	for _, line := range lines {
		select {
		case output <- line:
		default:
		}
	}
}

func (b *chatBroadcaster) Subscribe() (chan string, chan struct{}, chan chatNavigateEvent, chan chatCanvasSuggestEvent) {
	sub := &chatSubscription{
		output:        make(chan string, 1024),
		done:          make(chan struct{}, 1),
		navigate:      make(chan chatNavigateEvent, 4),
		canvasSuggest: make(chan chatCanvasSuggestEvent, 4),
	}
	b.mu.Lock()
	b.subs[sub.output] = sub
	b.mu.Unlock()
	return sub.output, sub.done, sub.navigate, sub.canvasSuggest
}

func (b *chatBroadcaster) Unsubscribe(output chan string) {
	b.mu.Lock()
	delete(b.subs, output)
	b.mu.Unlock()
	close(output)
}

func (b *chatBroadcaster) NotifyDone() {
	b.mu.RLock()
	for _, sub := range b.subs {
		select {
		case sub.done <- struct{}{}:
		default:
		}
	}
	b.mu.RUnlock()
}

// NotifyNavigate fans event out to every connected subscriber — see
// chatNavigateEvent's doc comment for why filtering by client_id/turn_id is
// the frontend's job this round, not the broadcaster's.
func (b *chatBroadcaster) NotifyNavigate(event chatNavigateEvent) {
	b.mu.RLock()
	for _, sub := range b.subs {
		select {
		case sub.navigate <- event:
		default:
		}
	}
	b.mu.RUnlock()
}

// NotifyCanvasSuggest mirrors NotifyNavigate exactly, for
// chatCanvasSuggestEvent.
func (b *chatBroadcaster) NotifyCanvasSuggest(event chatCanvasSuggestEvent) {
	b.mu.RLock()
	for _, sub := range b.subs {
		select {
		case sub.canvasSuggest <- event:
		default:
		}
	}
	b.mu.RUnlock()
}

type pendingApproval struct {
	mu        sync.Mutex
	reply     chan bool
	prompt    string
	detail    string
	sessionID string
	seq       uint64
}

func (p *pendingApproval) start(prompt, detail, sessionID string) chan bool {
	reply := make(chan bool, 1)
	p.mu.Lock()
	p.reply = reply
	p.prompt = prompt
	p.detail = detail
	p.sessionID = sessionID
	p.seq++
	p.mu.Unlock()
	return reply
}

func (p *pendingApproval) respond(approved bool) bool {
	p.mu.Lock()
	reply := p.reply
	p.reply = nil
	p.prompt = ""
	p.detail = ""
	p.sessionID = ""
	p.mu.Unlock()
	if reply == nil {
		return false
	}
	reply <- approved
	return true
}

func (p *pendingApproval) snapshot() (seq uint64, prompt, detail, sessionID string, active bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reply == nil {
		return p.seq, "", "", "", false
	}
	return p.seq, p.prompt, p.detail, p.sessionID, true
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
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
	if s.runner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent runner is not configured"})
		return
	}

	var body struct {
		Message     string  `json:"message"`
		SessionID   string  `json:"session_id,omitempty"`
		ProjectPath string  `json:"project_path,omitempty"`
		PlanningID  *string `json:"planning_id,omitempty"`
		BoardID     *string `json:"board_id,omitempty"`
		// ClientID/TurnID (Round 3) identify which browser tab and which of
		// its submits this request is — the frontend generates both (see
		// build_prompt_PLANNING_ROUND3.md's "Per-tab identity" section) and
		// the server treats them as opaque correlation tokens, never
		// inventing or reinterpreting them. Only used to address a
		// `navigate` SSE event back to the right tab/turn; harmless if
		// empty (an empty client_id just never matches a real tab's own).
		ClientID string `json:"client_id,omitempty"`
		TurnID   string `json:"turn_id,omitempty"`
		// CanvasObjectID scopes this turn to a single Canvas object — sent
		// only by the floating panel's mini-chat (app.js), never by the main
		// composer. See AgentRunner's doc comment.
		CanvasObjectID string `json:"canvas_object_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		http.Error(w, "missing message", http.StatusBadRequest)
		return
	}

	// Persistence is opt-in: when no chatStore is wired up (WithChatStore),
	// behavior is exactly the pre-sessions global chat — no session_id in,
	// none out.
	persist := s.chatStore != nil

	// Load (but do not create) an existing session first, purely to read its
	// current Planning context for resolvePlanningContext's "both omitted"
	// case — session creation and every other mutation waits until after
	// context resolution succeeds, so an invalid pair never has a chance to
	// touch anything, including via an incidental new-session side effect.
	var session chatstore.ChatSession
	sessionLoaded := false
	if persist && body.SessionID != "" {
		var err error
		session, err = s.chatStore.Load(body.SessionID)
		if err != nil {
			if errors.Is(err, chatstore.ErrNotFound) {
				http.Error(w, "chat session not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sessionLoaded = true
	}

	// /materialize <draft-name> is the unambiguous slash-command fallback
	// channel (Canvas build spec) — always resolves to exactly one draft or
	// a clear error, no model inference involved, so it's handled here,
	// before the message ever reaches the agent loop, the same way
	// resolvePlanningContext validates pre-turn rather than inside a turn.
	if draftName, ok := parseMaterializeSlashCommand(body.Message); ok {
		projectPath := body.ProjectPath
		if projectPath == "" {
			projectPath = session.ProjectPath
		}
		s.handleMaterializeSlashCommand(w, draftName, projectPath)
		return
	}

	planningID, boardID, err := resolvePlanningContext(s.planning, session.PlanningID, session.BoardID, body.PlanningID, body.BoardID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if persist {
		if !sessionLoaded {
			var createErr error
			session, createErr = s.chatStore.Create()
			if createErr != nil {
				http.Error(w, createErr.Error(), http.StatusInternalServerError)
				return
			}
		}
		session.Entries = append(session.Entries, chatstore.ChatEntry{Text: "You: " + body.Message, Kind: "system"})
		// Only overwrite when the client actually sent one — omitting
		// project_path on a later message keeps whatever project this
		// session already picked, it doesn't clear it.
		if body.ProjectPath != "" {
			session.ProjectPath = body.ProjectPath
		}
		session.PlanningID = planningID
		session.BoardID = boardID
	}

	if !s.agentMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "busy"})
		return
	}

	s.chatBroadcaster.ClearReplay()
	go func(message string, sess chatstore.ChatSession, planningID, boardID, clientID, turnID, canvasObjectID string) {
		defer func() {
			s.agentMu.Unlock()
			s.chatBroadcaster.NotifyDone()
		}()
		// sess.ID travels on the context, not as a new AgentRunner
		// parameter — see agenthost.ContextWithSessionID's doc comment.
		// Chosen over widening AgentRunner's signature (and every test that
		// constructs one) for a value that only the yeyo gate's telemetry
		// (agenthost/gate_telemetry.go, EXO_YEYO_GATE) currently reads.
		runCtx := agenthost.ContextWithSessionID(context.Background(), sess.ID)
		updated, navAction, canvasSuggestion, err := s.runner(runCtx, message, sess.Messages, sess.ProjectPath, planningID, boardID, canvasObjectID)
		if err != nil {
			_, _ = io.WriteString(s.chatBroadcaster, "error: "+err.Error()+"\n")
		}
		// Delivered on this turn's terminal state — success or error above
		// — never gated behind it: the tool already committed this before
		// Run returned, a later, unrelated failure must not lose it.
		if navAction != nil {
			s.chatBroadcaster.NotifyNavigate(chatNavigateEvent{
				Type:         "navigate",
				ClientID:     clientID,
				SessionID:    sess.ID,
				TurnID:       turnID,
				PlanningID:   navAction.PlanningID,
				PlanningName: navAction.PlanningName,
				BoardID:      navAction.BoardID,
				BoardName:    navAction.BoardName,
			})
		}
		if canvasSuggestion != nil {
			s.chatBroadcaster.NotifyCanvasSuggest(chatCanvasSuggestEvent{
				Type:     "canvas_suggest",
				ObjectID: canvasSuggestion.ObjectID,
				Name:     canvasSuggestion.Name,
			})
		}
		if persist {
			sess.Messages = updated
			for _, line := range s.chatBroadcaster.Snapshot() {
				sess.Entries = append(sess.Entries, chatstore.ChatEntry{Text: line})
			}
			if sess.Title == chatstore.DefaultTitle {
				sess.Title = deriveChatTitle(message)
			}
			if saveErr := s.chatStore.Save(sess); saveErr != nil {
				fmt.Printf("termserver: failed to save chat session %q: %v\n", sess.ID, saveErr)
			}
		}
	}(body.Message, session, planningID, boardID, body.ClientID, body.TurnID, body.CanvasObjectID)

	s.recordActivity()
	resp := map[string]string{"status": "accepted"}
	if persist {
		resp["session_id"] = session.ID
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	output, done, navigate, canvasSuggest := s.chatBroadcaster.Subscribe()
	defer s.chatBroadcaster.Unsubscribe(output)
	s.chatBroadcaster.Replay(output)

	if s.agentMu.TryLock() {
		s.agentMu.Unlock()
		s.writeSSE(w, chatStateEvent{Type: "idle"})
	} else {
		s.writeSSE(w, chatStateEvent{Type: "busy"})
	}

	lastApprovalSeq := uint64(0)
	if seq, prompt, detail, sessionID, active := s.approval.snapshot(); active {
		lastApprovalSeq = seq
		s.writeSSE(w, chatApprovalEvent{
			Type:      "approval",
			Prompt:    prompt,
			Detail:    detail,
			SessionID: sessionID,
		})
	}

	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	approvalTicker := time.NewTicker(250 * time.Millisecond)
	defer approvalTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case text, ok := <-output:
			if !ok {
				return
			}
			s.writeSSE(w, chatOutputEvent{Type: "output", Text: text})
		case <-done:
			s.writeSSE(w, chatStateEvent{Type: "done"})
		case event := <-navigate:
			s.writeSSE(w, event)
		case event := <-canvasSuggest:
			s.writeSSE(w, event)
		case <-approvalTicker.C:
			if seq, prompt, detail, sessionID, active := s.approval.snapshot(); active && seq != lastApprovalSeq {
				lastApprovalSeq = seq
				s.writeSSE(w, chatApprovalEvent{
					Type:      "approval",
					Prompt:    prompt,
					Detail:    detail,
					SessionID: sessionID,
				})
			}
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Approved bool `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if !s.approval.respond(body.Approved) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no pending approval"})
		return
	}

	s.recordActivity()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) RequestApproval(prompt, detail string, meta map[string]string) bool {
	sessionID := ""
	if meta != nil {
		sessionID = meta["session_id"]
	}
	reply := s.approval.start(prompt, detail, sessionID)
	return <-reply
}

func (s *Server) ChatOutputWriter() io.Writer {
	return s.chatBroadcaster
}

func (s *Server) writeSSE(w http.ResponseWriter, payload any) {
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
