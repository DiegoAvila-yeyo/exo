package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/DiegoAvila-yeyo/exo/backend"
	"github.com/DiegoAvila-yeyo/exo/cli"
	"github.com/DiegoAvila-yeyo/exo/singleton"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, singleton.ErrLeaseHeld) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(backend.ErrSecondInstanceExitCode)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: exo <install|uninstall|status|restart|serve>")
	}

	command := cli.New()
	ctx := context.Background()

	switch os.Args[1] {
	case "install":
		return command.Install(ctx)
	case "uninstall":
		return command.Uninstall(ctx)
	case "status":
		status, err := command.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("installed=%t running=%t sessions=%d\n", status.Installed, status.Running, status.SessionCount)
		return nil
	case "restart":
		return command.Restart(ctx)
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return backend.Run(ctx, backend.DefaultConfig())
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}
