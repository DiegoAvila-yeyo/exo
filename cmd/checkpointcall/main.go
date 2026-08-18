// Command checkpointcall drives agenthost.RunCheckpoint from argv, for
// exp1k's K1a and exp1k2's K2 checkpoint-gate corridas — the variant arg
// selects which. Prints one JSON object per corrida to stdout — same
// throwaway-experiment-tooling pattern as cmd/rawcall (exp1h) and the
// atom_tool.go/secret_tool.go additions of earlier rounds. Not part of exo's
// normal build/install.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
)

type logEntry struct {
	Variant   string                         `json:"variant"`
	Task      string                         `json:"task"`
	Expected  string                         `json:"expected"`
	Message   string                         `json:"message"`
	Decision  string                         `json:"decision"`
	ToolCalls []agenthost.CheckpointToolCall `json:"tool_calls"`
	FinalText string                         `json:"final_text"`
	Trace     string                         `json:"trace"`
	Error     string                         `json:"error,omitempty"`
}

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: checkpointcall <variant k1a|k2> <task-label> <expected yes|no> <user-message>")
		os.Exit(1)
	}
	variant := os.Args[1]
	var indexOnInspect bool
	switch variant {
	case "k1a":
		indexOnInspect = false
	case "k2":
		indexOnInspect = true
	default:
		fmt.Fprintf(os.Stderr, "unknown variant %q: must be \"k1a\" or \"k2\"\n", variant)
		os.Exit(1)
	}

	envPath, err := appconfig.EnvFilePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve env file:", err)
		os.Exit(1)
	}
	if err := appconfig.LoadEnvFile(envPath); err != nil {
		fmt.Fprintln(os.Stderr, "load env file:", err)
		os.Exit(1)
	}

	// Phase 2's normal tools (read_file, bash, fs_create, ...) resolve
	// relative paths — like the "yeyo/experiments/..." paths in the task
	// messages — against the process cwd, same reason agenthost.Host.New
	// chdirs before building its registry. RunCheckpoint doesn't do this
	// itself (it has no notion of "root path"), so this binary does it once
	// at startup instead.
	rootPath := strings.TrimSpace(os.Getenv("EXO_AGENT_ROOT_PATH"))
	if rootPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "resolve home dir:", err)
			os.Exit(1)
		}
		rootPath = home
	}
	if err := os.Chdir(filepath.Clean(rootPath)); err != nil {
		fmt.Fprintln(os.Stderr, "chdir to root path:", err)
		os.Exit(1)
	}

	task, expected, message := os.Args[2], os.Args[3], os.Args[4]
	var trace bytes.Buffer
	result, runErr := agenthost.RunCheckpoint(context.Background(), message, &trace, indexOnInspect)

	entry := logEntry{
		Variant:   variant,
		Task:      task,
		Expected:  expected,
		Message:   message,
		Decision:  result.Decision,
		ToolCalls: result.ToolCalls,
		FinalText: result.FinalText,
		Trace:     trace.String(),
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
