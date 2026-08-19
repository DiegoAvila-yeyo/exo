package termserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// parseMaterializeSlashCommand recognizes the "/materialize <draft-name>"
// slash command — the unambiguous fallback channel from the Canvas build
// spec, always resolving to exactly one draft or a clear error, no model
// inference involved. Case-sensitive on the command itself (slash commands
// are typed literally), but the draft name match itself
// (findDraftByNameForMaterialize) is case-insensitive/trimmed like every
// other exact-name lookup in this codebase.
func parseMaterializeSlashCommand(message string) (draftName string, ok bool) {
	const prefix = "/materialize "
	trimmed := strings.TrimSpace(message)
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	if name == "" {
		return "", false
	}
	return name, true
}

// findDraftByNameForMaterialize mirrors agenthost's findDraftByName exactly
// (same exact, case-insensitive, hard-fail-on-ambiguous lookup) — a
// separate copy rather than an import because termserver stays decoupled
// from agenthost, the same way it already is from every concrete backend
// via the AgentRunner contract.
func findDraftByNameForMaterialize(pc canvasstore.ProjectCanvas, name string) (canvasstore.CanvasObject, error) {
	target := strings.ToLower(strings.TrimSpace(name))
	var matches []canvasstore.CanvasObject
	for _, obj := range pc.Objects {
		if obj.Phase != canvasstore.PhaseDraft {
			continue
		}
		if strings.ToLower(strings.TrimSpace(obj.Name)) == target {
			matches = append(matches, obj)
		}
	}
	switch len(matches) {
	case 0:
		return canvasstore.CanvasObject{}, fmt.Errorf("no draft named %q", name)
	case 1:
		return matches[0], nil
	default:
		return canvasstore.CanvasObject{}, fmt.Errorf("more than one draft named %q — ambiguous, rename one or use its object_id", name)
	}
}

// handleMaterializeSlashCommand resolves and materializes draftName against
// projectPath directly, bypassing the agent loop entirely (deterministic
// and fully specified, so there's nothing for the model to decide) — this
// is why it doesn't go through agentMu or s.runner. Writes a short line to
// the chat broadcaster and fires NotifyDone so a connected SSE stream sees
// a visible result, mirroring the normal turn's terminal signal even though
// no turn ran.
func (s *Server) handleMaterializeSlashCommand(w http.ResponseWriter, draftName, projectPath string) {
	if s.canvas == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "canvas is not configured"})
		return
	}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no project is open"})
		return
	}

	pc, err := s.canvas.Load(projectPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	target, err := findDraftByNameForMaterialize(pc, draftName)
	if err != nil {
		_, _ = io.WriteString(s.chatBroadcaster, "/materialize: "+err.Error()+"\n")
		s.chatBroadcaster.NotifyDone()
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}
	if err := pc.Materialize(target.ObjectID); err != nil {
		_, _ = io.WriteString(s.chatBroadcaster, "/materialize: "+err.Error()+"\n")
		s.chatBroadcaster.NotifyDone()
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}
	if _, err := s.canvas.Save(pc); err != nil {
		_, _ = io.WriteString(s.chatBroadcaster, "/materialize: "+err.Error()+"\n")
		s.chatBroadcaster.NotifyDone()
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
		return
	}

	s.recordActivity()
	_, _ = io.WriteString(s.chatBroadcaster, "materialized \""+target.Name+"\" — now visible on the Canvas\n")
	s.chatBroadcaster.NotifyDone()
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
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
	// Validate before writing — a manual JSON edit is free-form (unlike a
	// tool call's schema-enforced input), so this is the one place a typo
	// like an edge referencing a renamed/deleted node id can be caught
	// before it's persisted. Silently dropping such an edge at render time
	// (see app.js's renderDiagramStage) would hide the mistake instead of
	// surfacing it to whoever made the edit.
	if obj := findCanvasObjectByID(pc, objectID); obj != nil && obj.Type == "diagram" {
		if err := validateDiagramPayload(req.Payload); err != nil {
			http.Error(w, "invalid diagram payload: "+err.Error(), http.StatusBadRequest)
			return
		}
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

func findCanvasObjectByID(pc canvasstore.ProjectCanvas, objectID string) *canvasstore.CanvasObject {
	for i := range pc.Objects {
		if pc.Objects[i].ObjectID == objectID {
			return &pc.Objects[i]
		}
	}
	return nil
}

// diagramNodeShape/diagramEdgeShape/diagramPayloadShape and
// validateDiagramPayload mirror agenthost/canvas_tools.go's copies exactly
// — a separate copy rather than a shared import because termserver stays
// decoupled from agenthost, the same way findDraftByNameForMaterialize
// already duplicates agenthost's findDraftByName instead of importing it.
type diagramNodeShape struct {
	ID string `json:"id"`
}

type diagramEdgeShape struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type diagramPayloadShape struct {
	Nodes []diagramNodeShape `json:"nodes"`
	Edges []diagramEdgeShape `json:"edges"`
}

// validateDiagramPayload rejects a diagram payload whose edges reference a
// node id that doesn't exist among its own nodes. A payload that doesn't
// even parse as {nodes, edges} is left alone — not this function's job to
// diagnose a JSON-shape problem.
func validateDiagramPayload(payload json.RawMessage) error {
	var shape diagramPayloadShape
	if err := json.Unmarshal(payload, &shape); err != nil {
		return nil
	}
	ids := make(map[string]bool, len(shape.Nodes))
	for _, n := range shape.Nodes {
		if n.ID != "" {
			ids[n.ID] = true
		}
	}
	for _, e := range shape.Edges {
		if e.From != "" && !ids[e.From] {
			return fmt.Errorf("edge %q references unknown node id %q (from)", e.ID, e.From)
		}
		if e.To != "" && !ids[e.To] {
			return fmt.Errorf("edge %q references unknown node id %q (to)", e.ID, e.To)
		}
	}
	return nil
}
