package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// atomsDecisionToolScale is atomsDecisionTool's twin for Experimento 1-bis
// Q1+Q2: identical schema and behavior (see atoms_decision_tool.go's
// doc-comment for the design rationale, unchanged here), except the
// "inspect" branch embeds a pre-built, pre-shuffled scaleCatalog's index
// instead of calling yeyo.RenderIndex(). This round only ever runs the K2
// shape (index delivered inside the inspect result) — see
// build_prompt_YEYO_Q1Q2.md, "Mecanismo ya validado, no lo toques": K1a's
// separate-index-lookup variant isn't part of this question.
type atomsDecisionToolScale struct {
	registry    *nbtool.Registry
	withAtom    *nbtool.Registry
	withoutAtom *nbtool.Registry
	onDecision  func(action string)
	catalog     scaleCatalog

	// extraInstruction, if non-empty, is appended to the "inspect" result
	// after the index — Q3B's mechanism for the cheap top-5-before-get
	// measurement Codex suggested (build prompt for Q3B, "Medición"). Empty
	// for every other round (Q1+Q2, Q3): the harness is allowed to
	// structure *when*/*how* the decision gets asked, per
	// docs/vision.md's "criterio de éxito redefinido" — it still never
	// picks or ranks candidates itself, it only asks the model to say its
	// ranking out loud before acting on it.
	extraInstruction string
}

func (t atomsDecisionToolScale) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "atoms_decision",
		Description: "Mandatory first step for this task, before any other action. " +
			"Decide whether this project's local rule catalog (yeyo atoms — " +
			"conventions specific to this codebase, not general best practice) " +
			"might contain something relevant here, beyond your own general " +
			"knowledge. Call this exactly once. action=\"inspect\": opens the " +
			"atom catalog tools (atom get) in addition to your normal " +
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

func (t atomsDecisionToolScale) Execute(_ context.Context, rawInput string) (string, bool) {
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
		msg := "recorded: inspect. atom get <name> is now available alongside your normal tools for this task — no need to call atom list separately, here is the catalog already:\n\n" + t.catalog.IndexText
		if t.extraInstruction != "" {
			msg += "\n\n" + t.extraInstruction
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

// atomToolScale is atomTool's twin: same "get <name>" shape, but resolves
// against a scaleCatalog's synthetic+real atom pool instead of the yeyo
// package directly. "list" is kept only for parity/robustness in case the
// model calls it out of habit — the index is already in the inspect result,
// same as the real K2 mechanism, so nothing depends on "list" being called.
type atomToolScale struct {
	catalog scaleCatalog
}

func (t atomToolScale) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "atom",
		Description: "Navigate the yeyo atom catalog of behavioral guidance. " +
			"Call with action \"list\" to see the periferia catalog (name + " +
			"description of every entry), then action \"get\" with an exact " +
			"name to load that atom's full instructions before acting on a " +
			"matching task.",
		InputSchema: map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "\"list\" to see the catalog, or \"get\" to fetch one atom.",
				"enum":        []string{"list", "get"},
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Exact atom name. Required when action is \"get\".",
			},
		},
		Required: []string{"action"},
	}
}

func (t atomToolScale) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}

	switch strings.TrimSpace(in.Action) {
	case "", "list":
		return t.catalog.IndexText, false
	case "get":
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return "action \"get\" requires a \"name\"", true
		}
		atom, ok := t.catalog.get(name)
		if !ok {
			return fmt.Sprintf("atom %q not found. Call atom list to see available names.", name), true
		}
		fmt.Printf("→ atom usado: %s\n", atom.Name)
		return atom.Body, false
	default:
		return fmt.Sprintf("unknown action %q, expected \"list\" or \"get\"", in.Action), true
	}
}
