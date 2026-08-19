package agenthost

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
)

func TestDynamicCentroFailsOpenWhenNotConfigured(t *testing.T) {
	if got := dynamicCentro(nil, "/proj"); got != "" {
		t.Fatalf("dynamicCentro(nil store) = %q, want empty", got)
	}
	store, err := canvasstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("canvasstore.New: %v", err)
	}
	if got := dynamicCentro(store, ""); got != "" {
		t.Fatalf("dynamicCentro(empty projectID) = %q, want empty", got)
	}
}

func TestDynamicCentroInjectsOnlyActiveMaterializedObjects(t *testing.T) {
	store, err := canvasstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("canvasstore.New: %v", err)
	}
	pc, err := store.Load("/proj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Object A: draft — must never be injected.
	draftObj, err := pc.AddDraft("diagram", "Still Draft", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft (draft): %v", err)
	}

	// Object B: materialized but inactive — must not be injected.
	inactiveObj, err := pc.AddDraft("diagram", "Materialized Inactive", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft (inactive): %v", err)
	}
	if err := pc.Materialize(inactiveObj.ObjectID); err != nil {
		t.Fatalf("Materialize (inactive): %v", err)
	}
	if _, err := pc.AppendAtom(inactiveObj.ObjectID, json.RawMessage(`{"nodes":["inactive body"]}`)); err != nil {
		t.Fatalf("AppendAtom (inactive): %v", err)
	}

	// Object C: materialized and active — must be injected, with its
	// current (latest) atom body.
	activeObj, err := pc.AddDraft("diagram", "Active Object", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft (active): %v", err)
	}
	if err := pc.Materialize(activeObj.ObjectID); err != nil {
		t.Fatalf("Materialize (active): %v", err)
	}
	if _, err := pc.AppendAtom(activeObj.ObjectID, json.RawMessage(`{"nodes":["old body"]}`)); err != nil {
		t.Fatalf("AppendAtom (active, first): %v", err)
	}
	if _, err := pc.AppendAtom(activeObj.ObjectID, json.RawMessage(`{"nodes":["current body"]}`)); err != nil {
		t.Fatalf("AppendAtom (active, second): %v", err)
	}
	if err := pc.SetActivation(activeObj.ObjectID, canvasstore.ActivationActive); err != nil {
		t.Fatalf("SetActivation: %v", err)
	}

	if _, err := store.Save(pc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	block := dynamicCentro(store, "/proj")

	if strings.Contains(block, draftObj.Name) {
		t.Fatalf("dynamicCentro injected a draft object: %q", block)
	}
	if strings.Contains(block, inactiveObj.Name) {
		t.Fatalf("dynamicCentro injected an inactive materialized object: %q", block)
	}
	if !strings.Contains(block, activeObj.Name) {
		t.Fatalf("dynamicCentro did not inject the active materialized object: %q", block)
	}
	if !strings.Contains(block, "current body") {
		t.Fatalf("dynamicCentro did not inject the current atom body: %q", block)
	}
	if strings.Contains(block, "old body") {
		t.Fatalf("dynamicCentro injected a superseded atom body: %q", block)
	}
}
