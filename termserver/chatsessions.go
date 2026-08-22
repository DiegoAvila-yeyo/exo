package termserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/DiegoAvila-yeyo/exo/chatstore"
	"github.com/DiegoAvila-yeyo/exo/sessionrecall"
)

// chatSessionStore is the subset of *chatstore.Store the server needs,
// mirroring the existing sessionStore interface pattern used for terminal
// sessions — keeps termserver decoupled from chatstore's on-disk format.
type chatSessionStore interface {
	Create() (chatstore.ChatSession, error)
	Load(id string) (chatstore.ChatSession, error)
	Save(session chatstore.ChatSession) error
	List() ([]chatstore.ChatSessionSummary, error)
}

// maxChatTitleLen bounds the auto-derived title so a long first message
// doesn't blow out the sidebar row.
const maxChatTitleLen = 50

// deriveChatTitle turns a first user message into a sidebar-sized title,
// breaking on a word boundary when it truncates.
func deriveChatTitle(message string) string {
	title := strings.TrimSpace(message)
	title = strings.Join(strings.Fields(title), " ") // collapse newlines/repeated spaces
	if len([]rune(title)) <= maxChatTitleLen {
		return title
	}
	runes := []rune(title)
	cut := runes[:maxChatTitleLen]
	if idx := strings.LastIndex(string(cut), " "); idx > 0 {
		cut = []rune(string(cut)[:idx])
	}
	return strings.TrimSpace(string(cut)) + "…"
}

// handleChatSessions serves the sidebar's session list and creates new,
// empty sessions ("New chat").
func (s *Server) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if s.chatStore == nil {
			writeJSON(w, http.StatusOK, []chatstore.ChatSessionSummary{})
			return
		}
		list, err := s.chatStore.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		if !ValidOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if err := ValidateDoubleSubmit(r); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if s.chatStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chat sessions are not configured"})
			return
		}
		session, err := s.chatStore.Create()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusCreated, chatstore.ChatSessionSummary{
			ID:        session.ID,
			Title:     session.Title,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleChatSessionDetail returns one session's full display transcript, so
// the frontend can reopen it exactly as it looked live. POST .../{id}/close
// is dispatched to handleCloseChatSession — see its doc comment.
func (s *Server) handleChatSessionDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/chat/sessions/")
	path = strings.Trim(path, "/")
	if strings.HasSuffix(path, "/close") {
		id := strings.TrimSuffix(path, "/close")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		s.handleCloseChatSession(w, r, id)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	id := path
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if s.chatStore == nil {
		http.NotFound(w, r)
		return
	}

	session, err := s.chatStore.Load(id)
	if err != nil {
		if errors.Is(err, chatstore.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.recordActivity()
	writeJSON(w, http.StatusOK, struct {
		ID                  string                `json:"id"`
		Title               string                `json:"title"`
		Entries             []chatstore.ChatEntry `json:"entries"`
		ProjectPath         string                `json:"project_path,omitempty"`
		PlanningID          string                `json:"planning_id,omitempty"`
		BoardID             string                `json:"board_id,omitempty"`
		Status              string                `json:"status,omitempty"`
		LastTurnTokens      int                   `json:"last_turn_tokens,omitempty"`
		ContextWindowTokens int                   `json:"context_window_tokens,omitempty"`
		ModelID             string                `json:"model_id,omitempty"`
	}{
		ID:                  session.ID,
		Title:               session.Title,
		Entries:             session.Entries,
		ProjectPath:         session.ProjectPath,
		PlanningID:          session.PlanningID,
		BoardID:             session.BoardID,
		Status:              session.Status,
		LastTurnTokens:      session.LastTurnTokens,
		ContextWindowTokens: session.ContextWindowTokens,
		ModelID:             session.ModelID,
	})
}

// handleCloseChatSession runs the close sequence from
// build_prompt_SESSION_RECALL.md's "what's already true" section: generate
// summary -> persist recall entry -> mark chatstore session closed. If
// summarization or the recall-store write fails, the session is left open
// and this returns an error — never reaches step 3 without a persisted
// summary.
//
// Idempotent: closing an already-closed session is a no-op success (no
// second summarization call, no duplicate recall entry) — this is what
// makes it safe to retry after a partial failure (e.g. process died
// between persisting the recall entry and flipping the chatstore status).
func (s *Server) handleCloseChatSession(w http.ResponseWriter, r *http.Request, id string) {
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
	if s.chatStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chat sessions are not configured"})
		return
	}
	if s.sessionRecall == nil || s.summarizer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session recall is not configured"})
		return
	}

	session, err := s.chatStore.Load(id)
	if err != nil {
		if errors.Is(err, chatstore.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if session.Status == chatstore.StatusClosed {
		// Already closed — treat a repeat close as a no-op success rather
		// than erroring or re-summarizing.
		s.recordActivity()
		writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
		return
	}

	title, description, summaryBody, err := s.summarizer(r.Context(), session.Title, session.Messages)
	if err != nil {
		http.Error(w, "failed to summarize session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	entry := sessionrecall.SessionSummary{
		SessionID:         session.ID,
		Title:             title,
		Description:       description,
		SummaryBody:       summaryBody,
		ClosedAt:          time.Now(),
		ModelID:           session.ModelID,
		ContextPctAtClose: TurnUsage{LastTurnTokens: session.LastTurnTokens, ContextWindowTokens: session.ContextWindowTokens}.ContextPct(),
		Status:            sessionrecall.StatusClosed,
	}

	if err := s.upsertSessionRecallEntry(session.ProjectPath, entry); err != nil {
		http.Error(w, "failed to persist session summary: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Only after the summary is durably persisted does the session flip to
	// closed — see the doc comment above.
	session.Status = chatstore.StatusClosed
	if err := s.chatStore.Save(session); err != nil {
		http.Error(w, "session summary saved, but failed to mark session closed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.recordActivity()
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// upsertSessionRecallEntry retries Load-modify-Save against sessionRecall's
// CAS a bounded number of times, replacing any existing entry for
// entry.SessionID (idempotent upsert, not append) — see sessionrecall.
// Store.Save's doc comment.
func (s *Server) upsertSessionRecallEntry(projectPath string, entry sessionrecall.SessionSummary) error {
	const maxAttempts = 5
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		pr, err := s.sessionRecall.Load(projectPath)
		if err != nil {
			return err
		}
		replaced := false
		for i, e := range pr.Entries {
			if e.SessionID == entry.SessionID {
				pr.Entries[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			pr.Entries = append(pr.Entries, entry)
		}
		if _, err := s.sessionRecall.Save(pr); err != nil {
			if errors.Is(err, sessionrecall.ErrStaleVersion) {
				lastErr = err
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}
