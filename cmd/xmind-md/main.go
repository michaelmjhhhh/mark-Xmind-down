package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/app"
)

// Set with -ldflags "-X main.version=v1.0.0" for release builds.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(app.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}
