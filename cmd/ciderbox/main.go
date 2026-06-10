package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mentholmike/ciderbox/internal/cli"
	_ "github.com/mentholmike/ciderbox/internal/providers/all"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	if err := cli.Run(ctx, os.Args[1:]); err != nil {
		var exit cli.ExitError
		if cli.AsExitError(err, &exit) {
			if exit.Message != "" {
				fmt.Fprintln(os.Stderr, exit.Message)
			}
			os.Exit(exit.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
