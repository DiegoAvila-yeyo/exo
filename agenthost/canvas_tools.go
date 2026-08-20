package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// maxCanvasSaveRetries bounds the Load-mutate-Save retry loop every canvas
// tool uses on canvasstore.ErrStaleVersion — see canvasstore/store.go's CAS
// doc comment. A handful of retries is enough to win a race against another
// write from the same process; if it's still losing after this many
// attempts something is wrong beyond ordinary contention, so the tool fails
// clearly instead of retrying forever.
const maxCanvasSaveRetries = 3

// canvasToolBase is embedded by every canvas_* tool. Mirrors
// planningToolBase (planning_tools.go): reload fresh from canvasstore on
// every Execute, never cache a *ProjectCanvas across turns, and centralize
// the "canvas is not configured" error.
type canvasToolBase struct {
	store *canvasstore.Store
	cell  *canvasCell
}

func (b canvasToolBase) currentCanvas() (canvasstore.ProjectCanvas, string, bool) {
	if b.store == nil {
		return canvasstore.ProjectCanvas{}, "canvas is not configured", false
	}
	if b.cell.projectID == "" {
		return canvasstore.ProjectCanvas{}, "no project is open — the canvas only works while the user has a project selected", false
	}
	pc, err := b.store.Load(b.cell.projectID)
	if err != nil {
		return canvasstore.ProjectCanvas{}, fmt.Sprintf("canvas unavailable: %v", err), false
	}
	return pc, "", true
}

// saveWithRetry applies mutate to a freshly-loaded ProjectCanvas and saves
// it, retrying the whole Load-mutate-Save cycle when Save reports
// ErrStaleVersion (another write landed in between). mutate must be free of
// side effects beyond the ProjectCanvas it's given, since it may run more
// than once.
func (b canvasToolBase) saveWithRetry(mutate func(*canvasstore.ProjectCanvas) error) (canvasstore.ProjectCanvas, error) {
	var last error
	for attempt := 0; attempt < maxCanvasSaveRetries; attempt++ {
		pc, err := b.store.Load(b.cell.projectID)
		if err != nil {
			return canvasstore.ProjectCanvas{}, err
		}
		if err := mutate(&pc); err != nil {
			return canvasstore.ProjectCanvas{}, err
		}
		saved, err := b.store.Save(pc)
		if err == nil {
			return saved, nil
		}
		if err != canvasstore.ErrStaleVersion {
			return canvasstore.ProjectCanvas{}, err
		}
		last = err
	}
	return canvasstore.ProjectCanvas{}, fmt.Errorf("canvas: gave up after %d retries: %w", maxCanvasSaveRetries, last)
}

// findDraftByName does the same exact, case-insensitive (whitespace-
// trimmed) lookup as findPlanningByName/findBoardByName
// (planning_navigate_tools.go) — never fuzzy. Zero or more-than-one
// matching drafts are both errors; the caller never guesses which one was
// meant.
func findDraftByName(pc canvasstore.ProjectCanvas, name string) (canvasstore.CanvasObject, error) {
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
		return canvasstore.CanvasObject{}, fmt.Errorf("more than one draft named %q — ambiguous", name)
	}
}

// --- canvas_list_drafts ---
//
// Read-only, always safe — this is what lets the model (or the
// /materialize slash command) enumerate drafts and their names/IDs before
// calling canvas_materialize_draft, so "ask which draft" is possible
// without ever guessing.

type canvasListDraftsTool struct{ canvasToolBase }

func (t canvasListDraftsTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "canvas_list_drafts",
		Description: "List the current project's draft Canvas objects — created but not yet " +
			"materialized (not visible on the Canvas). Use this to see what drafts exist and their " +
			"exact names before calling canvas_materialize_draft, especially when more than one draft " +
			"might match what the human is asking for.",
		InputSchema: map[string]any{},
	}
}

func (t canvasListDraftsTool) Execute(_ context.Context, _ string) (string, bool) {
	pc, errMsg, ok := t.currentCanvas()
	if !ok {
		return errMsg, true
	}
	var drafts []map[string]string
	for _, obj := range pc.Objects {
		if obj.Phase != canvasstore.PhaseDraft {
			continue
		}
		drafts = append(drafts, map[string]string{"object_id": obj.ObjectID, "name": obj.Name, "type": obj.Type})
	}
	out, err := json.Marshal(drafts)
	if err != nil {
		return fmt.Sprintf("failed to list drafts: %v", err), true
	}
	return string(out), false
}

// --- canvas_create_draft ---
//
// Drafts need an explicit, deterministic creation path the same way every
// other Canvas-affecting write in this codebase goes through an explicit
// tool (planning_create_board, planning_create_note) — the model doesn't
// silently mutate the store. This is deliberately separate from
// canvas_materialize_draft: creation and materialization are separate
// moments by design, mirroring the human's own two-phase mental model
// (discuss, then commit) — canvas_materialize_draft cannot create a draft
// and materialize it in one call.

type canvasCreateDraftTool struct{ canvasToolBase }

func (t canvasCreateDraftTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "canvas_create_draft",
		Description: "Create a new draft Canvas object — not yet visible on the Canvas. Call this " +
			"the moment you can coherently describe a candidate object (e.g. 'we're defining a diagram " +
			"X about Y'), not on every diagram-adjacent remark. Every draft must be named; the name " +
			"should reflect what the human and you have actually been discussing, not a placeholder. " +
			"The draft stays invisible until canvas_materialize_draft is called on it (by name, via " +
			"natural language, the /materialize slash command, or a UI button) — this tool never " +
			"materializes on its own.",
		InputSchema: map[string]any{
			"type": map[string]any{
				"type":        "string",
				"description": "Object type, e.g. \"diagram\".",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Name for the draft — required.",
			},
			"payload": map[string]any{
				"type":        "object",
				"description": "Optional partial payload for the object type (e.g. a diagram's nodes/edges so far).",
			},
		},
		Required: []string{"type", "name"},
	}
}

func (t canvasCreateDraftTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		Type    string          `json:"type"`
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	objType := strings.TrimSpace(in.Type)
	name := strings.TrimSpace(in.Name)
	if objType == "" {
		return "\"type\" is required", true
	}
	if name == "" {
		return "\"name\" is required", true
	}
	if t.store == nil {
		return "canvas is not configured", true
	}
	if t.cell.projectID == "" {
		return "no project is open — the canvas only works while the user has a project selected", true
	}
	if err := t.cell.checkScope(""); err != nil {
		return err.Error(), true
	}

	var created canvasstore.CanvasObject
	_, err := t.saveWithRetry(func(pc *canvasstore.ProjectCanvas) error {
		obj, err := pc.AddDraft(objType, name, in.Payload)
		if err != nil {
			return err
		}
		created = obj
		return nil
	})
	if err != nil {
		return fmt.Sprintf("failed to create draft: %v", err), true
	}
	return fmt.Sprintf("created draft %q (object_id %s) — not visible on the Canvas until materialized", created.Name, created.ObjectID), false
}

// --- canvas_materialize_draft ---

type canvasMaterializeDraftTool struct{ canvasToolBase }

func (t canvasMaterializeDraftTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "canvas_materialize_draft",
		Description: "Flip an already-existing draft Canvas object to materialized, making it " +
			"visible on the Canvas. Only operates on a draft_object_id from canvas_list_drafts or a " +
			"prior canvas_create_draft call — it never creates a draft itself. If more than one draft " +
			"could plausibly match what the human asked for, call canvas_list_drafts first and, if " +
			"still ambiguous, ask the human which one in plain text instead of guessing — never call " +
			"this tool with a guessed draft_object_id.",
		InputSchema: map[string]any{
			"draft_object_id": map[string]any{
				"type":        "string",
				"description": "The exact object_id of the draft to materialize.",
			},
		},
		Required: []string{"draft_object_id"},
	}
}

func (t canvasMaterializeDraftTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		DraftObjectID string `json:"draft_object_id"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	objectID := strings.TrimSpace(in.DraftObjectID)
	if objectID == "" {
		return "\"draft_object_id\" is required", true
	}
	if t.store == nil {
		return "canvas is not configured", true
	}
	if t.cell.projectID == "" {
		return "no project is open — the canvas only works while the user has a project selected", true
	}
	if err := t.cell.checkScope(""); err != nil {
		return err.Error(), true
	}

	var materialized canvasstore.CanvasObject
	_, err := t.saveWithRetry(func(pc *canvasstore.ProjectCanvas) error {
		obj := findObjectByID(*pc, objectID)
		if obj == nil {
			return fmt.Errorf("no draft with object_id %q", objectID)
		}
		if obj.Phase != canvasstore.PhaseDraft {
			return fmt.Errorf("object %q is not a draft (phase: %s) — refusing to materialize", objectID, obj.Phase)
		}
		if err := pc.Materialize(objectID); err != nil {
			return err
		}
		materialized = *findObjectByID(*pc, objectID)
		return nil
	})
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("materialized %q — now visible on the Canvas", materialized.Name), false
}

func findObjectByID(pc canvasstore.ProjectCanvas, objectID string) *canvasstore.CanvasObject {
	for i := range pc.Objects {
		if pc.Objects[i].ObjectID == objectID {
			return &pc.Objects[i]
		}
	}
	return nil
}

// --- canvas_edit_object ---
//
// The floating panel's mini-chat is supposed to let the AI edit an
// already-materialized object directly, versioned via supersedes — before
// this tool existed, canvas_create_draft/canvas_materialize_draft left no
// way to mutate a materialized object's content at all, so the mini-chat
// could only ever propose starting a brand new draft. This closes that gap
// by going through the exact same write path (AppendAtom, never mutating
// an existing atom) that the manual-edit HTTP PATCH endpoint uses
// (termserver/canvas.go's handleCanvasObjectPatch) — one write path for
// both edit surfaces, per the build spec.

type canvasEditObjectTool struct{ canvasToolBase }

func (t canvasEditObjectTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "canvas_edit_object",
		Description: "Edit an already-materialized Canvas object's content — versions it as a new " +
			"atom (the previous version is retained, never mutated), exactly like a human's manual " +
			"edit through the floating panel. Only works on an object whose phase is already " +
			"\"materialized\" (use canvas_materialize_draft first if it's still a draft). payload " +
			"replaces the object's current content in full — read the object's current payload from " +
			"the conversation context (materialized objects that are active are already in your " +
			"system prompt) before editing, so you don't drop fields the human didn't ask you to " +
			"change.",
		InputSchema: map[string]any{
			"object_id": map[string]any{
				"type":        "string",
				"description": "The exact object_id of the materialized object to edit.",
			},
			"payload": map[string]any{
				"type":        "object",
				"description": "The full replacement payload for this object (e.g. a diagram's complete nodes/edges/groups/layout).",
			},
		},
		Required: []string{"object_id", "payload"},
	}
}

func (t canvasEditObjectTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		ObjectID string          `json:"object_id"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	objectID := strings.TrimSpace(in.ObjectID)
	if objectID == "" {
		return "\"object_id\" is required", true
	}
	if len(in.Payload) == 0 {
		return "\"payload\" is required", true
	}
	if t.store == nil {
		return "canvas is not configured", true
	}
	if t.cell.projectID == "" {
		return "no project is open — the canvas only works while the user has a project selected", true
	}
	if err := t.cell.checkScope(objectID); err != nil {
		return err.Error(), true
	}

	var edited canvasstore.CanvasObject
	_, err := t.saveWithRetry(func(pc *canvasstore.ProjectCanvas) error {
		obj := findObjectByID(*pc, objectID)
		if obj == nil {
			return fmt.Errorf("no object with object_id %q", objectID)
		}
		if obj.Phase != canvasstore.PhaseMaterialized {
			return fmt.Errorf("object %q is not materialized (phase: %s) — canvas_edit_object only works on materialized objects", objectID, obj.Phase)
		}
		if obj.Type == "diagram" {
			if err := validateDiagramPayload(in.Payload); err != nil {
				return fmt.Errorf("invalid diagram payload: %w", err)
			}
		}
		if _, err := pc.AppendAtom(objectID, in.Payload); err != nil {
			return err
		}
		edited = *findObjectByID(*pc, objectID)
		return nil
	})
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("edited %q — new version saved", edited.Name), false
}

// --- canvas_activate_object / canvas_deactivate_object ---
//
// The one missing trigger for the anchoring mechanism Round 2/4 already
// designed: dynamicCentro (canvas_centro.go) already injects every active
// object's current atom into the system prompt every turn, and
// canvasstore.SetActivation already validates and maintains ActiveObjectIDs
// — but nothing ever called it, so ActiveObjectIDs stayed empty forever
// (see canvas_activation_gap_findings.md). These two tools are the only
// piece that was missing, reusing SetActivation exactly as-is.
//
// Deliberately two narrow tools, not one with a boolean flag: a misfire on
// a wrong tool name is auditable, a misfire on a flipped flag isn't — same
// reasoning already applied to planning_open vs. the create-tools.
//
// Activation is a deliberate, explicit action — by the human directly (the
// floating panel's toggle) or by the agent acting on an explicit human
// instruction ("ancla esto", "ya no lo necesito presente"). Never an
// inference from context, never a side effect of another tool — that
// discipline is why canvas_materialize_draft does not auto-activate.

type canvasActivateObjectTool struct{ canvasToolBase }

func (t canvasActivateObjectTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "canvas_activate_object",
		Description: "Anchor a materialized Canvas object: its current content is injected into " +
			"your system prompt every turn from now on, for every conversation in this project, until " +
			"deactivated. Only call this when the human explicitly asks you to anchor/keep-present an " +
			"object (e.g. \"ancla esto\", \"ten presente este diagrama\") — never as a helpful inference " +
			"from context, since active objects are the most expensive thing in the whole design (full " +
			"body injected unconditionally every turn). Only works on an object whose phase is already " +
			"\"materialized\".",
		InputSchema: map[string]any{
			"object_id": map[string]any{
				"type":        "string",
				"description": "The exact object_id of the materialized object to activate.",
			},
		},
		Required: []string{"object_id"},
	}
}

func (t canvasActivateObjectTool) Execute(_ context.Context, rawInput string) (string, bool) {
	objectID, errMsg, ok := parseObjectIDInput(rawInput)
	if !ok {
		return errMsg, true
	}
	if t.store == nil {
		return "canvas is not configured", true
	}
	if t.cell.projectID == "" {
		return "no project is open — the canvas only works while the user has a project selected", true
	}
	if err := t.cell.checkScope(objectID); err != nil {
		return err.Error(), true
	}

	var activated canvasstore.CanvasObject
	_, err := t.saveWithRetry(func(pc *canvasstore.ProjectCanvas) error {
		if err := pc.SetActivation(objectID, canvasstore.ActivationActive); err != nil {
			return err
		}
		activated = *findObjectByID(*pc, objectID)
		return nil
	})
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("%q is now active — I'll keep it in mind this session", activated.Name), false
}

type canvasDeactivateObjectTool struct{ canvasToolBase }

func (t canvasDeactivateObjectTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "canvas_deactivate_object",
		Description: "Un-anchor a materialized Canvas object: it stops being injected into your " +
			"system prompt. Only call this when the human explicitly asks you to stop keeping an object " +
			"present (e.g. \"ya no lo necesito anclado\", \"olvídate de ese diagrama por ahora\") — never " +
			"as a helpful inference from context.",
		InputSchema: map[string]any{
			"object_id": map[string]any{
				"type":        "string",
				"description": "The exact object_id of the object to deactivate.",
			},
		},
		Required: []string{"object_id"},
	}
}

func (t canvasDeactivateObjectTool) Execute(_ context.Context, rawInput string) (string, bool) {
	objectID, errMsg, ok := parseObjectIDInput(rawInput)
	if !ok {
		return errMsg, true
	}
	if t.store == nil {
		return "canvas is not configured", true
	}
	if t.cell.projectID == "" {
		return "no project is open — the canvas only works while the user has a project selected", true
	}
	if err := t.cell.checkScope(objectID); err != nil {
		return err.Error(), true
	}

	var deactivated canvasstore.CanvasObject
	_, err := t.saveWithRetry(func(pc *canvasstore.ProjectCanvas) error {
		if err := pc.SetActivation(objectID, canvasstore.ActivationInactive); err != nil {
			return err
		}
		deactivated = *findObjectByID(*pc, objectID)
		return nil
	})
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("%q is no longer anchored", deactivated.Name), false
}

// parseObjectIDInput does the common {"object_id": "..."} decode+trim+
// required-check shared by canvas_activate_object/canvas_deactivate_object.
func parseObjectIDInput(rawInput string) (objectID, errMsg string, ok bool) {
	var in struct {
		ObjectID string `json:"object_id"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return "", fmt.Sprintf("invalid tool input: %v", err), false
	}
	objectID = strings.TrimSpace(in.ObjectID)
	if objectID == "" {
		return "", "\"object_id\" is required", false
	}
	return objectID, "", true
}

// diagramNodeShape/diagramEdgeShape/diagramPayloadShape are the minimal
// subset of the §6 diagram payload schema needed to validate edge
// references — not a full schema, just enough to catch a dangling
// edge.from/edge.to before it silently disappears from the render (the
// frontend's renderDiagramStage no-ops on an edge whose endpoints don't
// resolve, per app.js).
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
// even parse as {nodes, edges} is left alone here — that's a JSON-shape
// problem the caller's own JSON validation already would have caught
// earlier in the request, not this function's job to diagnose.
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
