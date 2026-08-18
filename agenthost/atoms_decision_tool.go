package agenthost

import (
	"context"
	"encoding/json"
	"fmt"

	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/shared/api"
	"github.com/yeyoos/yeyo"
)

// atomsDecisionTool is exp1k's K1a checkpoint gate. During Phase 1 it is the
// only tool the agent's registry holds — see RunCheckpoint. Its schema is
// deliberately just the enum, no "reason"/free-text field: a text field would
// give the model room to reconstruct in words the investigate-first loop
// this experiment is trying to isolate as a protocol action instead. See
// build_prompt_YEYO_EXP1K.md and docs/codex_consult_navegacion_libre.md
// (tercera consulta) in ~/yeyo for the design rationale.
//
// Execute mutates `registry` — the same *tool.Registry pointer the Agent
// holds — in place via ReplaceFrom. Because agent.Agent.loop reads
// a.Tools.Definitions() fresh every turn (not just once at construction),
// this is all that's needed to open Phase 2 on the very next turn: no
// separate wiring, no callback into the agent loop itself.
type atomsDecisionTool struct {
	registry    *nbtool.Registry
	withAtom    *nbtool.Registry
	withoutAtom *nbtool.Registry
	onDecision  func(action string) // measurement hook, may be nil

	// indexOnInspect is exp1k2's (K2) single change from K1a: when true, the
	// "inspect" result carries the full periferia index (the same content
	// atom's "list" action already returns) directly in its own tool result,
	// instead of leaving a separate atom-list lookup to compete against
	// "just act now that the normal tools are open" — exp1k found only 3/10
	// inspects followed through with atom get once Phase 2 opened both the
	// catalog and the normal tools together. K1a leaves this false.
	indexOnInspect bool
}

func (t atomsDecisionTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "atoms_decision",
		Description: "Mandatory first step for this task, before any other action. " +
			"Decide whether this project's local rule catalog (yeyo atoms — " +
			"conventions specific to this codebase, not general best practice) " +
			"might contain something relevant here, beyond your own general " +
			"knowledge. Call this exactly once. action=\"inspect\": opens the " +
			"atom catalog tools (atom list/get) in addition to your normal " +
			"tools, for the rest of this task. action=\"skip\": proceeds " +
			"directly with your normal tools, no catalog access this task.",
		InputSchema: map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "\"inspect\" to open the atom catalog, \"skip\" to proceed without it.",
				"enum":        []string{"inspect", "skip"},
			},
		},
		Required: []string{"action"},
	}
}

func (t atomsDecisionTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}

	switch in.Action {
	case "inspect":
		t.registry.ReplaceFrom(t.withAtom)
		if t.onDecision != nil {
			t.onDecision("inspect")
		}
		fmt.Println("→ atoms_decision: inspect")
		msg := "recorded: inspect. atom list/get are now available alongside your normal tools for this task."
		if t.indexOnInspect {
			msg = "recorded: inspect. atom get <name> is now available alongside your normal tools for this task — no need to call atom list separately, here is the catalog already:\n\n" + yeyo.RenderIndex()
		}
		return msg, false
	case "skip":
		t.registry.ReplaceFrom(t.withoutAtom)
		if t.onDecision != nil {
			t.onDecision("skip")
		}
		fmt.Println("→ atoms_decision: skip")
		return "recorded: skip. proceeding with your normal tools; no atom catalog access this task.", false
	default:
		return fmt.Sprintf("invalid action %q: must be \"inspect\" or \"skip\"", in.Action), true
	}
}
