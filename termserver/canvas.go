package termserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
)

// canvasStore is the subset of *canvasstore.Store the server needs,
// mirroring planningStore's decoupling pattern (planning.go) — keeps
// termserver decoupled from canvasstore's on-disk format.
type canvasStore interface {
	Load(projectID string) (canvasstore.ProjectCanvas, error)
	Save(pc canvasstore.ProjectCanvas) (canvasstore.ProjectCanvas, error)
}

type canvasManualEditRequest struct {
	Payload         json.RawMessage `json:"payload"`
	ExpectedVersion int             `json:"expected_version"`
}

// handleCanvas serves GET /api/canvases?project_path=... — the current
// ProjectCanvas for a project. There is no POST here: object creation goes
// through the agent's canvas_create_draft tool (agenthost/canvas_tools.go),
// not a direct HTTP write path — the manual-edit surface only mutates an
// object that already exists (handleCanvasObjectDetail).
func (s *Server) handleCanvas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ValidReadOrigin(r.Header.Get("Origin"), s.port) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	projectPath := strings.TrimSpace(r.URL.Query().Get("project_path"))
	if projectPath == "" {
		http.Error(w, "project_path is required", http.StatusBadRequest)
		return
	}
	if s.canvas == nil {
		writeJSON(w, http.StatusOK, canvasstore.ProjectCanvas{ProjectID: projectPath})
		return
	}
	pc, err := s.canvas.Load(projectPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, pc)
}

// handleCanvasObjectDetail dispatches everything under
// /api/canvases/objects/{object_id}...:
//
//	/api/canvases/objects/{object_id}               PATCH
//	/api/canvases/objects/{object_id}/activate       POST
//	/api/canvases/objects/{object_id}/deactivate     POST
func (s *Server) handleCanvasObjectDetail(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/canvases/objects/"), "/")
	segments := strings.Split(trimmed, "/")
	if len(segments) == 0 || segments[0] == "" {
		http.NotFound(w, r)
		return
	}
	objectID := segments[0]

	switch {
	case len(segments) == 1:
		s.handleCanvasObjectPatch(w, r, objectID)
	case len(segments) == 2 && segments[1] == "activate":
		s.handleCanvasObjectActivation(w, r, objectID, canvasstore.ActivationActive)
	case len(segments) == 2 && segments[1] == "deactivate":
		s.handleCanvasObjectActivation(w, r, objectID, canvasstore.ActivationInactive)
	default:
		http.NotFound(w, r)
	}
}

// handleCanvasObjectPatch is the manual editing module's write path — it
// mutates Payload via a new CanvasAtom (never in place, see
// canvasstore.ProjectCanvas.AppendAtom) and is subject to the same
// optimistic-concurrency rule as every other canvas write: the caller must
// send back the Version it last read (expected_version), or the write is
// rejected with 409 so the client re-GETs and retries — the same
// 409-on-conflict convention chat.go's TryLock busy response already uses.
func (s *Server) handleCanvasObjectPatch(w http.ResponseWriter, r *http.Request, objectID string) {
	if r.Method != http.MethodPatch {
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
	if s.canvas == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "canvas is not configured"})
		return
	}
	projectPath := strings.TrimSpace(r.URL.Query().Get("project_path"))
	if projectPath == "" {
		http.Error(w, "project_path is required", http.StatusBadRequest)
		return
	}
	var req canvasManualEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	pc, err := s.canvas.Load(projectPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pc.Version != req.ExpectedVersion {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "stale_version"})
		return
	}
	if _, err := pc.AppendAtom(objectID, req.Payload); err != nil {
		if errors.Is(err, canvasstore.ErrObjectNotFound) {
			http.Error(w, "object not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.canvas.Save(pc)
	if err != nil {
		if errors.Is(err, canvasstore.ErrStaleVersion) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "stale_version"})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, saved)
}

// handleCanvasObjectActivation is the human-curated activate/deactivate
// action — deliberately HTTP-only, not an agent tool, per the Canvas build
// spec's "keep the active set small and human-curated by design — no
// automatic accumulation."
func (s *Server) handleCanvasObjectActivation(w http.ResponseWriter, r *http.Request, objectID string, activation canvasstore.Activation) {
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
	if s.canvas == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "canvas is not configured"})
		return
	}
	projectPath := strings.TrimSpace(r.URL.Query().Get("project_path"))
	if projectPath == "" {
		http.Error(w, "project_path is required", http.StatusBadRequest)
		return
	}

	pc, err := s.canvas.Load(projectPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := pc.SetActivation(objectID, activation); err != nil {
		if errors.Is(err, canvasstore.ErrObjectNotFound) {
			http.Error(w, "object not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.canvas.Save(pc)
	if err != nil {
		if errors.Is(err, canvasstore.ErrStaleVersion) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "stale_version"})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.recordActivity()
	writeJSON(w, http.StatusOK, saved)
}
