package planningstore

import (
	"path/filepath"
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

func TestCreateLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	created, err := s.Create("Exo Workspace")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Exo Workspace" || len(created.Boards) != 0 {
		t.Fatalf("unexpected new Planning: %+v", created)
	}

	loaded, err := s.Load(created.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != created.ID || loaded.Name != created.Name {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", loaded, created)
	}
}

func TestLoadNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Load("does-not-exist"); err != ErrNotFound {
		t.Fatalf("Load: got err %v, want ErrNotFound", err)
	}
}

func TestListOrdersByUpdatedAtDesc(t *testing.T) {
	s := newTestStore(t)
	first, _ := s.Create("First")
	second, _ := s.Create("Second")

	// Touch "first" so it becomes the most recently updated.
	first.Name = "First (renamed)"
	if err := s.Save(first); err != nil {
		t.Fatalf("Save: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List: got %d entries, want 2", len(list))
	}
	if list[0].ID != first.ID {
		t.Fatalf("List: got most-recent %q, want %q (second=%q)", list[0].ID, first.ID, second.ID)
	}
}

// TestSupersedeNeverDeletes is the manifesto's "nada se pierde, todo
// evoluciona" rule in code: superseding a Decision must not remove it, only
// flag it and point forward.
func TestSupersedeNeverDeletes(t *testing.T) {
	var p Planning
	old := p.AddKnowledge(Knowledge{Type: KnowledgeDecision, Title: "DeepSeek por defecto"})
	next := p.AddKnowledge(Knowledge{Type: KnowledgeDecision, Title: "GPT-6 como planner"})

	if ok := p.Supersede(old.ID, next.ID); !ok {
		t.Fatalf("Supersede returned false")
	}

	if len(p.Knowledge) != 2 {
		t.Fatalf("Knowledge count changed: got %d, want 2 (superseding must not delete)", len(p.Knowledge))
	}
	got := p.find(old.ID)
	if got.Status != StatusSuperseded || got.SupersededBy != next.ID {
		t.Fatalf("old entry not superseded correctly: %+v", got)
	}
}

// TestQuestionResolvesToDecision covers the Question→Decision link.
func TestQuestionResolvesToDecision(t *testing.T) {
	var p Planning
	q := p.AddKnowledge(Knowledge{Type: KnowledgeQuestion, Title: "¿Usamos PostgreSQL?"})
	if q.Status != StatusOpen {
		t.Fatalf("new Question status = %q, want %q", q.Status, StatusOpen)
	}
	d := p.AddKnowledge(Knowledge{Type: KnowledgeDecision, Title: "Usaremos PostgreSQL"})

	if ok := p.ResolveQuestion(q.ID, d.ID); !ok {
		t.Fatalf("ResolveQuestion returned false")
	}
	got := p.find(q.ID)
	if got.ResolvedBy != d.ID || got.Status != StatusAccepted {
		t.Fatalf("question not resolved correctly: %+v", got)
	}
}

// TestDerivedFromSeparatesRawAndDistilled: a raw Research entry stays raw;
// a Decision distilled from it links back via DerivedFrom, per "lo crudo y
// lo concluido no se mezclan".
func TestDerivedFromSeparatesRawAndDistilled(t *testing.T) {
	var p Planning
	raw := p.AddKnowledge(Knowledge{Type: KnowledgeResearch, Title: "Sesión de diseño Planning"})
	distilled := p.AddKnowledge(Knowledge{Type: KnowledgeDecision, Title: "Planning → N Projects", DerivedFrom: raw.ID})

	if distilled.DerivedFrom != raw.ID {
		t.Fatalf("DerivedFrom not set: %+v", distilled)
	}
	if p.find(raw.ID).Type != KnowledgeResearch {
		t.Fatalf("raw entry got reclassified, want it to stay Research")
	}
}

// TestForBoardContextualDisclosure: a board only surfaces Knowledge placed
// on it, and a single Decision can surface on more than one board.
func TestForBoardContextualDisclosure(t *testing.T) {
	var p Planning
	auth := p.AddBoard("Auth")
	runtime := p.AddBoard("Runtime")

	shared := p.AddKnowledge(Knowledge{Type: KnowledgePrinciple, Title: "El humano siempre dirige", BoardID: auth.ID})
	_ = p.AddKnowledge(Knowledge{Type: KnowledgeNote, Title: "nota suelta en runtime", BoardID: runtime.ID})

	onAuth := p.ForBoard(auth.ID)
	if len(onAuth) != 1 || onAuth[0].ID != shared.ID {
		t.Fatalf("ForBoard(auth) = %+v, want just %+v", onAuth, shared)
	}
	onRuntime := p.ForBoard(runtime.ID)
	if len(onRuntime) != 1 {
		t.Fatalf("ForBoard(runtime) = %+v, want 1 entry", onRuntime)
	}
}

// TestScopeForNarrowsWhenSet covers manifesto rule 2: Projects only ever
// read a scope of the Planning, never the whole thing unless the link says
// so explicitly.
func TestScopeForNarrowsWhenSet(t *testing.T) {
	var p Planning
	vision := p.AddBoard("Vision")
	_ = p.AddBoard("Pricing")
	decision := p.AddKnowledge(Knowledge{Type: KnowledgeDecision, Title: "In scope"})
	_ = p.AddKnowledge(Knowledge{Type: KnowledgeDecision, Title: "Out of scope"})

	p.Projects = append(p.Projects, ProjectLink{
		ProjectID:    "proj-1",
		Name:         "exo-web",
		BoardIDs:     []string{vision.ID},
		KnowledgeIDs: []string{decision.ID},
	})

	boards, knowledge := p.ScopeFor("proj-1")
	if len(boards) != 1 || boards[0].ID != vision.ID {
		t.Fatalf("ScopeFor boards = %+v, want just Vision", boards)
	}
	if len(knowledge) != 1 || knowledge[0].ID != decision.ID {
		t.Fatalf("ScopeFor knowledge = %+v, want just %q", knowledge, decision.Title)
	}
}

// TestScopeForUnscoped covers the common case: no explicit scope means the
// Project inherits everything.
func TestScopeForUnscoped(t *testing.T) {
	var p Planning
	p.AddBoard("Vision")
	p.AddKnowledge(Knowledge{Type: KnowledgePrinciple, Title: "..."})
	p.Projects = append(p.Projects, ProjectLink{ProjectID: "proj-1", Name: "exo-web"})

	boards, knowledge := p.ScopeFor("proj-1")
	if len(boards) != 1 || len(knowledge) != 1 {
		t.Fatalf("unscoped ScopeFor should inherit everything: boards=%+v knowledge=%+v", boards, knowledge)
	}
}

func TestStorePathsAreJSON(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.Create("x")
	if filepath.Ext(s.path(p.ID)) != ".json" {
		t.Fatalf("store path %q is not .json", s.path(p.ID))
	}
}
