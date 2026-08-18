// Command q3bcall drives agenthost.RunQ3BCheckpoint from argv, for
// Experimento 1-bis Q3B's corridas — same throwaway-experiment-tooling
// pattern as cmd/q3call (Q3), cmd/scalecall (Q1+Q2), cmd/checkpointcall
// (exp1k/exp1k2), and cmd/rawcall (exp1h). Prints one JSON object per
// corrida to stdout. Not part of exo's normal build/install.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
)

type logEntry struct {
	Task           string                         `json:"task"`
	Expected       string                         `json:"expected"`
	Message        string                         `json:"message"`
	Seed           int64                          `json:"seed"`
	TargetPosition int                            `json:"target_position"`
	Decision       string                         `json:"decision"`
	ToolCalls      []agenthost.CheckpointToolCall `json:"tool_calls"`
	Timeline       []agenthost.TimelineEntry      `json:"timeline"`
	FinalText      string                         `json:"final_text"`
	ElapsedMs      int64                          `json:"elapsed_ms"`
	InputTokens    int                            `json:"input_tokens"`
	OutputTokens   int                            `json:"output_tokens"`
	CacheCreation  int                            `json:"cache_creation_tokens"`
	CacheRead      int                            `json:"cache_read_tokens"`
	Trace          string                         `json:"trace"`
	Error          string                         `json:"error,omitempty"`
}

func main() {
	if len(os.Args) != 6 {
		fmt.Fprintln(os.Stderr, "usage: q3bcall <seed> <task-label> <expected yes|no> <user-message> <project-root-abs-path>")
		os.Exit(1)
	}
	seed, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid seed %q: must be an integer\n", os.Args[1])
		os.Exit(1)
	}
	task, expected, message, rootPath := os.Args[2], os.Args[3], os.Args[4], os.Args[5]

	envPath, err := appconfig.EnvFilePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve env file:", err)
		os.Exit(1)
	}
	if err := appconfig.LoadEnvFile(envPath); err != nil {
		fmt.Fprintln(os.Stderr, "load env file:", err)
		os.Exit(1)
	}

	if strings.TrimSpace(rootPath) == "" {
		fmt.Fprintln(os.Stderr, "project-root-abs-path must not be empty")
		os.Exit(1)
	}
	if err := os.Chdir(filepath.Clean(rootPath)); err != nil {
		fmt.Fprintln(os.Stderr, "chdir to root path:", err)
		os.Exit(1)
	}

	var trace bytes.Buffer
	result, runErr := agenthost.RunQ3BCheckpoint(context.Background(), message, &trace, seed)

	entry := logEntry{
		Task:           task,
		Expected:       expected,
		Message:        message,
		Seed:           result.Seed,
		TargetPosition: result.TargetPosition,
		Decision:       result.Decision,
		ToolCalls:      result.ToolCalls,
		Timeline:       result.Timeline,
		FinalText:      result.FinalText,
		ElapsedMs:      result.Elapsed.Milliseconds(),
		InputTokens:    result.Usage.InputTokens,
		OutputTokens:   result.Usage.OutputTokens,
		CacheCreation:  result.Usage.CacheCreationTokens,
		CacheRead:      result.Usage.CacheReadTokens,
		Trace:          trace.String(),
	}
	if runErr != nil {
		entry.Error = runErr.Error()
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entry); err != nil {
		fmt.Fprintln(os.Stderr, "encode result:", err)
		os.Exit(1)
	}
	if runErr != nil {
		os.Exit(1)
	}
}
