package importer

import (
	"strings"
	"testing"
)

// TestParseTeamsYAML_Valid verifies that a well-formed import file with extra
// (unknown) fields is parsed correctly and that all declared fields—including
// the aliases slice—are round-tripped without loss.
func TestParseTeamsYAML_Valid(t *testing.T) {
	records, err := ParseTeamsYAML("testdata/teams-valid.yaml")
	if err != nil {
		t.Fatalf("ParseTeamsYAML: unexpected error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("want 2 records, got %d", len(records))
	}

	tests := []struct {
		idx       int
		key       string
		name      string
		shortName string
		logo      string
		aliases   []string
	}{
		{
			idx:       0,
			key:       "lokomotiv",
			name:      "Lokomotiv Novosibirsk",
			shortName: "LOK",
			logo:      "logos/lokomotiv.png",
			aliases:   []string{"Loko", "Lokomotiv"},
		},
		{
			idx:       1,
			key:       "zenit",
			name:      "Zenit Saint Petersburg",
			shortName: "ZEN",
			logo:      "logos/zenit.png",
			aliases:   []string{"Zenit SPb"},
		},
	}

	for _, tt := range tests {
		r := records[tt.idx]

		if r.Key != tt.key {
			t.Errorf("records[%d].Key = %q, want %q", tt.idx, r.Key, tt.key)
		}
		if r.Name != tt.name {
			t.Errorf("records[%d].Name = %q, want %q", tt.idx, r.Name, tt.name)
		}
		if r.ShortName != tt.shortName {
			t.Errorf("records[%d].ShortName = %q, want %q", tt.idx, r.ShortName, tt.shortName)
		}
		if r.Logo != tt.logo {
			t.Errorf("records[%d].Logo = %q, want %q", tt.idx, r.Logo, tt.logo)
		}
		if len(r.Aliases) != len(tt.aliases) {
			t.Errorf("records[%d].Aliases length = %d, want %d", tt.idx, len(r.Aliases), len(tt.aliases))
		} else {
			for j, a := range tt.aliases {
				if r.Aliases[j] != a {
					t.Errorf("records[%d].Aliases[%d] = %q, want %q", tt.idx, j, r.Aliases[j], a)
				}
			}
		}
	}
}

// TestParseTeamsYAML_MissingKey verifies that a file containing a team record
// without the required 'key' field results in a clear, index-bearing error.
func TestParseTeamsYAML_MissingKey(t *testing.T) {
	_, err := ParseTeamsYAML("testdata/teams-missing-key.yaml")
	if err == nil {
		t.Fatal("ParseTeamsYAML: expected an error for missing key, got nil")
	}

	// The error must name the offending record. The second record (index 1) is
	// the one without a key in the test file.
	want := "teams[1]"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}

	// Confirm "key is required" phrasing is present.
	if !strings.Contains(err.Error(), "key is required") {
		t.Errorf("error %q does not contain %q", err.Error(), "key is required")
	}
}

// TestParseTeamsYAML_FileNotFound verifies that a missing file yields a
// wrapped OS-level error rather than a panic or a silent empty result.
func TestParseTeamsYAML_FileNotFound(t *testing.T) {
	_, err := ParseTeamsYAML("testdata/does-not-exist.yaml")
	if err == nil {
		t.Fatal("ParseTeamsYAML: expected an error for missing file, got nil")
	}
}

// TestParseTeamsYAML_UnknownKeysIgnored verifies explicitly that a YAML file
// whose records carry unknown fields does not produce a parse error. This test
// reuses teams-valid.yaml which embeds the 'country', 'founded', and 'stadium'
// keys that are not part of TeamImportRecord.
func TestParseTeamsYAML_UnknownKeysIgnored(t *testing.T) {
	records, err := ParseTeamsYAML("testdata/teams-valid.yaml")
	if err != nil {
		t.Fatalf("ParseTeamsYAML with unknown keys: unexpected error: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("ParseTeamsYAML with unknown keys: expected at least one record")
	}
}
