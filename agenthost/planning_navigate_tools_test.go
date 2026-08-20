package agenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/planningstore"
)

func newNavigateBase(t *testing.T, humanMessage string) navigateToolBase {
	t.Helper()
	ps, err := planningstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("planningstore.New: %v", err)
	}
	cell := newNavigateCell()
	cell.current = &navigateSlot{humanMessage: humanMessage}
	return navigateToolBase{store: ps, cell: cell}
}

func mustJSON(t *testing.T, v map[string]string) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

// --- planning_open ---

func TestPlanningOpenExactMatchPlanningOnly(t *testing.T) {
	base := newNavigateBase(t, "abre Exo")
	planning, err := base.store.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if isErr {
		t.Fatalf("planning_open returned error: %s", msg)
	}
	action := base.cell.current.action
	if action == nil || action.PlanningID != planning.ID || action.BoardID != "" {
		t.Fatalf("committed action = %+v, want planning-only for %q", action, planning.ID)
	}
}

// TestPlanningOpenNeverGuessesBoard is Round 3's explicit invariant:
// omitting board_name always lands planning-only, even when the Planning
// has exactly one Board — never auto-selected.
func TestPlanningOpenNeverGuessesBoard(t *testing.T) {
	base := newNavigateBase(t, "abre Exo")
	planning, err := base.store.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	planning.AddBoard("OnlyBoard")
	if err := base.store.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if isErr {
		t.Fatalf("planning_open returned error: %s", msg)
	}
	action := base.cell.current.action
	if action == nil || action.BoardID != "" {
		t.Fatalf("committed action = %+v, want BoardID empty (never auto-select)", action)
	}
}

func TestPlanningOpenWithExplicitBoard(t *testing.T) {
	base := newNavigateBase(t, "abre Exo / Auth")
	planning, err := base.store.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	board := planning.AddBoard("Auth")
	if err := base.store.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Exo", "board_name": "Auth",
	}))
	if isErr {
		t.Fatalf("planning_open returned error: %s", msg)
	}
	action := base.cell.current.action
	if action == nil || action.BoardID != board.ID {
		t.Fatalf("committed action = %+v, want board %q", action, board.ID)
	}
}

func TestPlanningOpenZeroMatchFails(t *testing.T) {
	base := newNavigateBase(t, "abre Exo")
	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if !isErr {
		t.Fatalf("expected error, got success: %s", msg)
	}
	if base.cell.current.action != nil {
		t.Fatalf("action committed despite zero-match failure: %+v", base.cell.current.action)
	}
}

func TestPlanningOpenAmbiguousMatchFails(t *testing.T) {
	base := newNavigateBase(t, "abre Exo")
	if _, err := base.store.Create("Exo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := base.store.Create("EXO"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if !isErr {
		t.Fatalf("expected ambiguous error, got success: %s", msg)
	}
}

// TestPlanningOpenRejectsUnmentionedName is the explicit-naming guard: a
// name the tool call carries but that never appeared in the human's
// message must be refused, not silently acted on.
func TestPlanningOpenRejectsUnmentionedName(t *testing.T) {
	base := newNavigateBase(t, "quiero pensar sobre autenticación")
	if _, err := base.store.Create("Exo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if !isErr {
		t.Fatalf("expected error (name not mentioned), got success: %s", msg)
	}
	if base.cell.current.action != nil {
		t.Fatal("action committed despite failing the naming check")
	}
}

func TestPlanningOpenNilStoreFails(t *testing.T) {
	cell := newNavigateCell()
	cell.current = &navigateSlot{humanMessage: "abre Exo"}
	base := navigateToolBase{store: nil, cell: cell}
	msg, isErr := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if !isErr || msg != "planning is not configured" {
		t.Fatalf("msg=%q isErr=%v, want %q true", msg, isErr, "planning is not configured")
	}
}

// --- planning_create_board_and_open ---

func TestPlanningCreateBoardAndOpenRequiresExistingPlanning(t *testing.T) {
	base := newNavigateBase(t, "en Exo crea un board Auth")
	msg, isErr := (planningCreateBoardAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Exo", "board_name": "Auth",
	}))
	if !isErr {
		t.Fatalf("expected error (planning doesn't exist), got success: %s", msg)
	}
}

func TestPlanningCreateBoardAndOpenSucceeds(t *testing.T) {
	base := newNavigateBase(t, "en Exo crea un board Auth")
	planning, err := base.store.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	msg, isErr := (planningCreateBoardAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Exo", "board_name": "Auth",
	}))
	if isErr {
		t.Fatalf("planning_create_board_and_open returned error: %s", msg)
	}
	action := base.cell.current.action
	if action == nil || action.PlanningID != planning.ID || action.BoardName != "Auth" {
		t.Fatalf("committed action = %+v", action)
	}
	reloaded, err := base.store.Load(planning.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.Boards) != 1 || reloaded.Boards[0].Name != "Auth" {
		t.Fatalf("Boards = %+v, want one named Auth", reloaded.Boards)
	}
}

func TestPlanningCreateBoardAndOpenRefusesDuplicateName(t *testing.T) {
	base := newNavigateBase(t, "en Exo crea un board Auth")
	planning, err := base.store.Create("Exo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	planning.AddBoard("Auth")
	if err := base.store.Save(planning); err != nil {
		t.Fatalf("Save: %v", err)
	}

	msg, isErr := (planningCreateBoardAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Exo", "board_name": "Auth",
	}))
	if !isErr {
		t.Fatalf("expected duplicate-name error, got success: %s", msg)
	}
}

// --- planning_create_planning_and_open ---

func TestPlanningCreatePlanningAndOpenWithoutBoard(t *testing.T) {
	base := newNavigateBase(t, "crea un planning llamado Ecommerce")
	msg, isErr := (planningCreatePlanningAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Ecommerce",
	}))
	if isErr {
		t.Fatalf("planning_create_planning_and_open returned error: %s", msg)
	}
	action := base.cell.current.action
	if action == nil || action.PlanningName != "Ecommerce" || action.BoardID != "" {
		t.Fatalf("committed action = %+v, want planning-only for Ecommerce", action)
	}
}

func TestPlanningCreatePlanningAndOpenWithBoard(t *testing.T) {
	base := newNavigateBase(t, "crea un planning llamado Ecommerce con un board Arquitectura")
	msg, isErr := (planningCreatePlanningAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Ecommerce", "initial_board_name": "Arquitectura",
	}))
	if isErr {
		t.Fatalf("planning_create_planning_and_open returned error: %s", msg)
	}
	action := base.cell.current.action
	if action == nil || action.BoardName != "Arquitectura" {
		t.Fatalf("committed action = %+v, want board Arquitectura", action)
	}
}

// TestPlanningCreatePlanningAndOpenRejectsUnmentionedOptionalBoardName is
// Codex's fix #1: an optional name argument that fails the naming check is
// an outright failure, not silently dropped-and-proceed.
func TestPlanningCreatePlanningAndOpenRejectsUnmentionedOptionalBoardName(t *testing.T) {
	base := newNavigateBase(t, "crea un planning llamado Ecommerce")
	msg, isErr := (planningCreatePlanningAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Ecommerce", "initial_board_name": "Architecture",
	}))
	if !isErr {
		t.Fatalf("expected error (initial_board_name not mentioned), got success: %s", msg)
	}
	if base.cell.current.action != nil {
		t.Fatal("action committed despite invalid optional argument — must fail outright, not drop it")
	}
	if _, err := findPlanningByName(base.store, "Ecommerce"); err == nil {
		t.Fatal("planning was created despite the call failing — must not partially apply")
	}
}

func TestPlanningCreatePlanningAndOpenRefusesDuplicateName(t *testing.T) {
	base := newNavigateBase(t, "crea un planning llamado Exo")
	if _, err := base.store.Create("Exo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	msg, isErr := (planningCreatePlanningAndOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{
		"planning_name": "Exo",
	}))
	if !isErr {
		t.Fatalf("expected duplicate-name error, got success: %s", msg)
	}
}

// --- one navigation per turn ---

// TestNavigateSlotAllowsOnlyOneCommitPerTurn: two navigation tool calls
// sharing the same (per-turn) slot — the second must be refused, the first
// must stand. The model doesn't get to change its mind mid-turn.
func TestNavigateSlotAllowsOnlyOneCommitPerTurn(t *testing.T) {
	base := newNavigateBase(t, "abre Exo y tambien abre Otro")
	if _, err := base.store.Create("Exo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := base.store.Create("Otro"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	msg1, isErr1 := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Exo"}))
	if isErr1 {
		t.Fatalf("first planning_open returned error: %s", msg1)
	}
	firstAction := base.cell.current.action
	if firstAction == nil {
		t.Fatal("first call did not commit")
	}

	msg2, isErr2 := (planningOpenTool{base}).Execute(context.Background(), mustJSON(t, map[string]string{"planning_name": "Otro"}))
	if !isErr2 {
		t.Fatalf("second planning_open should have been refused, got success: %s", msg2)
	}
	if base.cell.current.action != firstAction {
		t.Fatalf("committed action changed after the refused second call: %+v", base.cell.current.action)
	}
}

// TestHostBeginTurnResetsNavigateSlot confirms Host wiring: a fresh
// BeginTurn call must not see a previous turn's committed action.
func TestHostBeginTurnResetsNavigateSlot(t *testing.T) {
	cell := newNavigateCell()
	cell.current.action = &NavigateAction{PlanningID: "stale"}
	h := &Host{navigateCell: cell}

	h.BeginTurn("next turn's message", "")
	if h.TakeNavigateAction() != nil {
		t.Fatalf("TakeNavigateAction = %+v after BeginTurn, want nil (reset)", h.TakeNavigateAction())
	}
}
