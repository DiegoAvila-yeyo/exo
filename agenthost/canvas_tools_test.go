package agenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
)

func newTestCanvasBase(t *testing.T) (canvasToolBase, *canvasstore.Store) {
	t.Helper()
	store, err := canvasstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("canvasstore.New: %v", err)
	}
	cell := &canvasCell{projectID: "/proj", humanMessage: ""}
	return canvasToolBase{store: store, cell: cell}, store
}

func mustCreateDraftInput(t *testing.T, objType, name string) string {
	t.Helper()
	data, err := json.Marshal(map[string]string{"type": objType, "name": name})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func mustMaterializeInput(t *testing.T, objectID string) string {
	t.Helper()
	data, err := json.Marshal(map[string]string{"draft_object_id": objectID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func TestCanvasCreateDraftThenMaterializeSucceeds(t *testing.T) {
	base, store := newTestCanvasBase(t)
	base.cell.humanMessage = "let's sketch a diagram called Auth Flow"

	createTool := canvasCreateDraftTool{base}
	msg, isErr := createTool.Execute(context.Background(), mustCreateDraftInput(t, "diagram", "Auth Flow"))
	if isErr {
		t.Fatalf("canvas_create_draft failed: %s", msg)
	}

	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(pc.Objects) != 1 {
		t.Fatalf("Objects = %d, want 1", len(pc.Objects))
	}
	draft := pc.Objects[0]
	if draft.Phase != canvasstore.PhaseDraft {
		t.Fatalf("Phase = %s, want draft", draft.Phase)
	}

	materializeTool := canvasMaterializeDraftTool{base}
	msg, isErr = materializeTool.Execute(context.Background(), mustMaterializeInput(t, draft.ObjectID))
	if isErr {
		t.Fatalf("canvas_materialize_draft failed: %s", msg)
	}

	pc, err = store.Load("/proj")
	if err != nil {
		t.Fatalf("Load (after materialize): %v", err)
	}
	if pc.Objects[0].Phase != canvasstore.PhaseMaterialized {
		t.Fatalf("Phase after materialize = %s, want materialized", pc.Objects[0].Phase)
	}
}

func TestCanvasMaterializeDraftRefusesUnknownObjectID(t *testing.T) {
	base, _ := newTestCanvasBase(t)
	tool := canvasMaterializeDraftTool{base}
	_, isErr := tool.Execute(context.Background(), mustMaterializeInput(t, "object-does-not-exist"))
	if !isErr {
		t.Fatalf("expected error materializing an unknown object_id")
	}
}

func TestCanvasMaterializeDraftRefusesAlreadyMaterializedOrDeleted(t *testing.T) {
	base, store := newTestCanvasBase(t)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "Once", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize (seed): %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := canvasMaterializeDraftTool{base}
	_, isErr := tool.Execute(context.Background(), mustMaterializeInput(t, obj.ObjectID))
	if !isErr {
		t.Fatalf("expected error materializing an already-materialized object")
	}
}

// TestFindDraftByNameFailsClearlyOnAmbiguousOrUnnamedTarget is the direct
// acceptance-criteria check for canvas_materialize_draft's disambiguation:
// zero or multiple same-named drafts must both be hard errors, never a
// guess.
func TestFindDraftByNameFailsClearlyOnAmbiguousOrUnnamedTarget(t *testing.T) {
	pc := canvasstore.ProjectCanvas{ProjectID: "/proj"}
	if _, err := pc.AddDraft("diagram", "Shared Name", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AddDraft (1): %v", err)
	}
	if _, err := pc.AddDraft("diagram", "Shared Name", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AddDraft (2): %v", err)
	}

	if _, err := findDraftByName(pc, "Shared Name"); err == nil {
		t.Fatalf("findDraftByName with two same-named drafts = nil error, want ambiguous error")
	}
	if _, err := findDraftByName(pc, "No Such Draft"); err == nil {
		t.Fatalf("findDraftByName with zero matches = nil error, want not-found error")
	}

	unique, err := pc.AddDraft("diagram", "Unique Name", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft (unique): %v", err)
	}
	got, err := findDraftByName(pc, "unique name") // case-insensitive
	if err != nil {
		t.Fatalf("findDraftByName (unique, unambiguous): %v", err)
	}
	if got.ObjectID != unique.ObjectID {
		t.Fatalf("findDraftByName returned %q, want %q", got.ObjectID, unique.ObjectID)
	}
}

func TestCanvasListDraftsOnlyListsDrafts(t *testing.T) {
	base, store := newTestCanvasBase(t)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := pc.AddDraft("diagram", "Still Draft", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AddDraft (1): %v", err)
	}
	materialized, err := pc.AddDraft("diagram", "Already Materialized", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft (2): %v", err)
	}
	if err := pc.Materialize(materialized.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := canvasListDraftsTool{base}
	out, isErr := tool.Execute(context.Background(), "{}")
	if isErr {
		t.Fatalf("canvas_list_drafts failed: %s", out)
	}
	var drafts []map[string]string
	if err := json.Unmarshal([]byte(out), &drafts); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(drafts) != 1 || drafts[0]["name"] != "Still Draft" {
		t.Fatalf("canvas_list_drafts output = %+v, want only the still-draft object", drafts)
	}
}
