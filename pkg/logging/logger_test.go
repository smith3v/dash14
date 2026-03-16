package logging

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/smith3v/dash14/pkg/config"
)

// TestParseLevelKnown verifies that each accepted level string produces the
// expected slog.Level.
func TestParseLevelKnown(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseLevel(tc.input)
			if got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseLevelDefault verifies that an empty or unrecognised level string
// defaults to slog.LevelInfo.
func TestParseLevelDefault(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"unknown", "verbose"},
		{"mixed_case", "DEBUG"}, // we do not fold case; uppercase is unknown
		{"garbage", "42"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLevel(tc.input)
			if got != slog.LevelInfo {
				t.Errorf("parseLevel(%q) = %v, want LevelInfo", tc.input, got)
			}
		})
	}
}

// TestNewStderr verifies that New succeeds when no FilePath is configured and
// returns a non-nil logger together with a no-op cleanup function.
func TestNewStderr(t *testing.T) {
	cfg := config.LoggingConfig{Level: "info"}

	logger, cleanup, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}
	if cleanup == nil {
		t.Fatal("New() returned nil cleanup")
	}

	// cleanup should not panic when called (no-op for stderr)
	cleanup()
}

// TestNewFileOutput verifies that when FilePath is configured the logger writes
// JSON lines to that file, the parent directory is created automatically, and
// the cleanup function closes the file without error.
func TestNewFileOutput(t *testing.T) {
	dir := t.TempDir()
	// Use a subdirectory that doesn't yet exist to verify MkdirAll behaviour.
	logPath := filepath.Join(dir, "sub", "app.log")

	cfg := config.LoggingConfig{
		Level:    "debug",
		FilePath: logPath,
	}

	logger, cleanup, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}

	// Write a log entry and then flush/close via cleanup.
	logger.Info("test message", "key", "value")
	cleanup()

	// Verify the file exists and contains valid JSON.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", logPath, err)
	}
	if len(data) == 0 {
		t.Fatal("log file is empty after writing a message")
	}

	// Each log line should be a valid JSON object.
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Errorf("log file does not contain valid JSON: %v\ncontent: %s", err, data)
	}
}

// TestNewLevelFiltering verifies that the configured level actually filters
// messages. Writing a Debug message when the level is "warn" should leave the
// file empty.
func TestNewLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "filtered.log")

	cfg := config.LoggingConfig{
		Level:    "warn",
		FilePath: logPath,
	}

	logger, cleanup, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cleanup()

	// Debug message should be filtered out.
	logger.Debug("this should not appear")

	cleanup()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", logPath, err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty log file when debug message sent at warn level, got: %s", data)
	}
}

// TestNewFileAppend verifies that opening an existing log file does not
// truncate its contents (append mode).
func TestNewFileAppend(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")

	// First run: write one entry.
	cfg := config.LoggingConfig{Level: "info", FilePath: logPath}

	logger1, cleanup1, err := New(cfg)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	logger1.Info("first run")
	cleanup1()

	firstSize, err := fileSize(logPath)
	if err != nil {
		t.Fatal(err)
	}

	// Second run: write another entry — should append, not truncate.
	logger2, cleanup2, err := New(cfg)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	logger2.Info("second run")
	cleanup2()

	secondSize, err := fileSize(logPath)
	if err != nil {
		t.Fatal(err)
	}

	if secondSize <= firstSize {
		t.Errorf("expected log file to grow on second open (append mode), first=%d second=%d", firstSize, secondSize)
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
