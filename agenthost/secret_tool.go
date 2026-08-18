package agenthost

import (
	"context"

	"github.com/yeyoos/nucleo-base/shared/api"
)

// secretValueTool exists only for exp1g's Experimento P (tool-aversion
// control): a trivial tool with a fixed return value that is not derivable
// or guessable any other way. If the model reliably calls it when asked for
// the exact value, that rules out a general aversion to tool use in exo —
// any zero rate found for the yeyo atom catalog would then be specific to
// the situation where a competing parametric answer already exists, not a
// tool-use configuration problem.
type secretValueTool struct{}

func (secretValueTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "secret_value",
		Description: "Returns a fixed secret value. There is no other way to know this value.",
		InputSchema: map[string]any{},
		Required:    []string{},
	}
}

func (secretValueTool) Execute(_ context.Context, _ string) (string, bool) {
	return "XYLOPHONE-7734", false
}
