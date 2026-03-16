package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/smith3v/dash14/pkg/app"
)

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		// parseOptions already wrote the error and usage to os.Stderr.
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "dash14: %v\n", err)
		os.Exit(1)
	}
}

// run is the real entry point for the application. It is separated from main
// so it can be tested without spawning a separate process and so that deferred
// cleanup runs before os.Exit is called.
func run(ctx context.Context, opts Options) error {
	return app.Run(ctx, app.Options{
		ConfigPath: opts.ConfigPath,
		ImportPath: opts.ImportPath,
	})
}
