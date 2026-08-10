package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wenzhe/astock-workbench/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "astock: %s\n", err)
		os.Exit(1)
	}
	if err := application.Run(ctx, os.Args[1:]); err != nil {
		application.PrintError(err)
		os.Exit(1)
	}
}
