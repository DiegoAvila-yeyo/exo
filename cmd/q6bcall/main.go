// Command q6bcall drives agenthost.RunQ6BCheckpoint from argv, for
// Experimento 1-bis Q6b's corridas — Q6's composition question over a real
// fixture instead of a hypothetical file, so the diff can be checked for
// whether all 3 atoms' concerns survived to the final action, not just
// whether all 3 were fetched. Same throwaway-experiment-tooling pattern as
// cmd/q6call and every earlier round's driver. Prints one JSON object per
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
	Task          string                         `json:"task"`
	Message       string                         `json:"message"`
	Seed          int64                          `json:"seed"`
	Positions     map[string]int                 `json:"positions"`
	Decision      string                         `json:"decision"`
	ToolCalls     []agenthost.CheckpointToolCall `json:"tool_calls"`
	Timeline      []agenthost.TimelineEntry      `json:"timeline"`
	FinalText     string                         `json:"final_text"`
	ElapsedMs     int64                          `json:"elapsed_ms"`
	InputTokens   int                            `json:"input_tokens"`
	OutputTokens  int                            `json:"output_tokens"`
	CacheCreation int                            `json:"cache_creation_tokens"`
	CacheRead     int                            `json:"cache_read_tokens"`
	Trace         string                         `json:"trace"`
	Error         string                         `json:"error,omitempty"`
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: q6bcall <seed> <task-label> <user-message> <project-root-abs-path>")
		os.Exit(1)
	}
	seed, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid seed %q: must be an integer\n", os.Args[1])
		os.Exit(1)
	}
	task, message, rootPath := os.Args[2], os.Args[3], os.Args[4]

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
	result, runErr := agenthost.RunQ6BCheckpoint(context.Background(), message, &trace, seed)

	entry := logEntry{
		Task:          task,
		Message:       message,
		Seed:          result.Seed,
		Positions:     result.Positions,
		Decision:      result.Decision,
		ToolCalls:     result.ToolCalls,
		Timeline:      result.Timeline,
		FinalText:     result.FinalText,
		ElapsedMs:     result.Elapsed.Milliseconds(),
		InputTokens:   result.Usage.InputTokens,
		OutputTokens:  result.Usage.OutputTokens,
		CacheCreation: result.Usage.CacheCreationTokens,
		CacheRead:     result.Usage.CacheReadTokens,
		Trace:         trace.String(),
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
