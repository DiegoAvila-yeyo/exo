package agenthost

import (
	"context"
	"io"
	"time"

	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/agent"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// ScaleCheckpointResult is CheckpointResult plus what Experimento 1-bis Q1+Q2
// needs on top: the catalog size and target position for this corrida (so
// the report can check for a position pattern independent of N — see
// build_prompt_YEYO_Q1Q2.md, "Medición"), and token/latency cost.
type ScaleCheckpointResult struct {
	CheckpointResult
	N              int             `json:"n"`
	Seed           int64           `json:"seed"`
	TargetPosition int             `json:"target_position"`     // 1-based, within the rendered index
	Positions      map[string]int  `json:"positions,omitempty"` // Q3C: position of every tracked atom, not just one target
	Elapsed        time.Duration   `json:"elapsed_ns"`
	Usage          api.Usage       `json:"usage"`
	Timeline       []TimelineEntry `json:"timeline"` // Q3B/Q3C: assistant text and tool calls in the order they actually happened
}

// TimelineEntry is one event in a corrida's chronological order — assistant
// text or a tool call — added for Q3B's "read what the model said right
// before it called atom get" measurement (the top-5-before-get ranking).
// Kind is "text" or "tool_call"; only the matching field is populated.
type TimelineEntry struct {
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput string `json:"tool_input,omitempty"`
}

// usageReporter mirrors provider/fallback.go's local interface — providers
// that track cumulative token usage implement TotalUsage(). Not every
// provider.Provider does (only the ones checkpoint.go's buildProviderFromEnv
// can construct here do), so this is a soft type-assertion, not a hard
// dependency.
type usageReporter interface {
	TotalUsage() api.Usage
}

// RunScaleCheckpoint runs the same K2 shape RunCheckpoint validated
// (atoms_decision(inspect|skip) as Phase 1's only tool, index delivered
// inside the inspect result, free atom get after) over a size-N synthetic
// catalog built by buildScaleCatalog(n, seed) instead of the real yeyo
// periferia. See checkpoint.go's RunCheckpoint doc-comment for why this
// drives agent.Agent directly rather than going through
// runtime.Coordinator — same reasoning applies unchanged here.
func RunScaleCheckpoint(ctx context.Context, task string, trace io.Writer, n int, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildScaleCatalog(n, seed), "", 0)
}

// RunQ3Checkpoint is RunScaleCheckpoint's Q3 twin: same K2 shape, over a
// catalog built by buildQ3Catalog(nNeighbors, seed) instead — target is
// worktrees-not-code-dir, N fixed at 50, with nNeighbors near-neighbor
// distractors mixed in among the clean ones.
func RunQ3Checkpoint(ctx context.Context, task string, trace io.Writer, nNeighbors int, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildQ3Catalog(nNeighbors, seed), "", 0)
}

// q3bRankingInstruction is Q3B's cheap top-5-before-get measurement
// (build prompt for Q3B, "Medición" — Codex's suggestion). Appended to the
// "inspect" tool result only for this round, never to the mandatory
// atoms_decision gate itself — the gate's schema/enum stays exactly as
// validated, this only adds a sentence to the free-text Phase 2 the model
// already has after inspect.
const q3bRankingInstruction = "Before calling atom get, reply in text with your top-5 candidate atom names from the catalog above, ranked from most to least likely to be relevant to this task. Then call atom get for the one you think is correct."

// RunQ3BCheckpoint is RunScaleCheckpoint's Q3B twin: same K2 shape, over the
// fixed N=50 catalog buildQ3BCatalog(seed) builds (target + 3 functionally
// redundant atoms + clean filler), plus the top-5-before-get instruction
// appended to the inspect result.
func RunQ3BCheckpoint(ctx context.Context, task string, trace io.Writer, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildQ3BCatalog(seed), q3bRankingInstruction, 0)
}

// RunQ3CCheckpoint is RunScaleCheckpoint's Q3C twin: same K2 shape, over one
// of buildQ3CCatalog's 5 conditions (C0..C4), with the same top-5-before-get
// instruction Q3B introduced — Q3C needs the same "what did it rank first"
// signal, now over two atoms that genuinely disagree instead of four that
// agree.
func RunQ3CCheckpoint(ctx context.Context, task string, trace io.Writer, condition string, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildQ3CCatalog(condition, seed), q3bRankingInstruction, 0)
}

// RunQ4Checkpoint is RunScaleCheckpoint's Q4 twin: same K2 shape, over one of
// buildQ4Catalog's 4 conditions (Q4-A..Q4-D), with the same top-5-before-get
// instruction Q3B/Q3C introduced — Q4 needs the same "what did it rank
// first" signal, now over three atoms in a specializes/exception_of chain
// instead of two atoms in flat conflict.
func RunQ4Checkpoint(ctx context.Context, task string, trace io.Writer, condition string, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildQ4Catalog(condition, seed), q3bRankingInstruction, 0)
}

// RunQ6Checkpoint is RunScaleCheckpoint's Q6 twin: same K2 shape, over
// buildQ6Catalog(seed) — 3 atoms of the same type, all genuinely applicable
// to the same task, with no authority/specificity/exception relation between
// them. No extra instruction (no top-5-before-get ranking): Q6 measures the
// *set* of atom get calls the model actually makes, not which one it ranks
// first among competitors — there's no single "correct" pick here.
func RunQ6Checkpoint(ctx context.Context, task string, trace io.Writer, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildQ6Catalog(seed), "", 0)
}

// RunQ6BCheckpoint is Q6's twin over a real fixture instead of a
// hypothetical file: same catalog (buildQ6Catalog(seed), same 3 atoms, same
// no-precedence-metadata design), but the task now points at a real
// handlers.go under projectRoot that the model actually reads, splits, and
// (per the Q6 report's documented gap) whose diff can be checked for the
// three properties afterward. Needs more than the K2 default of 20 turns —
// real file editing (read, multiple writes, a caller package to keep
// compiling, doc updates) takes more steps than a hypothetical task that
// stops at "which atoms are relevant". maxTurns is fixed at 40, double the
// default, chosen after rep1 hit the 20-turn ceiling mid-edit; not tuned
// further per-corrida to keep the 5 repetitions comparable.
func RunQ6BCheckpoint(ctx context.Context, task string, trace io.Writer, seed int64) (ScaleCheckpointResult, error) {
	return runCatalogCheckpoint(ctx, task, trace, buildQ6Catalog(seed), "", 40)
}

// runCatalogCheckpoint is the shared runner behind RunScaleCheckpoint,
// RunQ3Checkpoint, and RunQ3BCheckpoint — all three need the exact same
// tool wiring and measurement, just over a differently-built scaleCatalog
// and (Q3B only) an extra instruction appended to the inspect result.
// maxTurns overrides agent.New's default (20) when non-zero — added for
// Q6B, which needs real multi-step file editing instead of a short
// gate+get+respond exchange; every earlier round passes 0 and keeps the
// original default unchanged.
func runCatalogCheckpoint(ctx context.Context, task string, trace io.Writer, catalog scaleCatalog, extraInstruction string, maxTurns int) (ScaleCheckpointResult, error) {
	systemPrompt := "You are exo's integrated coding agent. Help the user from the browser chat, prefer safe tool use, and explain failures clearly."

	provider, err := buildProviderFromEnv(systemPrompt)
	if err != nil {
		return ScaleCheckpointResult{}, err
	}

	withoutAtomReg := nbtool.NewRegistry()
	withoutAtomReg.ReplaceFrom(nbtool.Default)

	withAtomReg := nbtool.NewRegistry()
	withAtomReg.ReplaceFrom(nbtool.Default)
	withAtomReg.Register(atomToolScale{catalog: catalog})

	phase1Reg := nbtool.NewRegistry()
	result := &CheckpointResult{}
	decision := atomsDecisionToolScale{
		registry:         phase1Reg,
		withAtom:         withAtomReg,
		withoutAtom:      withoutAtomReg,
		onDecision:       func(action string) { result.Decision = action },
		catalog:          catalog,
		extraInstruction: extraInstruction,
	}
	phase1Reg.Register(decision)

	a := agent.New(provider, systemPrompt, phase1Reg)
	if maxTurns > 0 {
		a.MaxTurns = maxTurns
	}

	var timeline []TimelineEntry
	hooks := &agent.Hooks{
		OnAssistantText: func(text string) {
			timeline = append(timeline, TimelineEntry{Kind: "text", Text: text})
		},
		OnToolCall: func(name, rawInput string) {
			result.ToolCalls = append(result.ToolCalls, CheckpointToolCall{Name: name, Input: rawInput})
			timeline = append(timeline, TimelineEntry{Kind: "tool_call", ToolName: name, ToolInput: rawInput})
		},
		OnToolResult: func(name, res string, isErr bool) {
			if n := len(result.ToolCalls); n > 0 {
				result.ToolCalls[n-1].Result = res
				result.ToolCalls[n-1].IsError = isErr
			}
		},
	}

	var usageBefore api.Usage
	if ur, ok := provider.(usageReporter); ok {
		usageBefore = ur.TotalUsage()
	}

	start := time.Now()
	var finalText string
	if trace != nil {
		restore, redirErr := redirectStdout(trace)
		if redirErr != nil {
			return ScaleCheckpointResult{}, redirErr
		}
		finalText, err = a.SendWithHooks(ctx, task, hooks)
		restore()
	} else {
		finalText, err = a.SendWithHooks(ctx, task, hooks)
	}
	elapsed := time.Since(start)

	result.FinalText = finalText

	var usage api.Usage
	if ur, ok := provider.(usageReporter); ok {
		after := ur.TotalUsage()
		usage = api.Usage{
			InputTokens:         after.InputTokens - usageBefore.InputTokens,
			OutputTokens:        after.OutputTokens - usageBefore.OutputTokens,
			CacheCreationTokens: after.CacheCreationTokens - usageBefore.CacheCreationTokens,
			CacheReadTokens:     after.CacheReadTokens - usageBefore.CacheReadTokens,
		}
	}

	return ScaleCheckpointResult{
		CheckpointResult: *result,
		N:                catalog.N,
		Seed:             catalog.Seed,
		TargetPosition:   catalog.TargetPosition,
		Positions:        catalog.Positions,
		Elapsed:          elapsed,
		Usage:            usage,
		Timeline:         timeline,
	}, err
}
