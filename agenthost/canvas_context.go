package agenthost

import "strings"

// canvasCell holds the per-turn state the canvas_* tools need: which
// project this turn is scoped to, and the turn's raw human message for
// nameMentioned-style disambiguation checks (see canvas_tools.go). Mirrors
// navigateCell (planning_navigate.go): built once at Host creation, shared
// by pointer with every canvas_* tool for the Host's entire lifetime, and
// refreshed once per turn by the same Host.BeginTurn call that already
// refreshes navigateCell — not a second per-turn entrypoint.
type canvasCell struct {
	projectID    string
	humanMessage string
}

func newCanvasCell() *canvasCell {
	return &canvasCell{}
}

// nameMentioned reuses Round 3's explicit-naming guard verbatim (see
// navigateToolBase.nameMentioned in planning_navigate_tools.go): a
// non-empty name a canvas tool is about to act on must have appeared in
// what the human actually typed this turn, or the call is rejected — never
// silently treated as if the name had been omitted.
func (c *canvasCell) nameMentioned(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return true
	}
	return strings.Contains(strings.ToLower(c.humanMessage), strings.ToLower(trimmed))
}
