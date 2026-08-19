package canvasstore

import (
	"encoding/json"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestLoadAutoCreatesEmptyCanvas(t *testing.T) {
	s := newTestStore(t)
	pc, err := s.Load("/Users/yeyo/some-project")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pc.ProjectID != "/Users/yeyo/some-project" {
		t.Fatalf("ProjectID = %q, want the requested project id", pc.ProjectID)
	}
	if pc.Version != 0 {
		t.Fatalf("Version = %d, want 0 for an unsaved auto-created canvas", pc.Version)
	}
	if pc.Objects == nil || pc.Atoms == nil {
		t.Fatalf("Objects/Atoms should be initialized to empty slices, not nil")
	}
}

func TestSaveRejectsStaleVersion(t *testing.T) {
	s := newTestStore(t)
	projectID := "/Users/yeyo/some-project"

	first, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load (first): %v", err)
	}
	if _, err := first.AddDraft("diagram", "Draft A", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	saved, err := s.Save(first)
	if err != nil {
		t.Fatalf("Save (first): %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("Version after first save = %d, want 1", saved.Version)
	}

	// Simulate a second, stale writer that Loaded before `first` was saved.
	stale, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load (stale, before rewind): %v", err)
	}
	stale.Version = 0 // pretend this copy was loaded before the first Save landed
	if _, err := stale.AddDraft("diagram", "Draft B", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if _, err := s.Save(stale); err != ErrStaleVersion {
		t.Fatalf("Save (stale) err = %v, want ErrStaleVersion", err)
	}

	// Retry: reload fresh, reapply, should now succeed.
	fresh, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load (retry): %v", err)
	}
	if fresh.Version != 1 {
		t.Fatalf("Version on reload = %d, want 1", fresh.Version)
	}
	if _, err := fresh.AddDraft("diagram", "Draft B", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AddDraft (retry): %v", err)
	}
	retried, err := s.Save(fresh)
	if err != nil {
		t.Fatalf("Save (retry): %v", err)
	}
	if retried.Version != 2 {
		t.Fatalf("Version after retry = %d, want 2", retried.Version)
	}
	if len(retried.Objects) != 2 {
		t.Fatalf("Objects after retry = %d, want 2 (Draft A + Draft B)", len(retried.Objects))
	}
}

// TestConcurrentEditsOneBouncesOneSucceeds is the direct acceptance-criteria
// check: a manual edit and a simulated concurrent tool-driven edit on the
// same object, where one Save bounces on stale Version and a retry after
// re-Load succeeds.
func TestConcurrentEditsOneBouncesOneSucceeds(t *testing.T) {
	s := newTestStore(t)
	projectID := "/Users/yeyo/some-project"

	base, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	obj, err := base.AddDraft("diagram", "Shared Draft", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AddDraft: %v", err)
	}
	if err := base.Materialize(obj.ObjectID); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	base, err = s.Save(base)
	if err != nil {
		t.Fatalf("Save (base): %v", err)
	}

	// Two independent writers ("manual edit" and "tool-driven edit") both
	// Load the same version, then both try to append an atom.
	manualEdit, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load (manualEdit): %v", err)
	}
	toolEdit, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load (toolEdit): %v", err)
	}

	if _, err := manualEdit.AppendAtom(obj.ObjectID, json.RawMessage(`{"nodes":["from manual edit"]}`)); err != nil {
		t.Fatalf("AppendAtom (manualEdit): %v", err)
	}
	if _, err := s.Save(manualEdit); err != nil {
		t.Fatalf("Save (manualEdit, should be first writer): %v", err)
	}

	if _, err := toolEdit.AppendAtom(obj.ObjectID, json.RawMessage(`{"nodes":["from tool edit"]}`)); err != nil {
		t.Fatalf("AppendAtom (toolEdit): %v", err)
	}
	if _, err := s.Save(toolEdit); err != ErrStaleVersion {
		t.Fatalf("Save (toolEdit) err = %v, want ErrStaleVersion", err)
	}

	// Retry: reload fresh (sees manualEdit's atom), reapply the tool's edit
	// on top, should now succeed and both atoms should be traceable via the
	// supersedes chain — nothing overwritten in place.
	fresh, err := s.Load(projectID)
	if err != nil {
		t.Fatalf("Load (retry): %v", err)
	}
	manualAtom, ok := fresh.CurrentAtom(obj.ObjectID)
	if !ok || !jsonEqual(t, manualAtom.Body, `{"nodes":["from manual edit"]}`) {
		t.Fatalf("CurrentAtom after manual edit = %+v, ok=%v", manualAtom, ok)
	}
	toolAtom, err := fresh.AppendAtom(obj.ObjectID, json.RawMessage(`{"nodes":["from tool edit"]}`))
	if err != nil {
		t.Fatalf("AppendAtom (retry): %v", err)
	}
	if toolAtom.Supersedes != manualAtom.AtomID {
		t.Fatalf("retry atom Supersedes = %q, want %q (the manual edit's atom)", toolAtom.Supersedes, manualAtom.AtomID)
	}
	retried, err := s.Save(fresh)
	if err != nil {
		t.Fatalf("Save (retry): %v", err)
	}
	if len(retried.Atoms) != 2 {
		t.Fatalf("Atoms after retry = %d, want 2 (old atom retained, not mutated)", len(retried.Atoms))
	}
	current, ok := retried.CurrentAtom(obj.ObjectID)
	if !ok || !jsonEqual(t, current.Body, `{"nodes":["from tool edit"]}`) {
		t.Fatalf("CurrentAtom after retry = %+v, ok=%v, want the tool edit's atom", current, ok)
	}
}

// jsonEqual compares body against want by JSON structural equality, not
// byte-exact string — Save round-trips atom bodies through
// json.MarshalIndent, which reformats whitespace.
func jsonEqual(t *testing.T, body json.RawMessage, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("jsonEqual: unmarshal body: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("jsonEqual: unmarshal want: %v", err)
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
