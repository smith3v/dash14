package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smith3v/dash14/pkg/config"
)

// writeTemp writes content to a temporary file and returns its path.
// The file is removed automatically when the test ends.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

// validYAML is a minimal, fully-populated configuration.
const validYAML = `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  finished_template_path: "templates/finished.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
  template_cache_refresh_interval_seconds: 60
logging:
  level: "info"
  file_path: "var/dash14.log"
`

// minimalImportYAML contains only the fields required for import mode.
const minimalImportYAML = `
sqlite:
  path: "var/dash14.db"
`

func TestLoad_ValidYAML(t *testing.T) {
	t.Parallel()

	path := writeTemp(t, validYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Telegram.Token != "bot-token-123" {
		t.Errorf("Telegram.Token = %q, want %q", cfg.Telegram.Token, "bot-token-123")
	}
	if cfg.SQLite.Path != "var/dash14.db" {
		t.Errorf("SQLite.Path = %q, want %q", cfg.SQLite.Path, "var/dash14.db")
	}
	if cfg.Overlay.PlannedTemplatePath != "templates/planned.html" {
		t.Errorf("Overlay.PlannedTemplatePath = %q, want %q", cfg.Overlay.PlannedTemplatePath, "templates/planned.html")
	}
	if cfg.Overlay.LiveTemplatePath != "templates/live.html" {
		t.Errorf("Overlay.LiveTemplatePath = %q, want %q", cfg.Overlay.LiveTemplatePath, "templates/live.html")
	}
	if cfg.Overlay.IntermissionTemplatePath != "templates/intermission.html" {
		t.Errorf("Overlay.IntermissionTemplatePath = %q, want %q", cfg.Overlay.IntermissionTemplatePath, "templates/intermission.html")
	}
	if cfg.Overlay.FinishedTemplatePath != "templates/finished.html" {
		t.Errorf("Overlay.FinishedTemplatePath = %q, want %q", cfg.Overlay.FinishedTemplatePath, "templates/finished.html")
	}
	if cfg.Overlay.OutputPath != "out/overlay.html" {
		t.Errorf("Overlay.OutputPath = %q, want %q", cfg.Overlay.OutputPath, "out/overlay.html")
	}
	if cfg.Overlay.LogoDir != "var/logos" {
		t.Errorf("Overlay.LogoDir = %q, want %q", cfg.Overlay.LogoDir, "var/logos")
	}
	if cfg.Overlay.TemplateCacheRefreshIntervalSeconds != 60 {
		t.Errorf("Overlay.TemplateCacheRefreshIntervalSeconds = %d, want %d", cfg.Overlay.TemplateCacheRefreshIntervalSeconds, 60)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
	if cfg.Logging.FilePath != "var/dash14.log" {
		t.Errorf("Logging.FilePath = %q, want %q", cfg.Logging.FilePath, "var/dash14.log")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("Load() expected error for missing file, got nil")
	}
}

func TestLoad_UnknownKeyRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "unknown top-level key",
			content: `
telegram:
  token: "tok"
sqlite:
  path: "dash14.db"
unknown_key: "should fail"
`,
		},
		{
			name: "unknown nested key inside telegram",
			content: `
telegram:
  token: "tok"
  extra_field: "bad"
sqlite:
  path: "dash14.db"
`,
		},
		{
			name: "unknown nested key inside sqlite",
			content: `
telegram:
  token: "tok"
sqlite:
  path: "dash14.db"
  typo_path: "oops"
`,
		},
		{
			name: "unknown nested key inside overlay",
			content: `
telegram:
  token: "tok"
sqlite:
  path: "dash14.db"
overlay:
  planned_template_path: "t.html"
  oops: "bad"
`,
		},
		{
			name: "unknown nested key inside logging",
			content: `
telegram:
  token: "tok"
sqlite:
  path: "dash14.db"
logging:
  level: "info"
  verbosity: "typo"
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeTemp(t, tc.content)
			_, err := config.Load(path)
			if err == nil {
				t.Fatalf("Load() expected error for unknown key, got nil")
			}
		})
	}
}

func TestLoad_TemplateCacheRefreshInterval_DefaultsToZeroWhenOmitted(t *testing.T) {
	t.Parallel()

	content := `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  finished_template_path: "templates/finished.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
logging:
  level: "info"
`

	path := writeTemp(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Overlay.TemplateCacheRefreshIntervalSeconds != 0 {
		t.Fatalf("Overlay.TemplateCacheRefreshIntervalSeconds = %d, want 0", cfg.Overlay.TemplateCacheRefreshIntervalSeconds)
	}
}

func TestValidateRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantErr     bool
		wantErrSubs []string
	}{
		{
			name:    "valid: all required fields present",
			content: validYAML,
			wantErr: false,
		},
		{
			name: "valid: omitted template refresh interval defaults to zero",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  finished_template_path: "templates/finished.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
`,
			wantErr: false,
		},
		{
			name: "valid: positive template refresh interval",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  finished_template_path: "templates/finished.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
  template_cache_refresh_interval_seconds: 30
`,
			wantErr: false,
		},
		{
			name: "missing telegram token",
			content: `
sqlite:
  path: "dash14.db"
`,
			wantErr:     true,
			wantErrSubs: []string{"telegram.token"},
		},
		{
			name: "missing sqlite path",
			content: `
telegram:
  token: "tok"
`,
			wantErr:     true,
			wantErrSubs: []string{"sqlite.path"},
		},
		{
			name: "missing intermission template path",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
`,
			wantErr:     true,
			wantErrSubs: []string{"overlay.intermission_template_path"},
		},
		{
			name: "missing finished template path",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
`,
			wantErr:     true,
			wantErrSubs: []string{"overlay.finished_template_path"},
		},
		{
			name: "missing planned template path",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
`,
			wantErr:     true,
			wantErrSubs: []string{"overlay.planned_template_path"},
		},
		{
			name: "missing live template path",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  intermission_template_path: "templates/intermission.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
`,
			wantErr:     true,
			wantErrSubs: []string{"overlay.live_template_path"},
		},
		{
			name:    "missing both telegram token and sqlite path",
			content: `{}`,
			wantErr: true,
			wantErrSubs: []string{
				"telegram.token",
				"sqlite.path",
				"overlay.planned_template_path",
				"overlay.live_template_path",
				"overlay.intermission_template_path",
				"overlay.finished_template_path",
			},
		},
		{
			name: "negative template refresh interval",
			content: `
telegram:
  token: "bot-token-123"
sqlite:
  path: "var/dash14.db"
overlay:
  planned_template_path: "templates/planned.html"
  live_template_path: "templates/live.html"
  intermission_template_path: "templates/intermission.html"
  finished_template_path: "templates/finished.html"
  output_path: "out/overlay.html"
  logo_dir: "var/logos"
  template_cache_refresh_interval_seconds: -1
`,
			wantErr:     true,
			wantErrSubs: []string{"overlay.template_cache_refresh_interval_seconds"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeTemp(t, tc.content)
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			err = cfg.ValidateRuntime()
			if tc.wantErr {
				if err == nil {
					t.Fatal("ValidateRuntime() expected error, got nil")
				}
				for _, sub := range tc.wantErrSubs {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error %q does not contain %q", err.Error(), sub)
					}
				}
			} else if err != nil {
				t.Fatalf("ValidateRuntime() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateImport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantErr     bool
		wantErrSubs []string
	}{
		{
			name:    "valid: sqlite path present, telegram token absent",
			content: minimalImportYAML,
			wantErr: false,
		},
		{
			name: "valid: positive template refresh interval",
			content: `
sqlite:
  path: "var/dash14.db"
overlay:
  template_cache_refresh_interval_seconds: 15
`,
			wantErr: false,
		},
		{
			name:    "valid: full config also passes import validation",
			content: validYAML,
			wantErr: false,
		},
		{
			name: "missing sqlite path",
			content: `
telegram:
  token: "tok"
`,
			wantErr:     true,
			wantErrSubs: []string{"sqlite.path"},
		},
		{
			name:        "empty config fails import validation",
			content:     `{}`,
			wantErr:     true,
			wantErrSubs: []string{"sqlite.path"},
		},
		{
			name: "negative template refresh interval",
			content: `
sqlite:
  path: "var/dash14.db"
overlay:
  template_cache_refresh_interval_seconds: -5
`,
			wantErr:     true,
			wantErrSubs: []string{"overlay.template_cache_refresh_interval_seconds"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeTemp(t, tc.content)
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			err = cfg.ValidateImport()
			if tc.wantErr {
				if err == nil {
					t.Fatal("ValidateImport() expected error, got nil")
				}
				for _, sub := range tc.wantErrSubs {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error %q does not contain %q", err.Error(), sub)
					}
				}
			} else if err != nil {
				t.Fatalf("ValidateImport() unexpected error: %v", err)
			}
		})
	}
}

// TestValidateImport_TelegramNotRequired confirms that import validation does
// NOT fail when the Telegram token is absent. This is the key behavioral
// difference from ValidateRuntime.
func TestValidateImport_TelegramNotRequired(t *testing.T) {
	t.Parallel()

	content := `
sqlite:
  path: "dash14.db"
`
	path := writeTemp(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if err := cfg.ValidateImport(); err != nil {
		t.Errorf("ValidateImport() returned error for config without telegram token: %v", err)
	}

	if err := cfg.ValidateRuntime(); err == nil {
		t.Error("ValidateRuntime() expected error for config without telegram token, got nil")
	}
}
