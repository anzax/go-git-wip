package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/anzax/go-git-wip/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.NewCommand(version).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
