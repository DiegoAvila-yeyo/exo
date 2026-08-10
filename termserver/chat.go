package termserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

type AgentRunner func(ctx context.Context, input string) error

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

type chatSubscription struct {
	output chan string
	done   chan struct{}
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

func (b *chatBroadcaster) Subscribe() (chan string, chan struct{}) {
	sub := &chatSubscription{
		output: make(chan string, 1024),
		done:   make(chan struct{}, 1),
	}
	b.mu.Lock()
	b.subs[sub.output] = sub
	b.mu.Unlock()
	return sub.output, sub.done
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
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		http.Error(w, "missing message", http.StatusBadRequest)
		return
	}
	if !s.agentMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "busy"})
		return
	}

	s.chatBroadcaster.ClearReplay()
	go func(message string) {
		defer func() {
			s.agentMu.Unlock()
			s.chatBroadcaster.NotifyDone()
		}()
		if err := s.runner(context.Background(), message); err != nil {
			_, _ = io.WriteString(s.chatBroadcaster, "error: "+err.Error()+"\n")
		}
	}(body.Message)

	s.recordActivity()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
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

	output, done := s.chatBroadcaster.Subscribe()
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
