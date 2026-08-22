// Package sessionrecall implements "Sesiones" — the session-recall system
// designed in planning_design_session_recall_round1_prompt.md/_response.md.
// Separate from "Memorias de la IA" (nucleo-base's Layer 4,
// agenthost/host.go's openMemoryStoreBestEffort): this package only holds
// AI-written summaries of closed chat sessions, pulled by the model via
// agenthost's session_recall tool (list/get, mirroring atom_tool.go), never
// pushed automatically into any turn's context.
//
// One JSON file per project (keyed by ProjectID, the project's absolute
// root path — the same identity canvasstore.ProjectCanvas already uses),
// persisted by Store (store.go), mirroring canvasstore's CAS-protected
// write-tmp-then-rename pattern exactly. This package never imports
// chatstore — a SessionSummary references chatstore.ChatSession only by its
// SessionID, it never copies or embeds transcript content.
package sessionrecall

import "time"

// StatusClosed is the only value SessionSummary.Status holds today — an
// entry exists once its session is closed, full stop, there is no
// draft/materialized-style lifecycle here like Canvas's CanvasObject.Phase.
// Kept as a named constant (and Status kept as a field) rather than omitted
// because Supersedes implies entries can be superseded later
// (re-summarization), which needs somewhere to eventually record "this
// entry is stale, see its successor" without deleting the old one — see
// SessionSummary's doc comment.
const StatusClosed = "closed"

// SessionSummary is one closed session's recall entry. Never mutated in
// place once written — a re-summarization appends a new entry whose
// Supersedes points at the SessionID... actually at the prior entry it
// replaces, mirroring canvasstore.CanvasAtom's Supersedes idiom (append,
// point back, never delete).
type SessionSummary struct {
	SessionID         string    `json:"session_id"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	SummaryBody       string    `json:"summary_body"`
	ClosedAt          time.Time `json:"closed_at"`
	ModelID           string    `json:"model_id,omitempty"`
	ContextPctAtClose float64   `json:"context_pct_at_close,omitempty"`
	Status            string    `json:"status"`
	Supersedes        string    `json:"supersedes,omitempty"`
}

// ProjectRecall is the root aggregate — one JSON file per project.
type ProjectRecall struct {
	ProjectID string           `json:"project_id"`
	Entries   []SessionSummary `json:"entries"`
	Version   int              `json:"version"` // CAS token — see Store.Save
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// Find returns the entry for sessionID, if any.
func (pr ProjectRecall) Find(sessionID string) (SessionSummary, bool) {
	for _, e := range pr.Entries {
		if e.SessionID == sessionID {
			return e, true
		}
	}
	return SessionSummary{}, false
}
