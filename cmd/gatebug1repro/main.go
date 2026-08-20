// Command gatebug1repro reproduces (and, after the fix, verifies) Bug 1 from
// build_prompt_YEYO_FIX_AMBOS.md: the atoms_decision gate losing context
// between turns when a short follow-up message ("la ruta es X") doesn't
// repeat the original request. Same pattern as cmd/gatesmoke — throwaway,
// not part of exo's normal build/install.
//
// Turn 1 mirrors uso-real-report.md's Conversación 11: ask to add a
// diagnostic function to "the same file, no new file", without naming the
// file. Turn 2 answers with just the path — agenthost/host.go, a real file
// in this repo already at 599 lines (verifiably over protocolo-hulk's 300
// line limit before the requested change even lands).
//
// A second, unrelated pair of turns (-control flag) exercises the genuine
// topic-change case: code question, then an unrelated question — confirms
// the fix doesn't leave the gate stuck on the first task forever.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/sessions"
)

func main() {
	control := flag.Bool("control", false, "run the topic-change control conversation instead of the bug repro")
	flag.Parse()

	if err := os.Setenv("EXO_YEYO_GATE", "1"); err != nil {
		fmt.Fprintln(os.Stderr, "setenv EXO_YEYO_GATE:", err)
		os.Exit(1)
	}
	// Respects a caller-provided EXO_AGENT_ROOT_PATH (point this at a
	// throwaway copy of the repo, not ~/exo itself — turn 2 of the repro
	// intentionally makes the agent believe it should go implement the
	// requested function, and it has real bash/write_file tools once the
	// gate opens Phase 2; letting that land in the tracked working tree is
	// exactly the "unexpected scope creep" risk uso-real-report.md
	// documented, not something to reproduce by accident here too).
	if strings.TrimSpace(os.Getenv("EXO_AGENT_ROOT_PATH")) == "" {
		if err := os.Setenv("EXO_AGENT_ROOT_PATH", os.ExpandEnv("$HOME/exo")); err != nil {
			fmt.Fprintln(os.Stderr, "setenv EXO_AGENT_ROOT_PATH:", err)
			os.Exit(1)
		}
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

	manager := sessions.New()
	host, err := agenthost.New(context.Background(), manager, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenthost.New:", err)
		os.Exit(1)
	}
	defer host.Close()

	var turns []string
	var sessionName string
	if *control {
		sessionName = "gatebug1repro-control"
		turns = []string{
			"¿por qué en Go se prefiere composición sobre herencia?",
			"cambiando de tema — necesito el mensaje de commit para un fix de parseo de fechas",
		}
	} else {
		sessionName = "gatebug1repro-critical"
		turns = []string{
			"Necesito agregar una función de diagnóstico bien detallada, con comentarios, ahí mismo, en el mismo archivo, sin crear uno nuevo.",
			"la ruta es agenthost/host.go",
		}
	}

	sessionCtx := agenthost.ContextWithSessionID(context.Background(), sessionName)
	for i, msg := range turns {
		fmt.Printf("\n===== turno %d: %q =====\n", i+1, msg)
		var out strings.Builder
		if err := host.Run(sessionCtx, msg, &out); err != nil {
			fmt.Fprintf(os.Stderr, "turno %d: Run: %v\n", i+1, err)
			os.Exit(1)
		}
		fmt.Println(out.String())
	}
}
