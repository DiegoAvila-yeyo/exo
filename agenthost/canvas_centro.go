package agenthost

import (
	"fmt"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/canvasstore"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/instructions"
)

// dynamicCentro mirrors yeyo.RenderCentro()'s delivery mechanism (plain
// text appended into the system prompt) but is sourced from canvasstore
// instead of the yeyo catalog, and rebuilt every turn rather than only
// when rootPath changes — see RefreshCanvasCentro. planningContext
// (planning_context.go) is not involved: this is a fully separate
// anchoring path, deliberately kept narrow to "scope for the legacy
// planning_* tools" per the Canvas build spec.
//
// Fails open: any error loading the canvas (not configured, no project
// open, read failure) yields "" rather than blocking the turn — a broken
// Canvas must never take the agent down with it, same posture as
// openMemoryStoreBestEffort elsewhere in this package.
func dynamicCentro(store *canvasstore.Store, projectID string) string {
	if store == nil || projectID == "" {
		return ""
	}
	pc, err := store.Load(projectID)
	if err != nil {
		return ""
	}

	var b strings.Builder
	for _, id := range pc.ActiveObjectIDs {
		obj := findObjectByID(pc, id)
		if obj == nil || obj.Phase != canvasstore.PhaseMaterialized || obj.Activation != canvasstore.ActivationActive {
			// ActiveObjectIDs can point at an object that was later deleted,
			// or whose Activation was flipped off through a path that didn't
			// also clean up the list — skip rather than error.
			continue
		}
		atom, ok := pc.CurrentAtom(id)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n\n--- Canvas object %q (%s) ---\n%s", obj.Name, obj.Type, atom.Body)
	}
	return b.String()
}

// RefreshCanvasCentro rebuilds h.agent.System from the current rootPath's
// static centro plus this turn's dynamicCentro block, unconditionally.
// Unlike SetRootPath's system-prompt rebuild (which is skipped whenever the
// path didn't change, since most turns stay in the same project),
// RefreshCanvasCentro must run every single turn — the Canvas build spec
// requires the active set be read "every turn while Activation == active,"
// and a static/skipped rebuild would silently go stale the moment an
// object's content changes without a rootPath change alongside it.
//
// Call once per turn, before Run, in the same pre-Run sequence as
// SetPlanningContext/BeginTurn, under the same lock that serializes calls
// to Run (Server.agentMu — see planning_context.go's doc comment for the
// verified, not assumed, invariant this relies on).
func (h *Host) RefreshCanvasCentro() {
	var projectID string
	if h.canvasCell != nil {
		projectID = h.canvasCell.projectID
	}
	block := dynamicCentro(h.canvasStore, projectID)
	systemPrompt := buildSystemPrompt(h.currentRootPath, block)
	if h.agent != nil {
		h.agent.System = systemPrompt
	}
	if h.coordinator != nil {
		h.coordinator.Layers = instructions.FromPrompt(systemPrompt)
	}
}
