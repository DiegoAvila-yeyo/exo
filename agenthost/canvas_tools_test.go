package agenthost

import (
	"context"
	"encoding/json"
	"strings"
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

func mustEditObjectInput(t *testing.T, objectID string, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"object_id": objectID, "payload": payload})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

// TestCanvasEditObjectVersionsMaterializedObject is the direct regression
// check for QA finding #1: the mini-chat previously had no tool that could
// mutate an already-materialized object, so canvas_edit_object needs to
// version it via AppendAtom's supersedes chain — never mutate in place —
// exactly like the manual-edit HTTP path.
func TestCanvasEditObjectVersionsMaterializedObject(t *testing.T) {
	base, store := newTestCanvasBase(t)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "Auth Flow", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := canvasEditObjectTool{base}
	msg, isErr := tool.Execute(context.Background(), mustEditObjectInput(t, obj.ObjectID, map[string]any{
		"nodes": []map[string]any{{"id": "n1", "label": "User"}},
	}))
	if isErr {
		t.Fatalf("canvas_edit_object failed: %s", msg)
	}

	first, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load (after first edit): %v", err)
	}
	firstAtom, ok := first.CurrentAtom(obj.ObjectID)
	if !ok {
		t.Fatalf("CurrentAtom missing after first edit")
	}
	if firstAtom.Supersedes != "" {
		t.Fatalf("first atom Supersedes = %q, want empty", firstAtom.Supersedes)
	}

	msg, isErr = tool.Execute(context.Background(), mustEditObjectInput(t, obj.ObjectID, map[string]any{
		"nodes": []map[string]any{{"id": "n1", "label": "User (renamed)"}},
	}))
	if isErr {
		t.Fatalf("canvas_edit_object (second edit) failed: %s", msg)
	}

	second, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load (after second edit): %v", err)
	}
	if len(second.Atoms) != 2 {
		t.Fatalf("Atoms after two edits = %d, want 2 (first retained, not mutated)", len(second.Atoms))
	}
	secondAtom, ok := second.CurrentAtom(obj.ObjectID)
	if !ok || secondAtom.Supersedes != firstAtom.AtomID {
		t.Fatalf("second atom Supersedes = %q (ok=%v), want %q", secondAtom.Supersedes, ok, firstAtom.AtomID)
	}
}

func TestCanvasEditObjectRefusesDraftOrDeleted(t *testing.T) {
	base, store := newTestCanvasBase(t)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	draftObj, err := pc.AddDraft("diagram", "Still Draft", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := canvasEditObjectTool{base}
	_, isErr := tool.Execute(context.Background(), mustEditObjectInput(t, draftObj.ObjectID, map[string]any{"nodes": []any{}}))
	if !isErr {
		t.Fatalf("expected error editing a still-draft object")
	}

	_, isErr = tool.Execute(context.Background(), mustEditObjectInput(t, "object-does-not-exist", map[string]any{"nodes": []any{}}))
	if !isErr {
		t.Fatalf("expected error editing an unknown object_id")
	}
}

func TestValidateDiagramPayloadRejectsDanglingEdgeReference(t *testing.T) {
	valid := json.RawMessage(`{"nodes":[{"id":"a"},{"id":"b"}],"edges":[{"id":"e1","from":"a","to":"b"}]}`)
	if err := validateDiagramPayload(valid); err != nil {
		t.Fatalf("validateDiagramPayload(valid) = %v, want nil", err)
	}

	dangling := json.RawMessage(`{"nodes":[{"id":"a"}],"edges":[{"id":"e1","from":"a","to":"does-not-exist"}]}`)
	if err := validateDiagramPayload(dangling); err == nil {
		t.Fatalf("validateDiagramPayload(dangling) = nil, want error")
	}
}

func TestCanvasEditObjectRejectsDanglingDiagramEdge(t *testing.T) {
	base, store := newTestCanvasBase(t)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "Auth Flow", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := canvasEditObjectTool{base}
	_, isErr := tool.Execute(context.Background(), mustEditObjectInput(t, obj.ObjectID, map[string]any{
		"nodes": []map[string]any{{"id": "a"}},
		"edges": []map[string]any{{"id": "e1", "from": "a", "to": "missing"}},
	}))
	if !isErr {
		t.Fatalf("expected error editing a diagram object with a dangling edge reference")
	}
}

// --- canvas_activate_object / canvas_deactivate_object ---
//
// Direct regression coverage for canvas_activation_gap_findings.md: before
// these tools existed, ActiveObjectIDs could never become non-empty, so
// dynamicCentro's anchoring never fired for any object.

func mustObjectIDInput(t *testing.T, objectID string) string {
	t.Helper()
	data, err := json.Marshal(map[string]string{"object_id": objectID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

// materializedTestObject creates a draft, materializes it, and saves — the
// common setup every activation test needs.
func materializedTestObject(t *testing.T, store *canvasstore.Store) canvasstore.CanvasObject {
	t.Helper()
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := pc.AddDraft("diagram", "Auth Flow", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return obj
}

func TestCanvasActivateObjectSucceedsOnMaterialized(t *testing.T) {
	base, store := newTestCanvasBase(t)
	obj := materializedTestObject(t, store)

	tool := canvasActivateObjectTool{base}
	msg, isErr := tool.Execute(context.Background(), mustObjectIDInput(t, obj.ObjectID))
	if isErr {
		t.Fatalf("canvas_activate_object failed: %s", msg)
	}

	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := findObjectByID(pc, obj.ObjectID)
	if found == nil || found.Activation != canvasstore.ActivationActive {
		t.Fatalf("Activation = %v, want active", found)
	}
	if len(pc.ActiveObjectIDs) != 1 || pc.ActiveObjectIDs[0] != obj.ObjectID {
		t.Fatalf("ActiveObjectIDs = %v, want [%s]", pc.ActiveObjectIDs, obj.ObjectID)
	}
}

func TestCanvasActivateObjectFailsOnDraftOrDeleted(t *testing.T) {
	base, store := newTestCanvasBase(t)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	draftObj, err := pc.AddDraft("diagram", "Still Draft", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	deletedObj, err := pc.AddDraft("diagram", "Deleted Later", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := pc.Materialize(deletedObj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if err := pc.Delete(deletedObj.ObjectID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tool := canvasActivateObjectTool{base}
	_, isErr := tool.Execute(context.Background(), mustObjectIDInput(t, draftObj.ObjectID))
	if !isErr {
		t.Fatalf("expected error activating a still-draft object")
	}
	_, isErr = tool.Execute(context.Background(), mustObjectIDInput(t, deletedObj.ObjectID))
	if !isErr {
		t.Fatalf("expected error activating a deleted object")
	}
}

func TestCanvasDeactivateObjectOnAlreadyInactiveIsNoop(t *testing.T) {
	base, store := newTestCanvasBase(t)
	obj := materializedTestObject(t, store)

	tool := canvasDeactivateObjectTool{base}
	msg, isErr := tool.Execute(context.Background(), mustObjectIDInput(t, obj.ObjectID))
	if isErr {
		t.Fatalf("canvas_deactivate_object on already-inactive object failed: %s", msg)
	}

	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := findObjectByID(pc, obj.ObjectID)
	if found == nil || found.Activation == canvasstore.ActivationActive {
		t.Fatalf("Activation = %v, want inactive", found)
	}
	if len(pc.ActiveObjectIDs) != 0 {
		t.Fatalf("ActiveObjectIDs = %v, want empty", pc.ActiveObjectIDs)
	}
}

func TestCanvasActivateThenDeactivateObject(t *testing.T) {
	base, store := newTestCanvasBase(t)
	obj := materializedTestObject(t, store)

	activateTool := canvasActivateObjectTool{base}
	if _, isErr := activateTool.Execute(context.Background(), mustObjectIDInput(t, obj.ObjectID)); isErr {
		t.Fatalf("canvas_activate_object failed")
	}

	deactivateTool := canvasDeactivateObjectTool{base}
	msg, isErr := deactivateTool.Execute(context.Background(), mustObjectIDInput(t, obj.ObjectID))
	if isErr {
		t.Fatalf("canvas_deactivate_object failed: %s", msg)
	}

	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := findObjectByID(pc, obj.ObjectID)
	if found == nil || found.Activation == canvasstore.ActivationActive {
		t.Fatalf("Activation = %v, want inactive after deactivate", found)
	}
	if len(pc.ActiveObjectIDs) != 0 {
		t.Fatalf("ActiveObjectIDs after deactivate = %v, want empty", pc.ActiveObjectIDs)
	}
}

// TestCanvasSaveWithRetryRecoversFromCASConflict exercises the same
// Load-mutate-Save retry loop canvas_activate_object/canvas_deactivate_object
// (and canvas_edit_object) share via canvasToolBase.saveWithRetry: a write
// that lands between this attempt's Load and Save must not be treated as a
// failure — the loop reloads and retries, and the mutation still lands.
func TestCanvasSaveWithRetryRecoversFromCASConflict(t *testing.T) {
	base, store := newTestCanvasBase(t)
	obj := materializedTestObject(t, store)

	attempts := 0
	_, err := base.saveWithRetry(func(pc *canvasstore.ProjectCanvas) error {
		attempts++
		if attempts == 1 {
			// Simulate a concurrent write landing between this attempt's
			// Load and Save: save a no-op change directly through the
			// store so the in-flight pc's Version is now stale.
			racing, loadErr := store.Load("/proj")
			if loadErr != nil {
				t.Fatalf("racing Load: %v", loadErr)
			}
			if _, saveErr := store.Save(racing); saveErr != nil {
				t.Fatalf("racing Save: %v", saveErr)
			}
		}
		return pc.SetActivation(obj.ObjectID, canvasstore.ActivationActive)
	})
	if err != nil {
		t.Fatalf("saveWithRetry did not recover from CAS conflict: %v", err)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want at least 2 (a retry must have happened)", attempts)
	}

	pc, loadErr := store.Load("/proj")
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	found := findObjectByID(pc, obj.ObjectID)
	if found == nil || found.Activation != canvasstore.ActivationActive {
		t.Fatalf("Activation after CAS-conflict retry = %v, want active", found)
	}
}

// --- canvasCell.checkScope — the mini-chat-scoping fix ---
//
// Live testing found the mini-chat could act on a Canvas object other than
// the one its own floating panel was for, whenever more than one object was
// anchored at once (canvas_activation_gap_findings.md /
// canvas_qa_retest_checklist.md's test #1 retest). These confirm the fix:
// once cell.scopedObjectID is set, every canvas_* tool that names or would
// create an object refuses to act on anything but that exact object_id.

func TestCanvasEditObjectRejectsWhenScopedToDifferentObject(t *testing.T) {
	base, store := newTestCanvasBase(t)
	scoped := materializedTestObject(t, store)
	other := materializedTestObject(t, store)
	base.cell.scopedObjectID = scoped.ObjectID

	tool := canvasEditObjectTool{base}
	msg, isErr := tool.Execute(context.Background(), mustEditObjectInput(t, other.ObjectID, map[string]any{
		"nodes": []map[string]any{{"id": "a", "label": "changed"}},
		"edges": []map[string]any{},
	}))
	if !isErr {
		t.Fatalf("expected error editing a different object than the scoped one")
	}
	if !strings.Contains(msg, scoped.ObjectID) {
		t.Fatalf("error message %q should name the scoped object_id %q", msg, scoped.ObjectID)
	}

	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := pc.CurrentAtom(other.ObjectID); ok {
		t.Fatalf("the out-of-scope object must not have been edited")
	}
}

func TestCanvasEditObjectAllowedWhenScopedToSameObject(t *testing.T) {
	base, store := newTestCanvasBase(t)
	obj := materializedTestObject(t, store)
	base.cell.scopedObjectID = obj.ObjectID

	tool := canvasEditObjectTool{base}
	_, isErr := tool.Execute(context.Background(), mustEditObjectInput(t, obj.ObjectID, map[string]any{
		"nodes": []map[string]any{{"id": "a", "label": "changed"}},
		"edges": []map[string]any{},
	}))
	if isErr {
		t.Fatalf("editing the scoped object itself should succeed")
	}
}

func TestCanvasActivateObjectRejectsWhenScopedToDifferentObject(t *testing.T) {
	base, store := newTestCanvasBase(t)
	scoped := materializedTestObject(t, store)
	other := materializedTestObject(t, store)
	base.cell.scopedObjectID = scoped.ObjectID

	tool := canvasActivateObjectTool{base}
	_, isErr := tool.Execute(context.Background(), mustObjectIDInput(t, other.ObjectID))
	if !isErr {
		t.Fatalf("expected error activating a different object than the scoped one")
	}
}

func TestCanvasDeactivateObjectRejectsWhenScopedToDifferentObject(t *testing.T) {
	base, store := newTestCanvasBase(t)
	scoped := materializedTestObject(t, store)
	other := materializedTestObject(t, store)
	base.cell.scopedObjectID = scoped.ObjectID

	tool := canvasDeactivateObjectTool{base}
	_, isErr := tool.Execute(context.Background(), mustObjectIDInput(t, other.ObjectID))
	if !isErr {
		t.Fatalf("expected error deactivating a different object than the scoped one")
	}
}

func TestCanvasCreateDraftRejectsWhenScoped(t *testing.T) {
	base, store := newTestCanvasBase(t)
	scoped := materializedTestObject(t, store)
	base.cell.scopedObjectID = scoped.ObjectID

	tool := canvasCreateDraftTool{base}
	msg, isErr := tool.Execute(context.Background(), mustCreateDraftInput(t, "diagram", "Sneaky New Object"))
	if !isErr {
		t.Fatalf("expected error creating a new draft while scoped to an existing object")
	}
	if !strings.Contains(msg, scoped.ObjectID) {
		t.Fatalf("error message %q should name the scoped object_id %q", msg, scoped.ObjectID)
	}

	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(pc.Objects) != 1 {
		t.Fatalf("Objects = %d, want 1 (no duplicate created)", len(pc.Objects))
	}
}

func TestCanvasMaterializeDraftRejectsWhenScoped(t *testing.T) {
	base, store := newTestCanvasBase(t)
	scoped := materializedTestObject(t, store)
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	draft, err := pc.AddDraft("diagram", "Some Draft", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	base.cell.scopedObjectID = scoped.ObjectID

	tool := canvasMaterializeDraftTool{base}
	_, isErr := tool.Execute(context.Background(), mustMaterializeInput(t, draft.ObjectID))
	if !isErr {
		t.Fatalf("expected error materializing a different object while scoped")
	}
}
