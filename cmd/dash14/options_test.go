package main

import (
	"strings"
	"testing"
)

func TestParseOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           []string
		wantErr        bool
		wantErrMsg     string // substring expected in error output
		wantConfig     string
		wantImport     string
		wantImportMode bool
	}{
		{
			name:           "valid runtime mode",
			args:           []string{"--config", "config.yaml"},
			wantConfig:     "config.yaml",
			wantImport:     "",
			wantImportMode: false,
		},
		{
			name:           "valid import mode",
			args:           []string{"--config", "config.yaml", "--import", "teams.yaml"},
			wantConfig:     "config.yaml",
			wantImport:     "teams.yaml",
			wantImportMode: true,
		},
		{
			name:       "missing --config",
			args:       []string{},
			wantErr:    true,
			wantErrMsg: "--config is required",
		},
		{
			name:       "unexpected positional arguments",
			args:       []string{"--config", "config.yaml", "extra"},
			wantErr:    true,
			wantErrMsg: "unexpected positional arguments",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			opts, err := parseOptions(tc.args, &buf)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; output: %q", buf.String())
				}
				if tc.wantErrMsg != "" && !strings.Contains(buf.String(), tc.wantErrMsg) {
					t.Errorf("error output %q does not contain %q", buf.String(), tc.wantErrMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v; output: %q", err, buf.String())
			}
			if opts.ConfigPath != tc.wantConfig {
				t.Errorf("ConfigPath = %q, want %q", opts.ConfigPath, tc.wantConfig)
			}
			if opts.ImportPath != tc.wantImport {
				t.Errorf("ImportPath = %q, want %q", opts.ImportPath, tc.wantImport)
			}
			if opts.ImportMode() != tc.wantImportMode {
				t.Errorf("ImportMode() = %v, want %v", opts.ImportMode(), tc.wantImportMode)
			}
		})
	}
}
