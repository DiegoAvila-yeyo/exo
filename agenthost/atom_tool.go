package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yeyoos/nucleo-base/shared/api"
	"github.com/yeyoos/yeyo"
)

// atomTool exposes the yeyo periferia catalog as a tool: "list" returns the
// lightweight index (name + description), "get" fetches one atom's full
// body. Mirrors the shape of nucleo-base's tool/skill.go (catalog + fetch by
// name) without importing or modifying it — yeyo lives in its own repo.
//
// Every successful "get" is logged to stdout with the same
// "→ phase: ..."-style marker the coordinator already uses for phases
// (runtime/coordinator_render.go). Host.Run redirects os.Stdout to the chat
// stream for the duration of a turn, so this line shows up there the same
// way phase markers do — no separate wiring needed in termserver/chat.go.
type atomTool struct{}

func (atomTool) Definition() api.ToolDef {
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

func (atomTool) Execute(_ context.Context, rawInput string) (string, bool) {
	var in struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}

	switch strings.TrimSpace(in.Action) {
	case "", "list":
		return yeyo.RenderIndex(), false
	case "get":
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return "action \"get\" requires a \"name\"", true
		}
		atom, ok := yeyo.Get(name)
		if !ok {
			return fmt.Sprintf("atom %q not found. Call atom list to see available names.", name), true
		}
		fmt.Printf("→ atom usado: %s\n", atom.Name)
		return atom.Body, false
	default:
		return fmt.Sprintf("unknown action %q, expected \"list\" or \"get\"", in.Action), true
	}
}
