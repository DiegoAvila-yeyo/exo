// Command q3call drives agenthost.RunQ3Checkpoint from argv, for
// Experimento 1-bis Q3's corridas — same throwaway-experiment-tooling
// pattern as cmd/scalecall (Q1+Q2), cmd/checkpointcall (exp1k/exp1k2), and
// cmd/rawcall (exp1h). Prints one JSON object per corrida to stdout. Not
// part of exo's normal build/install.
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
	Condition      string                         `json:"condition"` // D0/D1/D3/D7
	Task           string                         `json:"task"`
	Expected       string                         `json:"expected"`
	Message        string                         `json:"message"`
	NNeighbors     int                            `json:"n_neighbors"`
	Seed           int64                          `json:"seed"`
	TargetPosition int                            `json:"target_position"`
	Decision       string                         `json:"decision"`
	ToolCalls      []agenthost.CheckpointToolCall `json:"tool_calls"`
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
	if len(os.Args) != 8 {
		fmt.Fprintln(os.Stderr, "usage: q3call <condition-label> <n-neighbors 0|1|3|7> <seed> <task-label> <expected yes|no> <user-message> <project-root-abs-path>")
		os.Exit(1)
	}
	condition := os.Args[1]
	nNeighbors, err := strconv.Atoi(os.Args[2])
	if err != nil || nNeighbors < 0 || nNeighbors > 7 {
		fmt.Fprintf(os.Stderr, "invalid n-neighbors %q: must be 0, 1, 3, or 7\n", os.Args[2])
		os.Exit(1)
	}
	seed, err := strconv.ParseInt(os.Args[3], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid seed %q: must be an integer\n", os.Args[3])
		os.Exit(1)
	}
	task, expected, message, rootPath := os.Args[4], os.Args[5], os.Args[6], os.Args[7]

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
	result, runErr := agenthost.RunQ3Checkpoint(context.Background(), message, &trace, nNeighbors, seed)

	entry := logEntry{
		Condition:      condition,
		Task:           task,
		Expected:       expected,
		Message:        message,
		NNeighbors:     nNeighbors,
		Seed:           result.Seed,
		TargetPosition: result.TargetPosition,
		Decision:       result.Decision,
		ToolCalls:      result.ToolCalls,
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
