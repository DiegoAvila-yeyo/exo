package agenthost

import (
	"fmt"
	"strings"
)

// canvasCell holds the per-turn state the canvas_* tools need: which
// project this turn is scoped to, the turn's raw human message for
// nameMentioned-style disambiguation checks (see canvas_tools.go), and
// which single Canvas object (if any) this turn is scoped to. Mirrors
// navigateCell (planning_navigate.go): built once at Host creation, shared
// by pointer with every canvas_* tool for the Host's entire lifetime, and
// refreshed once per turn by the same Host.BeginTurn call that already
// refreshes navigateCell — not a second per-turn entrypoint.
//
// scopedObjectID is how the floating panel's mini-chat stays confined to
// its own object even when other objects are simultaneously anchored via
// dynamicCentro. Before this existed, the mini-chat only "scoped" itself by
// prefixing the human's message with the object's name/id as plain text
// (termserver/assets/app.js) — advisory only, nothing enforced it, and with
// two objects active at once the model could (and did, in live testing)
// call canvas_edit_object against the wrong object_id, or call
// canvas_create_draft and spawn a duplicate object instead of editing the
// one the human was actually looking at. scopedObjectID is set from the
// browser's own request (the panel it has open), never inferred by the
// model — same "the browser establishes context, not the model" principle
// already used for planning_id/board_id (see planning_context.go). Empty
// means "ordinary main-chat turn, not scoped to any single object."
type canvasCell struct {
	projectID      string
	humanMessage   string
	scopedObjectID string
}

// checkScope rejects a canvas tool call whose target objectID doesn't match
// this turn's scope, when the turn is scoped. Called by every canvas_* tool
// that either targets an existing object_id (canvas_edit_object,
// canvas_activate_object, canvas_deactivate_object) or would bring a
// different object into being (canvas_create_draft, canvas_materialize_draft
// — pass "" for objectID, since "a scoped turn may only touch its own
// already-materialized object" applies regardless of what object_id, if
// any, the call names). Unscoped turns (scopedObjectID == "") always pass —
// this only restricts mini-chat turns, never the main composer.
func (c *canvasCell) checkScope(objectID string) error {
	if c.scopedObjectID == "" {
		return nil
	}
	if objectID == c.scopedObjectID {
		return nil
	}
	return fmt.Errorf(
		"this conversation is scoped to Canvas object %q — it can't act on %q or create a new object; "+
			"open that other object's own panel to work on it",
		c.scopedObjectID, objectIDOrNone(objectID))
}

func objectIDOrNone(objectID string) string {
	if objectID == "" {
		return "(a new object)"
	}
	return objectID
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
