package canvasstore

import (
	"encoding/json"
	"testing"
)

func newEmptyCanvas(projectID string) ProjectCanvas {
	return ProjectCanvas{
		ProjectID: projectID,
		Objects:   []CanvasObject{},
		Atoms:     []CanvasAtom{},
	}
}

func TestAddDraftRequiresName(t *testing.T) {
	pc := newEmptyCanvas("/proj")
	if _, err := pc.AddDraft("diagram", "", json.RawMessage(`{}`)); err != ErrNameRequired {
		t.Fatalf("AddDraft with empty name err = %v, want ErrNameRequired", err)
	}
	if len(pc.Objects) != 0 {
		t.Fatalf("Objects = %d, want 0 — a rejected AddDraft must not persist a partial object", len(pc.Objects))
	}
}

func TestAddDraftNotVisibleUntilMaterialized(t *testing.T) {
	pc := newEmptyCanvas("/proj")
	obj, err := pc.AddDraft("diagram", "My Diagram", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if obj.Phase != PhaseDraft {
		t.Fatalf("Phase = %s, want draft", obj.Phase)
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	got := pc.find(obj.ObjectID)
	if got.Phase != PhaseMaterialized {
		t.Fatalf("Phase after Materialize = %s, want materialized", got.Phase)
	}
}

func TestMaterializeRefusesAlreadyMaterializedOrDeleted(t *testing.T) {
	pc := newEmptyCanvas("/proj")
	obj, _ := pc.AddDraft("diagram", "My Diagram", json.RawMessage(`{}`))
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize (first): %v", err)
	}
	if err := pc.Materialize(obj.ObjectID); err == nil {
		t.Fatalf("Materialize (already materialized) = nil, want error")
	}

	deletedObj, _ := pc.AddDraft("diagram", "To Delete", json.RawMessage(`{}`))
	if err := pc.Delete(deletedObj.ObjectID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := pc.Materialize(deletedObj.ObjectID); err == nil {
		t.Fatalf("Materialize (already deleted) = nil, want error")
	}
}

func TestMaterializeRefusesUnknownObject(t *testing.T) {
	pc := newEmptyCanvas("/proj")
	if err := pc.Materialize("object-does-not-exist"); err != ErrObjectNotFound {
		t.Fatalf("Materialize (unknown) err = %v, want ErrObjectNotFound", err)
	}
}

func TestSetActivationOnlyWhenMaterialized(t *testing.T) {
	pc := newEmptyCanvas("/proj")
	obj, _ := pc.AddDraft("diagram", "My Diagram", json.RawMessage(`{}`))
	if err := pc.SetActivation(obj.ObjectID, ActivationActive); err == nil {
		t.Fatalf("SetActivation on a draft = nil, want error")
	}
	if err := pc.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if err := pc.SetActivation(obj.ObjectID, ActivationActive); err != nil {
		t.Fatalf("SetActivation on a materialized object: %v", err)
	}
	if len(pc.ActiveObjectIDs) != 1 || pc.ActiveObjectIDs[0] != obj.ObjectID {
		t.Fatalf("ActiveObjectIDs = %v, want [%s]", pc.ActiveObjectIDs, obj.ObjectID)
	}
	if err := pc.SetActivation(obj.ObjectID, ActivationInactive); err != nil {
		t.Fatalf("SetActivation (deactivate): %v", err)
	}
	if len(pc.ActiveObjectIDs) != 0 {
		t.Fatalf("ActiveObjectIDs after deactivate = %v, want empty", pc.ActiveObjectIDs)
	}
	got := pc.find(obj.ObjectID)
	if got.Phase != PhaseMaterialized {
		t.Fatalf("Deactivating must not touch Phase, got %s", got.Phase)
	}
}

func TestAppendAtomBuildsSupersedesChainNeverMutates(t *testing.T) {
	pc := newEmptyCanvas("/proj")
	obj, _ := pc.AddDraft("diagram", "My Diagram", json.RawMessage(`{}`))
	_ = pc.Materialize(obj.ObjectID)

	first, err := pc.AppendAtom(obj.ObjectID, json.RawMessage(`{"v":1}`))
	if err != nil {
		t.Fatalf("AppendAtom (first): %v", err)
	}
	if first.Supersedes != "" {
		t.Fatalf("first atom Supersedes = %q, want empty", first.Supersedes)
	}
	second, err := pc.AppendAtom(obj.ObjectID, json.RawMessage(`{"v":2}`))
	if err != nil {
		t.Fatalf("AppendAtom (second): %v", err)
	}
	if second.Supersedes != first.AtomID {
		t.Fatalf("second atom Supersedes = %q, want %q", second.Supersedes, first.AtomID)
	}
	if len(pc.Atoms) != 2 {
		t.Fatalf("Atoms = %d, want 2 — the first atom must be retained, not mutated", len(pc.Atoms))
	}
	current, ok := pc.CurrentAtom(obj.ObjectID)
	if !ok || current.AtomID != second.AtomID {
		t.Fatalf("CurrentAtom = %+v (ok=%v), want the second atom", current, ok)
	}
}
