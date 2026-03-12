// Package logging constructs a structured *slog.Logger from application
// configuration. It abstracts the choice of handler, log level parsing, and
// optional file output so that the rest of the application only depends on
// *slog.Logger and never on handler internals.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/smith3v/dash14/config"
)

// New builds a *slog.Logger using cfg.
//
// Level parsing: "debug", "info", "warn", and "error" (all lowercase) are
// accepted. An empty or unrecognised level defaults to INFO so that
// misconfigured deployments still produce useful output rather than silently
// dropping messages.
//
// Output destination: when cfg.FilePath is non-empty the parent directory is
// created (os.MkdirAll) and the file is opened in append mode so that log
// data survives restarts. When cfg.FilePath is empty, os.Stderr is used.
//
// The returned cleanup function must be called by the caller (typically via
// defer) to close the underlying file. When logging to stderr the cleanup
// function is a no-op.
func New(cfg config.LoggingConfig) (*slog.Logger, func(), error) {
	level := parseLevel(cfg.Level)

	var w io.Writer
	cleanup := func() {}

	if cfg.FilePath != "" {
		dir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("logging: create log directory %q: %w", dir, err)
		}

		f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("logging: open log file %q: %w", cfg.FilePath, err)
		}

		w = f
		cleanup = func() { f.Close() }
	} else {
		w = os.Stderr
	}

	opts := &slog.HandlerOptions{Level: level}
	handler := slog.NewJSONHandler(w, opts)
	logger := slog.New(handler)

	return logger, cleanup, nil
}

// parseLevel maps a level string to the corresponding slog.Level value.
// Unknown and empty strings default to slog.LevelInfo.
func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}
