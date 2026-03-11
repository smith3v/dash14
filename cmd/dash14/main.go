package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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
	if opts.ImportMode() {
		// TODO: load config, open DB, run importer, exit.
		fmt.Printf("import mode: config=%s import=%s\n", opts.ConfigPath, opts.ImportPath)
		return nil
	}

	// TODO: load config, init logging, open DB, start Telegram bot and overlay server.
	fmt.Printf("runtime mode: config=%s\n", opts.ConfigPath)

	// Block until the context is cancelled (SIGINT / SIGTERM).
	<-ctx.Done()
	return nil
}
