package agenthost

import (
	"context"
	"encoding/json"
	"fmt"

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
