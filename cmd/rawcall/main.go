// Command rawcall drives agenthost.RawSend from argv, for exp1h's raw
// single-pass comparison against the full Coordinator pipeline. Not part of
// exo's normal build/install — throwaway experiment tooling.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: rawcall <system-prompt> <user-message>")
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

	system := os.Args[1]
	user := os.Args[2]
	out, err := agenthost.RawSend(context.Background(), system, user)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rawcall error:", err)
		os.Exit(1)
	}
	fmt.Println(out)
}
