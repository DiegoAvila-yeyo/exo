package agenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/planningstore"
)

func newTestPlanningStoreForTools(t *testing.T) *planningstore.Store {
	t.Helper()
	ps, err := planningstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("planningstore.New: %v", err)
	}
	return ps
}

func TestPlanningToolsFailClearlyWithoutContext(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	base := planningToolBase{store: ps, ctx: &planningContext{}}

	// With no context at all (planningID == ""), currentBoard's chained
	// call into currentPlanning surfaces the weaker "not in a Planning"
	// error first — the Board-specific error only kicks in once a Planning
	// IS set but no Board is (see TestPlanningBoardScopedToolsRejectPlanningOnlyContext).
	if msg, isErr := (planningListBoardTool{base}).Execute(context.Background(), `{}`); !isErr || msg != notInPlanningErr {
		t.Fatalf("planning_list_board without context: msg=%q isErr=%v, want %q true", msg, isErr, notInPlanningErr)
	}
	if msg, isErr := (planningCreateBoardTool{base}).Execute(context.Background(), `{"name":"x"}`); !isErr || msg != notInPlanningErr {
		t.Fatalf("planning_create_board without context: msg=%q isErr=%v, want %q true", msg, isErr, notInPlanningErr)
	}
	if msg, isErr := (planningCreateNoteTool{base}).Execute(context.Background(), `{"type":"note","title":"x"}`); !isErr || msg != notInPlanningErr {
		t.Fatalf("planning_create_note without context: msg=%q isErr=%v, want %q true", msg, isErr, notInPlanningErr)
	}
}

func TestPlanningToolsFailClearlyWithNilStore(t *testing.T) {
	base := planningToolBase{store: nil, ctx: &planningContext{planningID: "p", boardID: "b"}}
	msg, isErr := (planningListBoardTool{base}).Execute(context.Background(), `{}`)
	if !isErr || msg != "planning is not configured" {
		t.Fatalf("msg=%q isErr=%v, want %q true", msg, isErr, "planning is not configured")
	}
}

func TestPlanningCreateNoteRoundTripsWithAISuggestedAuthor(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	base := planningToolBase{store: ps, ctx: &planningContext{planningID: planning.ID, boardID: board.ID}}

	createMsg, isErr := (planningCreateNoteTool{base}).Execute(context.Background(), `{"type":"note","title":"Use Redis","body":"for caching"}`)
	if isErr {
		t.Fatalf("planning_create_note returned error: %s", createMsg)
	}

	listMsg, isErr := (planningListBoardTool{base}).Execute(context.Background(), `{}`)
	if isErr {
		t.Fatalf("planning_list_board returned error: %s", listMsg)
	}
	var entries []planningKnowledgeSummary
	if err := json.Unmarshal([]byte(listMsg), &entries); err != nil {
		t.Fatalf("decode planning_list_board result: %v (raw: %s)", err, listMsg)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	if entries[0].Title != "Use Redis" || entries[0].Type != "note" {
		t.Fatalf("entry = %+v, want title=%q type=%q", entries[0], "Use Redis", "note")
	}
	if entries[0].Author != string(planningstore.AuthorAISuggested) {
		t.Fatalf("entry.Author = %q, want %q", entries[0].Author, planningstore.AuthorAISuggested)
	}
}

func TestPlanningCreateNoteRejectsDecisionAndPrincipleTypes(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}
	base := planningToolBase{store: ps, ctx: &planningContext{planningID: planning.ID, boardID: board.ID}}

	for _, forbidden := range []string{"decision", "principle", "reference", "bogus"} {
		input, _ := json.Marshal(map[string]string{"type": forbidden, "title": "should not exist"})
		msg, isErr := (planningCreateNoteTool{base}).Execute(context.Background(), string(input))
		if !isErr {
			t.Fatalf("type=%q: expected error, got success: %s", forbidden, msg)
		}
	}

	reloaded, err := ps.Load(planning.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Knowledge) != 0 {
		t.Fatalf("Knowledge = %+v, want none created", reloaded.Knowledge)
	}
}

// TestPlanningCreateBoardWorksWithPlanningOnlyContext is the fix for the
// bootstrap gap: a Planning with no Boards yet (boardID == "") must still
// let planning_create_board create the first one.
func TestPlanningCreateBoardWorksWithPlanningOnlyContext(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	base := planningToolBase{store: ps, ctx: &planningContext{planningID: planning.ID, boardID: ""}}

	msg, isErr := (planningCreateBoardTool{base}).Execute(context.Background(), `{"name":"Vision"}`)
	if isErr {
		t.Fatalf("planning_create_board with planning-only context returned error: %s", msg)
	}

	reloaded, err := ps.Load(planning.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Boards) != 1 || reloaded.Boards[0].Name != "Vision" {
		t.Fatalf("Boards = %+v, want one named %q", reloaded.Boards, "Vision")
	}
}

// TestPlanningBoardScopedToolsRejectPlanningOnlyContext confirms the other
// two tools stay strict: being in a Planning isn't enough for them, they
// need an actual Board open.
func TestPlanningBoardScopedToolsRejectPlanningOnlyContext(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	base := planningToolBase{store: ps, ctx: &planningContext{planningID: planning.ID, boardID: ""}}

	if msg, isErr := (planningListBoardTool{base}).Execute(context.Background(), `{}`); !isErr || msg != notInPlanningBoardErr {
		t.Fatalf("planning_list_board with planning-only context: msg=%q isErr=%v, want %q true", msg, isErr, notInPlanningBoardErr)
	}
	if msg, isErr := (planningCreateNoteTool{base}).Execute(context.Background(), `{"type":"note","title":"x"}`); !isErr || msg != notInPlanningBoardErr {
		t.Fatalf("planning_create_note with planning-only context: msg=%q isErr=%v, want %q true", msg, isErr, notInPlanningBoardErr)
	}
}

func TestPlanningCreateBoardIsDirectNoAuthorNoReview(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}
	base := planningToolBase{store: ps, ctx: &planningContext{planningID: planning.ID, boardID: board.ID}}

	msg, isErr := (planningCreateBoardTool{base}).Execute(context.Background(), `{"name":"Auth Flow"}`)
	if isErr {
		t.Fatalf("planning_create_board returned error: %s", msg)
	}

	reloaded, err := ps.Load(planning.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, b := range reloaded.Boards {
		if b.Name == "Auth Flow" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Boards = %+v, want one named %q", reloaded.Boards, "Auth Flow")
	}
	if len(reloaded.Boards) != 2 {
		t.Fatalf("Boards count = %d, want 2 (Vision + Auth Flow)", len(reloaded.Boards))
	}
}

// TestPlanningToolsReloadFreshNeverCacheAcrossTurns is Round 2's second
// small addition: a tool must reload the Board fresh on every Execute, so a
// Board deleted between when context was set and when the tool runs
// surfaces as a clear error, never a nil dereference or a write against
// stale data. There's no "delete board" operation in the model yet, so this
// simulates deletion the only way currently possible: saving a Planning
// whose Boards slice no longer contains the one the context still points
// at (e.g. it was recreated/rewritten by a concurrent actor).
func TestPlanningToolsReloadFreshNeverCacheAcrossTurns(t *testing.T) {
	ps := newTestPlanningStoreForTools(t)
	planning, err := ps.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Vision")
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}
	base := planningToolBase{store: ps, ctx: &planningContext{planningID: planning.ID, boardID: board.ID}}

	// First call succeeds normally.
	if msg, isErr := (planningListBoardTool{base}).Execute(context.Background(), `{}`); isErr {
		t.Fatalf("first call: unexpected error: %s", msg)
	}

	// The board disappears from the Planning (simulating deletion) between
	// turns; the tool's *planningContext still points at its old ID.
	planning.Boards = nil
	if err := ps.Save(planning); err != nil {
		t.Fatalf("Save (removing board): %v", err)
	}

	msg, isErr := (planningListBoardTool{base}).Execute(context.Background(), `{}`)
	if !isErr {
		t.Fatalf("second call: expected error after board removed, got success: %s", msg)
	}
	if msg != "the current board no longer exists" {
		t.Fatalf("second call error = %q, want %q", msg, "the current board no longer exists")
	}
}
