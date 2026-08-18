package agenthost

// planningContext is the Planning/Board the current chat turn is scoped
// to, shared by pointer between Host and the three planning_* tools
// (planning_tools.go). Host.SetPlanningContext writes it once per turn,
// before Run; tools read it during Execute.
//
// Plain mutable fields, no mutex — verified safe (not assumed) against
// Round 2's build prompt: termserver/chat.go's handleChat holds
// Server.agentMu for the entire duration of one turn's host.Run call (lock
// acquired before dispatch, released only after the runner returns), so two
// turns can never run concurrently against the same Host. If that
// invariant ever changes, this needs to become per-turn state instead of
// shared Host state — see build_prompt_PLANNING_ROUND2.md's "Before writing
// Host.SetPlanningContext" note.
type planningContext struct {
	planningID string
	boardID    string
}

func (c *planningContext) set(planningID, boardID string) {
	c.planningID = planningID
	c.boardID = boardID
}

func (c *planningContext) get() (planningID, boardID string) {
	return c.planningID, c.boardID
}
