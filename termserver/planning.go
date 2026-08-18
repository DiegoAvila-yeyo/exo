package termserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/DiegoAvila-yeyo/exo/planningstore"
)

// planningStore is the subset of *planningstore.Store the server needs,
// mirroring the existing chatSessionStore/sessionStore decoupling pattern —
// keeps termserver decoupled from planningstore's on-disk format.
type planningStore interface {
	Create(name string) (planningstore.Planning, error)
	Load(id string) (planningstore.Planning, error)
	Save(p planningstore.Planning) error
	List() ([]planningstore.PlanningSummary, error)
}

type createPlanningRequest struct {
	Name string `json:"name"`
}

type createBoardRequest struct {
	Name string `json:"name"`
}

// handlePlannings serves the Planning list screen: GET lists every
// Planning, POST creates a new (empty) one.
func (s *Server) handlePlannings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if s.planning == nil {
			writeJSON(w, http.StatusOK, []planningstore.PlanningSummary{})
			return
		}
		list, err := s.planning.List()
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
		if s.planning == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "planning is not configured"})
			return
		}
		var req createPlanningRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		p, err := s.planning.Create(req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusCreated, p)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePlanningDetail dispatches everything under /api/plannings/{id}...:
//
//	/api/plannings/{id}                                          GET
//	/api/plannings/{id}/boards                                   GET, POST
//	/api/plannings/{id}/boards/{board_id}                        GET
//	/api/plannings/{id}/boards/{board_id}/knowledge              GET
//	/api/plannings/{id}/knowledge/{knowledge_id}/accept          POST
//	/api/plannings/{id}/knowledge/{knowledge_id}/reject          POST
func (s *Server) handlePlanningDetail(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/plannings/"), "/")
	segments := strings.Split(trimmed, "/")
	if len(segments) == 0 || segments[0] == "" {
		http.NotFound(w, r)
		return
	}
	planningID := segments[0]

	switch {
	case len(segments) == 1:
		s.handleGetPlanning(w, r, planningID)
	case len(segments) == 2 && segments[1] == "boards":
		s.handleBoards(w, r, planningID)
	case len(segments) == 3 && segments[1] == "boards":
		s.handleGetBoard(w, r, planningID, segments[2])
	case len(segments) == 4 && segments[1] == "boards" && segments[3] == "knowledge":
		s.handleBoardKnowledge(w, r, planningID, segments[2])
	case len(segments) == 4 && segments[1] == "knowledge" && (segments[3] == "accept" || segments[3] == "reject"):
		s.handleKnowledgeTransition(w, r, planningID, segments[2], segments[3])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleGetPlanning(w http.ResponseWriter, r *http.Request, planningID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	p, ok := s.loadPlanningOr404(w, planningID)
	if !ok {
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleBoards(w http.ResponseWriter, r *http.Request, planningID string) {
	switch r.Method {
	case http.MethodGet:
		if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		p, ok := s.loadPlanningOr404(w, planningID)
		if !ok {
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusOK, p.Boards)
	case http.MethodPost:
		if !ValidOrigin(r.Header.Get("Origin"), s.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if err := ValidateDoubleSubmit(r); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if s.planning == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "planning is not configured"})
			return
		}
		var req createBoardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		p, ok := s.loadPlanningOr404(w, planningID)
		if !ok {
			return
		}
		board := p.AddBoard(req.Name)
		if err := s.planning.Save(p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.recordActivity()
		writeJSON(w, http.StatusCreated, board)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request, planningID, boardID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	p, ok := s.loadPlanningOr404(w, planningID)
	if !ok {
		return
	}
	for _, b := range p.Boards {
		if b.ID == boardID {
			s.recordActivity()
			writeJSON(w, http.StatusOK, b)
			return
		}
	}
	http.NotFound(w, r)
}

// handleBoardKnowledge is Round 1's only Knowledge-facing endpoint —
// contextual disclosure for one board, not a general Knowledge CRUD API.
func (s *Server) handleBoardKnowledge(w http.ResponseWriter, r *http.Request, planningID, boardID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	p, ok := s.loadPlanningOr404(w, planningID)
	if !ok {
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, p.ForBoard(boardID))
}

// handleKnowledgeTransition performs exactly one of two state transitions:
// ai_suggested -> accepted, or ai_suggested -> rejected. It is not a general
// "set author" endpoint — any Knowledge entry whose current Author isn't
// AuthorAISuggested (already human, already accepted/rejected, archived...)
// is refused with 409, unchanged. AuthorState is a state machine, and these
// two endpoints only resolve the one pending state Round 2 introduces.
func (s *Server) handleKnowledgeTransition(w http.ResponseWriter, r *http.Request, planningID, knowledgeID, action string) {
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
	p, ok := s.loadPlanningOr404(w, planningID)
	if !ok {
		return
	}

	var target *planningstore.Knowledge
	for i := range p.Knowledge {
		if p.Knowledge[i].ID == knowledgeID {
			target = &p.Knowledge[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "knowledge entry not found", http.StatusNotFound)
		return
	}
	if target.Author != planningstore.AuthorAISuggested {
		http.Error(w, "only ai_suggested entries can be accepted or rejected", http.StatusConflict)
		return
	}

	if action == "accept" {
		target.Author = planningstore.AuthorAccepted
	} else {
		target.Author = planningstore.AuthorRejected
	}
	target.UpdatedAt = time.Now()

	if err := s.planning.Save(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, *target)
}

func (s *Server) loadPlanningOr404(w http.ResponseWriter, planningID string) (planningstore.Planning, bool) {
	if s.planning == nil {
		http.NotFound(w, nil)
		return planningstore.Planning{}, false
	}
	p, err := s.planning.Load(planningID)
	if err != nil {
		if errors.Is(err, planningstore.ErrNotFound) {
			http.Error(w, "planning not found", http.StatusNotFound)
			return planningstore.Planning{}, false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return planningstore.Planning{}, false
	}
	return p, true
}
