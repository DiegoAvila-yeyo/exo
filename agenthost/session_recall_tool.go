package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/sessionrecall"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// sessionRecallTool exposes closed-session summaries (the "Sesiones"
// system, sessionrecall package) as a tool: "list" returns titles +
// one-line descriptions for the active project's closed sessions only,
// "get" fetches one session's full summary + metadata. Structurally mirrors
// atom_tool.go's list/get contract exactly, per
// planning_design_session_recall_round1_response.md's decision #5 — same
// shape for the model, different (and separate) backing store.
//
// "get" never returns the raw chatstore transcript — only summary +
// metadata. The transcript stays human/manual backup, reachable only by
// opening the (read-only, once closed) session in the UI, never through
// this tool. Adding a transcript-fetch action here would bypass the whole
// point of the pull-only, summary-only recall design.
type sessionRecallTool struct {
	store *sessionrecall.Store
	cell  *canvasCell // scope: cell.projectID, refreshed every turn by Host.BeginTurn — never from tool input
}

func (sessionRecallTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "session_recall",
		Description: "Look up summaries of past, closed chat sessions in this project — pull " +
			"context from earlier work without loading a full transcript. Call with action " +
			"\"list\" to see every closed session's title and one-line description, then action " +
			"\"get\" with an exact session_id to load that session's full summary.",
		InputSchema: map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "\"list\" to see closed sessions, or \"get\" to fetch one summary.",
				"enum":        []string{"list", "get"},
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Exact session id. Required when action is \"get\".",
			},
		},
		Required: []string{"action"},
	}
}

func (t sessionRecallTool) Execute(_ context.Context, rawInput string) (string, bool) {
	if t.store == nil {
		return "session recall is not configured", true
	}
	if t.cell == nil || strings.TrimSpace(t.cell.projectID) == "" {
		return "session recall has no active project for this turn", true
	}

	var in struct {
		Action    string `json:"action"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}

	projectRecall, err := t.store.Load(t.cell.projectID)
	if err != nil {
		return fmt.Sprintf("failed to load session recall: %v", err), true
	}

	switch strings.TrimSpace(in.Action) {
	case "", "list":
		return renderSessionRecallIndex(projectRecall), false
	case "get":
		sessionID := strings.TrimSpace(in.SessionID)
		if sessionID == "" {
			return "action \"get\" requires a \"session_id\"", true
		}
		entry, ok := projectRecall.Find(sessionID)
		if !ok {
			return fmt.Sprintf("session %q not found in this project's recall store. Call session_recall list to see available ids.", sessionID), true
		}
		return renderSessionSummary(entry), false
	default:
		return fmt.Sprintf("unknown action %q, expected \"list\" or \"get\"", in.Action), true
	}
}

func renderSessionRecallIndex(pr sessionrecall.ProjectRecall) string {
	if len(pr.Entries) == 0 {
		return "No closed sessions with a summary yet in this project."
	}
	var sb strings.Builder
	for _, e := range pr.Entries {
		fmt.Fprintf(&sb, "- %s: %s — %s\n", e.SessionID, e.Title, e.Description)
	}
	return sb.String()
}

func renderSessionSummary(e sessionrecall.SessionSummary) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Title: %s\n", e.Title)
	fmt.Fprintf(&sb, "Closed at: %s\n", e.ClosedAt.Format("2006-01-02 15:04"))
	if e.ModelID != "" {
		fmt.Fprintf(&sb, "Model: %s\n", e.ModelID)
	}
	if e.ContextPctAtClose > 0 {
		fmt.Fprintf(&sb, "Context window at close: %.0f%%\n", e.ContextPctAtClose)
	}
	sb.WriteString("\n")
	sb.WriteString(e.SummaryBody)
	return sb.String()
}
