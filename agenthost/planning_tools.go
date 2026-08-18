package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/planningstore"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// notInPlanningErr is returned by tools that only need to know which
// Planning is open — right now just planning_create_board. Being "in a
// Planning" with no Board selected is a real, valid state (a Planning with
// no Boards yet), so this is the weaker of the two gates.
const notInPlanningErr = "not currently inside a Planning — this tool only works while the user " +
	"has the Planning section open in the app"

// notInPlanningBoardErr is returned by tools that act on a specific Board
// (planning_list_board, planning_create_note) — being in a Planning isn't
// enough for these, a Board must actually be open.
const notInPlanningBoardErr = "not currently inside a Planning board — this tool only works while " +
	"the user has a specific Board open in the app"

// planningToolBase is embedded by all three planning_* tools. It centralizes
// Round 2's two safety rules that apply to every one of them: reload the
// Planning/Board fresh from planningstore.Store on every Execute (never
// cache a pointer across turns — the board may have been deleted or moved
// between when context was set and when the tool actually runs), and treat
// "board not found under this planning" and "not in a board at all" as the
// same class of clear, non-panicking error.
type planningToolBase struct {
	store *planningstore.Store
	ctx   *planningContext
}

// currentPlanning reloads the Planning the current turn is scoped to,
// requiring only planningID — boardID may be empty (in the Planning, no
// Board open yet). Used by planning_create_board, which doesn't need an
// existing Board to act; currentBoard (below) is the stricter gate for
// tools that do.
func (b planningToolBase) currentPlanning() (planningstore.Planning, string, bool) {
	if b.store == nil {
		return planningstore.Planning{}, "planning is not configured", false
	}
	planningID, _ := b.ctx.get()
	if planningID == "" {
		return planningstore.Planning{}, notInPlanningErr, false
	}
	p, err := b.store.Load(planningID)
	if err != nil {
		return planningstore.Planning{}, fmt.Sprintf("planning unavailable: %v", err), false
	}
	return p, "", true
}

// currentBoard reloads the Planning and locates the Board fresh — this is
// also where "board_id no longer belongs to planning_id" (e.g. deleted and
// recreated elsewhere between turns) surfaces, as a plain "board no longer
// exists" error rather than acting on stale data. Requires both planningID
// and boardID set; a Planning with no Board open fails here with
// notInPlanningBoardErr even though currentPlanning alone would succeed.
func (b planningToolBase) currentBoard() (planningstore.Planning, planningstore.Board, string, bool) {
	p, errMsg, ok := b.currentPlanning()
	if !ok {
		return planningstore.Planning{}, planningstore.Board{}, errMsg, false
	}
	_, boardID := b.ctx.get()
	if boardID == "" {
		return planningstore.Planning{}, planningstore.Board{}, notInPlanningBoardErr, false
	}
	for _, board := range p.Boards {
		if board.ID == boardID {
			return p, board, "", true
		}
	}
	return planningstore.Planning{}, planningstore.Board{}, "the current board no longer exists", false
}

// --- planning_list_board ---

type planningListBoardTool struct{ planningToolBase }

type planningKnowledgeSummary struct {
	Title  string `json:"title"`
	Type   string `json:"type"`
	Author string `json:"author"`
}

func (t planningListBoardTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "planning_list_board",
		Description: "List the Notes/Research/Questions already on the current Planning Board, " +
			"so you don't propose duplicates. Only works while the user has a Planning Board open " +
			"in the app — outside of that it fails with a clear error, don't retry it from a normal " +
			"chat. Each entry includes its author: \"human\" or \"accepted\" means confirmed " +
			"knowledge, \"ai_suggested\" means a still-pending suggestion — don't treat the latter " +
			"as settled. Rejected/archived entries are never returned.",
		InputSchema: map[string]any{},
	}
}

func (t planningListBoardTool) Execute(_ context.Context, _ string) (string, bool) {
	p, board, errMsg, ok := t.currentBoard()
	if !ok {
		return errMsg, true
	}
	out := make([]planningKnowledgeSummary, 0)
	for _, k := range p.ForBoard(board.ID) {
		if k.Author == planningstore.AuthorRejected || k.Author == planningstore.AuthorArchived {
			continue
		}
		out = append(out, planningKnowledgeSummary{Title: k.Title, Type: string(k.Type), Author: string(k.Author)})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf("failed to encode result: %v", err), true
	}
	return string(data), false
}

// --- planning_create_board ---

// Boards are structural containers, not canonical knowledge. AI-created
// Boards may be created directly without Author metadata or acceptance.
// Only Knowledge is governed by AuthorState in Round 2.
type planningCreateBoardTool struct{ planningToolBase }

func (t planningCreateBoardTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "planning_create_board",
		Description: "Create a new Board inside the current Planning. Boards are just named spaces " +
			"to organize thinking in — created directly, no human review step. Works as soon as the " +
			"user has a Planning open in the app, even one with no Boards yet — you don't need an " +
			"existing Board open to use this.",
		InputSchema: map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The new board's name, e.g. \"Auth Flow\".",
			},
		},
		Required: []string{"name"},
	}
}

func (t planningCreateBoardTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "\"name\" is required", true
	}

	p, errMsg, ok := t.currentPlanning()
	if !ok {
		return errMsg, true
	}
	board := p.AddBoard(name)
	if err := t.store.Save(p); err != nil {
		return fmt.Sprintf("failed to save: %v", err), true
	}
	return fmt.Sprintf("created board %q (id %s)", board.Name, board.ID), false
}

// --- planning_create_note ---

type planningCreateNoteTool struct{ planningToolBase }

var planningAllowedNoteTypes = map[string]planningstore.KnowledgeType{
	"note":     planningstore.KnowledgeNote,
	"research": planningstore.KnowledgeResearch,
	"question": planningstore.KnowledgeQuestion,
}

func (t planningCreateNoteTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "planning_create_note",
		Description: "Add a Note, Research entry, or Question to the current Planning Board. type " +
			"must be exactly one of \"note\", \"research\", \"question\" — Decisions and Principles " +
			"cannot be created through this tool at all; if the user wants one of those, tell them " +
			"to create it directly on the board instead of calling this. Everything created here is " +
			"marked as an AI suggestion (author=\"ai_suggested\") and stays pending until the human " +
			"accepts or rejects it from the Notes panel. Only works while the user has a Planning " +
			"Board open in the app.",
		InputSchema: map[string]any{
			"type": map[string]any{
				"type":        "string",
				"description": "One of \"note\", \"research\", \"question\". Never \"decision\" or \"principle\".",
				"enum":        []string{"note", "research", "question"},
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Short title.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "The content.",
			},
		},
		Required: []string{"type", "title"},
	}
}

func (t planningCreateNoteTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	knowledgeType, ok := planningAllowedNoteTypes[strings.TrimSpace(in.Type)]
	if !ok {
		return fmt.Sprintf(
			"invalid type %q: planning_create_note only accepts \"note\", \"research\", or "+
				"\"question\" — Decisions and Principles can't be created through Planning tools",
			in.Type,
		), true
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return "\"title\" is required", true
	}

	p, board, errMsg, okBoard := t.currentBoard()
	if !okBoard {
		return errMsg, true
	}
	k := p.AddKnowledge(planningstore.Knowledge{
		Type:    knowledgeType,
		Title:   title,
		Body:    in.Body,
		BoardID: board.ID,
		Author:  planningstore.AuthorAISuggested,
	})
	if err := t.store.Save(p); err != nil {
		return fmt.Sprintf("failed to save: %v", err), true
	}
	return fmt.Sprintf("created %s %q (id %s) — pending human review", in.Type, k.Title, k.ID), false
}
