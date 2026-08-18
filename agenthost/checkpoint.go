package agenthost

import (
	"context"
	"io"

	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/agent"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
)

// CheckpointToolCall records one tool invocation observed during a
// RunCheckpoint corrida, in the order it happened.
type CheckpointToolCall struct {
	Name    string `json:"name"`
	Input   string `json:"input"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// CheckpointResult is everything exp1k measures about one corrida.
type CheckpointResult struct {
	// Decision is "inspect", "skip", or "" if atoms_decision was never
	// called (the model answered with text, or hallucinated something
	// else — both show up as an empty Decision with ToolCalls/FinalText
	// to inspect).
	Decision  string               `json:"decision"`
	ToolCalls []CheckpointToolCall `json:"tool_calls"`
	FinalText string               `json:"final_text"`
}

// buildCheckpointToolsets constructs the three registries the checkpoint
// needs and wires the atoms_decision tool that swaps between them. Split out
// from RunCheckpoint so a test can assert on the registry contents directly
// — by code, not by asking the model — without touching the network.
//
// indexOnInspect selects K1a (false) vs K2 (true) — see atomsDecisionTool's
// indexOnInspect field for what changes.
func buildCheckpointToolsets(indexOnInspect bool) (phase1, withAtom, withoutAtom *nbtool.Registry, result *CheckpointResult) {
	withoutAtomReg := nbtool.NewRegistry()
	withoutAtomReg.ReplaceFrom(nbtool.Default)

	withAtomReg := nbtool.NewRegistry()
	withAtomReg.ReplaceFrom(nbtool.Default)
	withAtomReg.Register(atomTool{})

	phase1Reg := nbtool.NewRegistry()
	res := &CheckpointResult{}
	decision := atomsDecisionTool{
		registry:       phase1Reg,
		withAtom:       withAtomReg,
		withoutAtom:    withoutAtomReg,
		onDecision:     func(action string) { res.Decision = action },
		indexOnInspect: indexOnInspect,
	}
	phase1Reg.Register(decision)

	return phase1Reg, withAtomReg, withoutAtomReg, res
}

// RunCheckpoint runs exp1k's K1a design end to end for one task: Phase 1
// gives the model exactly one tool, atoms_decision(action: "inspect" |
// "skip") — no atom, no read_file, no bash, not even the tasks_file/progress
// plumbing tools that Exp1G's L kept registered (see agenthost/host.go,
// EXO_YEYO_JUDGE_ONLY). This deliberately does not go through
// runtime.Coordinator, which calls those plumbing tools itself via
// ensureTask before the model's first turn — RunCheckpoint drives
// agent.Agent directly instead, the minimal path that still gives the model
// real, schema-backed tool calling (unlike exp1h's RawSend, which offered
// none at all). See build_prompt_YEYO_EXP1K.md and
// docs/codex_consult_navegacion_libre.md (tercera consulta) in ~/yeyo.
//
// trace, if non-nil, receives the same human-readable "→ atoms_decision:
// ..." / "⚙️ tool: ..." / assistant-text stream that agent.Agent prints to
// stdout during the loop (agenthost/stdout.go's redirectStdout — same
// mechanism Host.Run already uses for the browser chat stream). Callers that
// also print a JSON summary to stdout should pass a non-nil trace, or the
// two outputs interleave on the same fd. Pass nil to leave stdout alone.
//
// indexOnInspect: false runs exp1k's K1a; true runs exp1k2's K2 (the
// periferia index is delivered inside the "inspect" tool result itself —
// see build_prompt_YEYO_EXP1K2.md and docs/codex_consult_navegacion_libre.md,
// cuarta consulta, in ~/yeyo).
func RunCheckpoint(ctx context.Context, task string, trace io.Writer, indexOnInspect bool) (CheckpointResult, error) {
	// No yeyo centro/index text here on purpose: those atoms tell the model
	// to call "atom list", a tool that doesn't exist yet in Phase 1 — keeping
	// them would contradict the very toolset the model is looking at. The
	// protocol comes entirely from what tool is available, not from text.
	systemPrompt := "You are exo's integrated coding agent. Help the user from the browser chat, prefer safe tool use, and explain failures clearly."

	provider, err := buildProviderFromEnv(systemPrompt)
	if err != nil {
		return CheckpointResult{}, err
	}

	phase1, _, _, result := buildCheckpointToolsets(indexOnInspect)

	a := agent.New(provider, systemPrompt, phase1)

	hooks := &agent.Hooks{
		OnToolCall: func(name, rawInput string) {
			result.ToolCalls = append(result.ToolCalls, CheckpointToolCall{Name: name, Input: rawInput})
		},
		OnToolResult: func(name, res string, isErr bool) {
			if n := len(result.ToolCalls); n > 0 {
				result.ToolCalls[n-1].Result = res
				result.ToolCalls[n-1].IsError = isErr
			}
		},
	}

	var finalText string
	if trace != nil {
		restore, redirErr := redirectStdout(trace)
		if redirErr != nil {
			return CheckpointResult{}, redirErr
		}
		finalText, err = a.SendWithHooks(ctx, task, hooks)
		restore()
	} else {
		finalText, err = a.SendWithHooks(ctx, task, hooks)
	}

	result.FinalText = finalText
	return *result, err
}
