package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yeyoos/nucleo-base/shared/api"
	"github.com/yeyoos/yeyo"

	"github.com/DiegoAvila-yeyo/exo/yeyotelemetry"
)

// This file is the consumer of the atoms_decision gate's measurement seam
// (agenthost/atoms_decision_tool.go's onDecision, and a wrapper added here
// for atom_tool.go's "get" — see atomGetTelemetryTool's doc comment for why
// atom_tool.go itself needed no change). Everything here is a no-op when
// EXO_YEYO_GATE is off or the telemetry store failed to open — see Host.Run
// and openTelemetryStoreBestEffort.

// sessionIDContextKey is how a session id set by termserver/chat.go on the
// context passed into Host.Run survives down to wherever the gate's tools
// actually execute (agent.go's execCtx derives from the same ctx chain via
// capture.Bind, which preserves context.WithValue data — verified by
// reading nucleo-base/layer2-runtime-rails/capture/context.go before
// relying on it). Not exported as a raw type to avoid collisions with
// other packages' context keys.
type sessionIDContextKey struct{}

// ContextWithSessionID attaches a chat session id to ctx, for Host.Run to
// read back out (via sessionIDFromContext) and stamp onto every telemetry
// event this turn produces. Safe to call with an empty id — telemetry
// events just get session_id="" (e.g. a non-persisted chat, chatStore ==
// nil), same as any other optional correlation field elsewhere in this
// codebase.
func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

func sessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(sessionIDContextKey{}).(string)
	return id
}

// gateTurnState is the per-turn mutable state the gate's telemetry closures
// read from — same shape/purpose as navigateCell (planning_navigate.go):
// atomsDecisionTool and atomGetTelemetryTool are constructed once, in New,
// long before any particular turn's sessionID/turnID exist, so the
// closures close over a *Host and read this field fresh on every call
// instead of capturing per-turn values directly.
type gateTurnState struct {
	sessionID string
	turnID    string
	atomSeq   int // next atom_get sequence number to assign, 1-based
}

// beginGateTurn resets gateTurnState for the turn about to run and returns
// the fresh turnID, for Run to use later when recording the turn_result
// event. Only meaningful when the gate is enabled; harmless no-op
// otherwise (h.gateTurn stays nil).
func (h *Host) beginGateTurn(ctx context.Context) string {
	if !h.gateEnabled {
		return ""
	}
	turnID := uuid.NewString()
	h.gateTurn = &gateTurnState{
		sessionID: sessionIDFromContext(ctx),
		turnID:    turnID,
	}
	return turnID
}

// previousRawInputForSession returns the raw (undecorated) user input the
// last turn of session sessionID sent, or "" if this is the first turn of
// the session (or the gate is off, or sessionID is "" — an unpersisted
// chat, see ContextWithSessionID). rememberRawInputForSession is how it
// gets populated, right before Run hands the turn to the coordinator.
//
// Why a small per-session map here, tracking raw input directly, instead of
// the tasks_file/progress task-tracking Coordinator already has (ensureTask,
// runtime.Coordinator.BootstrapTools) — investigated first, per
// build_prompt_YEYO_FIX_AMBOS.md's instruction not to reinvent it, and it
// doesn't fit: ensureTask's task-reuse branch (nucleo-base/layer2-runtime-
// rails/runtime/coordinator_task.go) only activates when Coordinator.Capture
// is non-nil, and nothing in exo ever sets it (agent.New/runtime.
// NewCoordinator leave it nil) — every turn today takes ensureTask's other
// branch, minting a brand-new adhoc-<timestamp> task from just that turn's
// raw input. That's the same bug in different clothes: a task "description"
// derived from ensureTask would be "la ruta es agenthost/host.go" on turn 2,
// not the original ask. Wiring Coordinator.Capture would fix task ID reuse,
// but ActiveTaskID is only ever cleared by an explicit Recorder.Reset()
// (meant for "/tab new") — nothing in Coordinator detects a task finishing
// or the conversation genuinely changing topic mid-session, so once wired it
// would pin the gate to the *first* task of a session forever, failing this
// bug's own control case (topic genuinely changes — see gatebug1repro
// -control). One bounded turn of raw-input lookback doesn't have that
// failure mode: a topic change just replaces what "previous message" is,
// every turn, with no completion/reset logic needed.
//
// A first version of this fix tried surfacing the same signal through the
// gate tool's own Definition().Description instead of the conversation
// content — cheaper (no change to what reaches the model as a real
// message), and it does reach the model, but reproducing the exact
// conversation from uso-real-report.md's Conversación 11 with that version
// in place still produced "skip" (see
// experiments/fix-contexto-y-lexico-report.md's Bug 1 section for the raw
// run) — tool-schema text evidently doesn't get the same weight as an
// actual turn of conversation. This version instead folds it into the
// prompt string Run hands to coordinator.Run, the same mechanism
// nucleo-base's own decoratePrompt uses for "[ACTIVE TASK]"/"[PLAN
// SNAPSHOT]" blocks — content the model actually reads as part of the turn,
// not metadata about the tools available.
func (h *Host) previousRawInputForSession(sessionID string) string {
	if h == nil || !h.gateEnabled || sessionID == "" {
		return ""
	}
	h.gateLastInputMu.Lock()
	defer h.gateLastInputMu.Unlock()
	return h.gateLastInput[sessionID]
}

// rememberRawInputForSession records input (as received by Run, before any
// decoration) as this session's most recent turn, for the *next* turn's
// previousRawInputForSession to read. Deliberately stores the raw string,
// never the gate-augmented one Run actually sends to the coordinator —
// storing the augmented version would nest a growing chain of "[PREVIOUS
// MESSAGE]" blocks inside each other, turn after turn.
func (h *Host) rememberRawInputForSession(sessionID, input string) {
	if h == nil || !h.gateEnabled || sessionID == "" {
		return
	}
	h.gateLastInputMu.Lock()
	defer h.gateLastInputMu.Unlock()
	if h.gateLastInput == nil {
		h.gateLastInput = make(map[string]string)
	}
	h.gateLastInput[sessionID] = input
}

// gatePreviousMessageBlock wraps prev (a previous turn's raw input) into the
// "[PREVIOUS MESSAGE]" block Run prepends to this turn's input when the gate
// is on — see previousRawInputForSession's doc comment for why this exists
// and why it's scoped to conversation content, not tool metadata. Returns ""
// for an empty prev (first turn of a session), so callers can no-op cleanly.
func gatePreviousMessageBlock(prev string) string {
	prev = truncateRunes(prev, 400)
	if prev == "" {
		return ""
	}
	return "[PREVIOUS MESSAGE — context only, may or may not still apply]\n" + prev
}

// truncateRunes trims s to at most n runes, appending "..." when it does.
func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// recordGateDecision is atomsDecisionTool's onDecision hook. yeyo.Periferia
// is a pure function of the embedded catalog (loaded once at package init
// — see ~/yeyo/catalog.go), so it's safe to call here at decision time
// regardless of which turn/session this is; only sessionID/turnID vary per
// turn.
func (h *Host) recordGateDecision(action string) {
	if h.telemetry == nil || h.gateTurn == nil {
		return
	}
	periferia := yeyo.Periferia()
	snapshot := make([]yeyotelemetry.AtomSnapshot, len(periferia))
	for i, a := range periferia {
		snapshot[i] = yeyotelemetry.AtomSnapshot{
			Name:        a.Name,
			ContentHash: yeyotelemetry.ContentHash(a.Body),
		}
	}
	indexTextBytes := len(yeyo.RenderIndex())
	if err := h.telemetry.RecordDecision(h.gateTurn.sessionID, h.gateTurn.turnID, action, snapshot, indexTextBytes); err != nil {
		fmt.Printf("agenthost: yeyo telemetry: record decision: %v\n", err)
	}
}

// recordGateAtomGet is called by atomGetTelemetryTool after a successful
// "get". Assigns and persists the next sequence number for this turn.
func (h *Host) recordGateAtomGet(atomName, body string) {
	if h.telemetry == nil || h.gateTurn == nil {
		return
	}
	h.gateTurn.atomSeq++
	seq := h.gateTurn.atomSeq
	contentHash := yeyotelemetry.ContentHash(body)
	if err := h.telemetry.RecordAtomGet(h.gateTurn.sessionID, h.gateTurn.turnID, atomName, contentHash, seq); err != nil {
		fmt.Printf("agenthost: yeyo telemetry: record atom_get: %v\n", err)
	}
}

// recordGateTurnResult is called by Run after the turn finishes, regardless
// of outcome. turnID is threaded in explicitly (rather than read back off
// h.gateTurn) because Run's own gate-only registry reset for the *next*
// turn could race a caller that inspects h.gateTurn concurrently — passing
// the value already captured at beginGateTurn avoids that.
func (h *Host) recordGateTurnResult(sessionID, turnID, result string, messageIndexBefore int) {
	if h.telemetry == nil || turnID == "" {
		return
	}
	if err := h.telemetry.RecordTurnResult(sessionID, turnID, result, messageIndexBefore); err != nil {
		fmt.Printf("agenthost: yeyo telemetry: record turn_result: %v\n", err)
	}
}

// atomGetTelemetryTool wraps atomTool to observe successful "get" calls
// without touching atom_tool.go — that file was explicitly out of scope
// for this build (see build_prompt_YEYO_FASEA_TELEMETRIA.md). It delegates
// every actual list/get to the real atomTool and only reads the request/
// response to log; it never reimplements the catalog logic itself.
//
// Finding worth flagging: unlike atomsDecisionTool, atom_tool.go's
// atomTool has no onGet-style hook at all — only a stdout print
// ("→ atom usado: %s"). The migration summary that mentioned "onDecision/
// onGet, ambos vacíos por ahora" was only half right: onDecision exists as
// a real field, onGet does not exist anywhere in atom_tool.go. This
// wrapper is how that gap gets closed without editing the restricted file.
type atomGetTelemetryTool struct {
	inner atomTool
	host  *Host
}

func (t atomGetTelemetryTool) Definition() api.ToolDef {
	return t.inner.Definition()
}

func (t atomGetTelemetryTool) Execute(ctx context.Context, rawInput string) (string, bool) {
	result, isErr := t.inner.Execute(ctx, rawInput)
	if isErr {
		return result, isErr
	}

	var in struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		// atomTool.Execute already validated this input successfully
		// (isErr is false), so a re-parse failure here would be a real bug
		// in this wrapper, not a bad model call — surfaced via stdout
		// instead of dropped silently, but the tool result itself is
		// unaffected (telemetry must never break the gate it's observing).
		fmt.Printf("agenthost: yeyo telemetry: re-parse atom tool input: %v\n", err)
		return result, isErr
	}
	if in.Action == "get" {
		t.host.recordGateAtomGet(in.Name, result)
	}
	return result, isErr
}
