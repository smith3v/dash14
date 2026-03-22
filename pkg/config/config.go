// Package config handles YAML configuration loading and validation for dash14.
package config

// Config holds the full application configuration loaded from a YAML file.
// Fields are grouped into nested structs by domain to keep the YAML schema
// readable and to allow independent validation per startup mode.
type Config struct {
	Telegram TelegramConfig `yaml:"telegram"`
	SQLite   SQLiteConfig   `yaml:"sqlite"`
	Overlay  OverlayConfig  `yaml:"overlay"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// TelegramConfig holds Telegram bot credentials and settings.
type TelegramConfig struct {
	// Token is the bot token issued by BotFather. Required for runtime mode.
	Token string `yaml:"token"`
}

// SQLiteConfig holds the SQLite database path.
type SQLiteConfig struct {
	// Path is the filesystem path to the SQLite database file. Required.
	Path string `yaml:"path"`
}

// OverlayConfig holds paths used by the overlay renderer.
type OverlayConfig struct {
	// PlannedTemplatePath is the path to the HTML template rendered when a
	// game is in the "planned" state.
	PlannedTemplatePath string `yaml:"planned_template_path"`

	// LiveTemplatePath is the path to the HTML template rendered while a
	// game is in progress.
	LiveTemplatePath string `yaml:"live_template_path"`

	// IntermissionTemplatePath is the path to the HTML template rendered for
	// the standalone intermission scoreboard page.
	IntermissionTemplatePath string `yaml:"intermission_template_path"`

	// OutputPath is where the rendered overlay HTML file is written. OBS
	// reads this file as a Browser Source.
	OutputPath string `yaml:"output_path"`

	// LogoDir is the managed directory where team logo files are stored.
	LogoDir string `yaml:"logo_dir"`

	// TemplateCacheRefreshIntervalSeconds controls how often parsed overlay
	// templates are refreshed from disk. Zero means load once and keep the
	// in-memory cache indefinitely.
	TemplateCacheRefreshIntervalSeconds int `yaml:"template_cache_refresh_interval_seconds"`
}

// LoggingConfig controls log output format and destination.
type LoggingConfig struct {
	// Level is the minimum log level to emit. Accepted values are "debug",
	// "info", "warn", and "error". Defaults to "info" when empty.
	Level string `yaml:"level"`

	// FilePath is an optional path to a log file. When empty, logs are
	// written to stderr.
	FilePath string `yaml:"file_path"`
}
