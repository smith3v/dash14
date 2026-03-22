package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads and parses the YAML configuration file at path.
//
// Strict mode is enabled: any key present in the YAML file that does not
// correspond to a known Config field causes an error. This catches typos in
// configuration files early so operators get immediate feedback rather than
// silently running with a misconfigured field.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: open %q: %w", path, err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %q: %w", path, err)
	}

	return cfg, nil
}

// ValidateRuntime checks that all fields required for the normal runtime are
// present and non-empty. This includes Telegram bot credentials that are not
// needed in import mode.
func (c Config) ValidateRuntime() error {
	var errs []error

	if c.Telegram.Token == "" {
		errs = append(errs, errors.New("telegram.token is required"))
	}
	if c.SQLite.Path == "" {
		errs = append(errs, errors.New("sqlite.path is required"))
	}
	if c.Overlay.PlannedTemplatePath == "" {
		errs = append(errs, errors.New("overlay.planned_template_path is required"))
	}
	if c.Overlay.LiveTemplatePath == "" {
		errs = append(errs, errors.New("overlay.live_template_path is required"))
	}
	if c.Overlay.IntermissionTemplatePath == "" {
		errs = append(errs, errors.New("overlay.intermission_template_path is required"))
	}
	if c.Overlay.TemplateCacheRefreshIntervalSeconds < 0 {
		errs = append(errs, errors.New("overlay.template_cache_refresh_interval_seconds must be >= 0"))
	}

	return errors.Join(errs...)
}

// ValidateImport checks that all fields required for import mode are present.
// Import mode does not start the Telegram bot, so Telegram credentials are
// intentionally not required here.
func (c Config) ValidateImport() error {
	var errs []error

	if c.SQLite.Path == "" {
		errs = append(errs, errors.New("sqlite.path is required"))
	}
	if c.Overlay.TemplateCacheRefreshIntervalSeconds < 0 {
		errs = append(errs, errors.New("overlay.template_cache_refresh_interval_seconds must be >= 0"))
	}

	return errors.Join(errs...)
}
