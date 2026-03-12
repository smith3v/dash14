// Package importer handles YAML team import file parsing and logo file management.
package importer

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TeamImportRecord represents a single team entry from an import YAML file.
// Unknown fields within the YAML record are silently ignored; only the fields
// declared here are captured.
type TeamImportRecord struct {
	Key       string   `yaml:"key"`
	Name      string   `yaml:"name"`
	ShortName string   `yaml:"short_name"`
	Logo      string   `yaml:"logo"`
	Aliases   []string `yaml:"aliases"`
}

// ParseTeamsYAML reads the YAML file at path and returns the list of team
// records it contains. Unknown fields within each record are silently ignored,
// which allows import files produced by external tools to carry extra metadata
// without causing parse failures.
//
// The only required field is key. If any record is missing key, ParseTeamsYAML
// returns an error that identifies the offending record by its 0-based index
// within the file.
func ParseTeamsYAML(path string) ([]TeamImportRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("importer: read %q: %w", path, err)
	}

	// Decode without KnownFields(true) so that unknown keys inside each
	// record are silently ignored. yaml.v3's default behaviour is to skip
	// keys that do not map to a struct field.
	var records []TeamImportRecord
	if err := yaml.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("importer: parse %q: %w", path, err)
	}

	for i, r := range records {
		if r.Key == "" {
			return nil, fmt.Errorf("importer: teams[%d]: key is required", i)
		}
	}

	return records, nil
}
