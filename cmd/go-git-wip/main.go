package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/anzax/go-git-wip/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Run(ctx, version, os.Args[1:], os.Stdout, os.Stderr))
}
