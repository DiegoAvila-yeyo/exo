// Command gatesmoke drives agenthost.Host.Run through two turns of the same
// session with EXO_YEYO_GATE forced on, to verify the one property no
// checkpoint-based experiment could test: that the gate fires again on the
// second turn, not just the first. Throwaway smoke tool, same pattern as
// cmd/q6call and friends — not part of exo's normal build/install.
//
// Extended (Fase A telemetry build) to also stamp a fixed fake session id
// on each turn's context (agenthost.ContextWithSessionID) — same mechanism
// termserver/chat.go now uses in production — so a run of this tool
// produces real, query-able rows in yeyo_telemetry.db for verification.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/sessions"
)

func main() {
	if err := os.Setenv("EXO_YEYO_GATE", "1"); err != nil {
		fmt.Fprintln(os.Stderr, "setenv:", err)
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

	manager := sessions.New()
	host, err := agenthost.New(context.Background(), manager, nil, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenthost.New:", err)
		os.Exit(1)
	}
	defer host.Close()

	turns := []string{
		"ping",
		"¿tenés alguna convención sobre cómo escribir mensajes de commit en este proyecto?",
	}
	sessionCtx := agenthost.ContextWithSessionID(context.Background(), "gatesmoke-session-1")
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
