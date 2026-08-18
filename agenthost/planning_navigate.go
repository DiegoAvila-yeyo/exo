package agenthost

// NavigateAction is what a navigation tool (planning_open,
// planning_create_board_and_open, planning_create_planning_and_open —
// see planning_navigate_tools.go) commits when it successfully resolves or
// creates a destination. It carries no client_id/session_id/turn_id — those
// are HTTP-request-level concerns the caller (backend.go's runner closure)
// combines this with after Host.Run returns; Host itself only ever knows
// "which Planning/Board," never which browser tab asked.
type NavigateAction struct {
	PlanningID   string
	PlanningName string
	BoardID      string // empty means "land planning-only, no Board open"
	BoardName    string
}

// navigateSlot is where a navigation tool commits its result for the
// current turn, and where the current turn's human message text lives for
// the explicit-naming check (planning_navigate_tools.go's nameMentioned).
// Host constructs a fresh one for every turn (BeginTurn) — unlike
// planningContext (planning_context.go), which is deliberately long-lived
// Host state reused turn to turn, this must never survive into the next
// turn, so a forgotten reset can't leak a stale navigation forward.
type navigateSlot struct {
	humanMessage string
	action       *NavigateAction // nil until a tool commits one
}

// commit records a, unless something already committed this turn — at
// most one NavigateAction per turn, per Round 3's build prompt: the model
// doesn't get to change its mind mid-turn about where to send the user.
func (s *navigateSlot) commit(a *NavigateAction) bool {
	if s.action != nil {
		return false
	}
	s.action = a
	return true
}

// navigateCell is the long-lived indirection the three navigation tools
// are constructed with once, at Host creation (buildToolRegistry) — Host
// swaps cell.current to a fresh slot at the start of every turn
// (BeginTurn), so the tools — which hold this same cell pointer for the
// Host's entire lifetime — always read whatever slot is current without
// needing to be reconstructed per turn.
type navigateCell struct {
	current *navigateSlot
}

func newNavigateCell() *navigateCell {
	return &navigateCell{current: &navigateSlot{}}
}

// BeginTurn resets the per-turn navigation state — must never leak between
// turns. Call once per turn, before Run, alongside SetPlanningContext/
// SetRootPath. humanMessage is exactly what the human typed this turn
// (AgentRunner's input) — the three navigation tools check requested names
// against it before acting, per Round 3's "the agent never invents a
// destination" rule.
func (h *Host) BeginTurn(humanMessage string) {
	h.navigateCell.current = &navigateSlot{humanMessage: humanMessage}
}

// TakeNavigateAction returns whatever a navigation tool committed during
// the turn that just ran (nil if none). Call after Host.Run returns,
// regardless of whether Run itself errored — a tool's persisted side
// effect (a Board/Planning actually created) committed the instant that
// tool's Execute returned success, and a later, unrelated failure in the
// rest of the turn must not make it disappear. Despite the name, this does
// not clear anything — BeginTurn (the next turn) is what prevents
// staleness, not read-once semantics here.
func (h *Host) TakeNavigateAction() *NavigateAction {
	return h.navigateCell.current.action
}
