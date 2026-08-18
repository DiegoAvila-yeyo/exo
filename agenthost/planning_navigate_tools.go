package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/planningstore"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// navigateToolBase is embedded by the three navigation tools. Unlike
// planningToolBase (planning_tools.go), these tools don't require any
// existing Planning/Board context — their whole job is to establish one —
// so there's no currentPlanning/currentBoard gate here, only the store and
// the per-turn navigateCell.
type navigateToolBase struct {
	store *planningstore.Store
	cell  *navigateCell
}

// nameMentioned is Round 3's explicit-naming guard: a real (if best-effort)
// check that a name a tool is about to act on actually appeared in what the
// human typed this turn, not something the model completed on its own.
// Empty name means "not supplied" — always fine here, callers decide
// whether an empty optional name is acceptable for their own tool; this
// only ever rejects a *non-empty* name that isn't in the message.
func (b navigateToolBase) nameMentioned(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return true
	}
	return strings.Contains(strings.ToLower(b.cell.current.humanMessage), strings.ToLower(trimmed))
}

func (b navigateToolBase) commit(a *NavigateAction) bool {
	return b.cell.current.commit(a)
}

// findPlanningByName does Round 3's exact, case-insensitive (whitespace-
// trimmed) lookup — never fuzzy. Zero or more-than-one matches are both
// errors; the caller never guesses which one was meant.
func findPlanningByName(store *planningstore.Store, name string) (planningstore.PlanningSummary, error) {
	list, err := store.List()
	if err != nil {
		return planningstore.PlanningSummary{}, fmt.Errorf("could not list plannings: %w", err)
	}
	target := strings.ToLower(strings.TrimSpace(name))
	var matches []planningstore.PlanningSummary
	for _, p := range list {
		if strings.ToLower(strings.TrimSpace(p.Name)) == target {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return planningstore.PlanningSummary{}, fmt.Errorf("no planning named %q", name)
	case 1:
		return matches[0], nil
	default:
		return planningstore.PlanningSummary{}, fmt.Errorf("more than one planning named %q — ambiguous", name)
	}
}

func findBoardByName(planning planningstore.Planning, name string) (planningstore.Board, error) {
	target := strings.ToLower(strings.TrimSpace(name))
	var matches []planningstore.Board
	for _, b := range planning.Boards {
		if strings.ToLower(strings.TrimSpace(b.Name)) == target {
			matches = append(matches, b)
		}
	}
	switch len(matches) {
	case 0:
		return planningstore.Board{}, fmt.Errorf("no board named %q in planning %q", name, planning.Name)
	case 1:
		return matches[0], nil
	default:
		return planningstore.Board{}, fmt.Errorf("more than one board named %q — ambiguous", name)
	}
}

// --- planning_open ---

type planningOpenTool struct{ base navigateToolBase }

func (t planningOpenTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "planning_open",
		Description: "Open a Planning — and, optionally, a specific Board inside it — by exact " +
			"name, navigating the user's screen there. Never creates anything; fails clearly if the " +
			"Planning or Board doesn't exist, or if the name is ambiguous. planning_name and " +
			"board_name (when given) MUST be names the human explicitly typed in their current " +
			"message — never invent, complete, or guess a name. Omitting board_name lands on the " +
			"Planning with no Board selected, even if it has exactly one Board or a recently-used " +
			"one — this never guesses which Board to open.",
		InputSchema: map[string]any{
			"planning_name": map[string]any{
				"type":        "string",
				"description": "Exact Planning name, as typed by the human.",
			},
			"board_name": map[string]any{
				"type":        "string",
				"description": "Exact Board name, as typed by the human. Omit to land on the Planning with no Board open.",
			},
		},
		Required: []string{"planning_name"},
	}
}

func (t planningOpenTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		PlanningName string `json:"planning_name"`
		BoardName    string `json:"board_name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	if t.base.store == nil {
		return "planning is not configured", true
	}
	if !t.base.nameMentioned(in.PlanningName) {
		return "planning_name wasn't in your message — only open a Planning the human explicitly named", true
	}
	if !t.base.nameMentioned(in.BoardName) {
		return "board_name wasn't in your message — only open a Board the human explicitly named", true
	}

	summary, err := findPlanningByName(t.base.store, in.PlanningName)
	if err != nil {
		return err.Error(), true
	}

	action := &NavigateAction{PlanningID: summary.ID, PlanningName: summary.Name}
	if strings.TrimSpace(in.BoardName) != "" {
		planning, err := t.base.store.Load(summary.ID)
		if err != nil {
			return fmt.Sprintf("planning unavailable: %v", err), true
		}
		board, err := findBoardByName(planning, in.BoardName)
		if err != nil {
			return err.Error(), true
		}
		action.BoardID = board.ID
		action.BoardName = board.Name
	}

	if !t.base.commit(action) {
		return "a navigation target has already been selected for this turn", true
	}
	if action.BoardID != "" {
		return fmt.Sprintf("opening %s / %s", action.PlanningName, action.BoardName), false
	}
	return fmt.Sprintf("opening %s", action.PlanningName), false
}

// --- planning_create_board_and_open ---

type planningCreateBoardAndOpenTool struct{ base navigateToolBase }

func (t planningCreateBoardAndOpenTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "planning_create_board_and_open",
		Description: "Create a new Board inside an existing Planning and navigate the user's " +
			"screen there. The Planning must already exist — this never creates one (use " +
			"planning_create_planning_and_open for that). planning_name and board_name MUST be " +
			"names the human explicitly typed in their current message.",
		InputSchema: map[string]any{
			"planning_name": map[string]any{
				"type":        "string",
				"description": "Exact name of the existing Planning, as typed by the human.",
			},
			"board_name": map[string]any{
				"type":        "string",
				"description": "Name for the new Board, as typed by the human.",
			},
		},
		Required: []string{"planning_name", "board_name"},
	}
}

func (t planningCreateBoardAndOpenTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		PlanningName string `json:"planning_name"`
		BoardName    string `json:"board_name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	if t.base.store == nil {
		return "planning is not configured", true
	}
	if !t.base.nameMentioned(in.PlanningName) {
		return "planning_name wasn't in your message — only act on a Planning the human explicitly named", true
	}
	boardName := strings.TrimSpace(in.BoardName)
	if boardName == "" {
		return "\"board_name\" is required", true
	}
	if !t.base.nameMentioned(boardName) {
		return "board_name wasn't in your message — only create a Board with a name the human explicitly gave", true
	}

	summary, err := findPlanningByName(t.base.store, in.PlanningName)
	if err != nil {
		return err.Error(), true
	}
	planning, err := t.base.store.Load(summary.ID)
	if err != nil {
		return fmt.Sprintf("planning unavailable: %v", err), true
	}
	if _, err := findBoardByName(planning, boardName); err == nil {
		return fmt.Sprintf("a board named %q already exists in %q", boardName, planning.Name), true
	}

	board := planning.AddBoard(boardName)
	if err := t.base.store.Save(planning); err != nil {
		return fmt.Sprintf("failed to save: %v", err), true
	}

	action := &NavigateAction{PlanningID: planning.ID, PlanningName: planning.Name, BoardID: board.ID, BoardName: board.Name}
	if !t.base.commit(action) {
		return "a navigation target has already been selected for this turn", true
	}
	return fmt.Sprintf("created board %q in %q, opening it", board.Name, planning.Name), false
}

// --- planning_create_planning_and_open ---

type planningCreatePlanningAndOpenTool struct{ base navigateToolBase }

func (t planningCreatePlanningAndOpenTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "planning_create_planning_and_open",
		Description: "Create a brand-new Planning — the only tool that can — and navigate the " +
			"user's screen there. planning_name MUST be a name the human explicitly typed in their " +
			"current message. initial_board_name is optional; if you supply it, it MUST also have " +
			"been explicitly typed by the human this turn — never invent a default like \"General\" " +
			"or \"Untitled\". Without it, this lands on the new Planning with no Board open.",
		InputSchema: map[string]any{
			"planning_name": map[string]any{
				"type":        "string",
				"description": "Name for the new Planning, as typed by the human.",
			},
			"initial_board_name": map[string]any{
				"type":        "string",
				"description": "Optional: name for its first Board, only if the human also explicitly named this.",
			},
		},
		Required: []string{"planning_name"},
	}
}

func (t planningCreatePlanningAndOpenTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		PlanningName     string `json:"planning_name"`
		InitialBoardName string `json:"initial_board_name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	if t.base.store == nil {
		return "planning is not configured", true
	}
	planningName := strings.TrimSpace(in.PlanningName)
	if planningName == "" {
		return "\"planning_name\" is required", true
	}
	if !t.base.nameMentioned(planningName) {
		return "planning_name wasn't in your message — only create a Planning with a name the human explicitly gave", true
	}
	// Optional name arguments are an invariant, not a best-effort nicety:
	// if the model supplies one and it fails the naming check, the call
	// fails outright — it does not silently drop the argument and proceed
	// as if it had been omitted, which would let the model "helpfully
	// complete" an intention the human never stated.
	initialBoardName := strings.TrimSpace(in.InitialBoardName)
	if initialBoardName != "" && !t.base.nameMentioned(initialBoardName) {
		return "initial_board_name wasn't in your message — omit it, or only use a name the human explicitly gave", true
	}

	if _, err := findPlanningByName(t.base.store, planningName); err == nil {
		return fmt.Sprintf("a planning named %q already exists", planningName), true
	}

	planning, err := t.base.store.Create(planningName)
	if err != nil {
		return fmt.Sprintf("failed to create planning: %v", err), true
	}

	action := &NavigateAction{PlanningID: planning.ID, PlanningName: planning.Name}
	if initialBoardName != "" {
		board := planning.AddBoard(initialBoardName)
		if err := t.base.store.Save(planning); err != nil {
			return fmt.Sprintf("planning created but failed to save its first board: %v", err), true
		}
		action.BoardID = board.ID
		action.BoardName = board.Name
	}

	if !t.base.commit(action) {
		return "a navigation target has already been selected for this turn", true
	}
	if action.BoardID != "" {
		return fmt.Sprintf("created planning %q with board %q, opening it", planning.Name, action.BoardName), false
	}
	return fmt.Sprintf("created planning %q, opening it", planning.Name), false
}
