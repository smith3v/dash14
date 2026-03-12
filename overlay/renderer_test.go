package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smith3v/dash14/config"
)

// templateDir returns the absolute path to the templates directory relative to
// this test file's location. Tests must reference real templates so that
// template syntax is validated end-to-end.
func templateDir(t *testing.T) string {
	t.Helper()
	// This file lives at overlay/renderer_test.go; templates/ is one level up.
	dir, err := filepath.Abs(filepath.Join("..", "templates"))
	if err != nil {
		t.Fatalf("could not resolve templates dir: %v", err)
	}
	return dir
}

func TestRenderPlanned(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")
	tmplDir := templateDir(t)

	cfg := config.OverlayConfig{
		PlannedTemplatePath: filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:    filepath.Join(tmplDir, "live.html.tmpl"),
		OutputPath:          outPath,
	}

	r := NewRenderer(cfg)

	vm := PlannedViewModel{
		HomeTeamName:       "Dynamo",
		HomeTeamShortName:  "DYN",
		HomeTeamLogoPath:   "",
		GuestTeamName:      "Aurora",
		GuestTeamShortName: "AUR",
		GuestTeamLogoPath:  "",
	}

	if err := r.RenderPlanned(vm); err != nil {
		t.Fatalf("RenderPlanned returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable: %v", err)
	}

	got := string(content)

	checks := []struct {
		desc string
		want string
	}{
		{"home team name", "Dynamo"},
		{"guest team name", "Aurora"},
		{"home short name", "DYN"},
		{"guest short name", "AUR"},
		{"upcoming label", "Upcoming"},
		{"doctype", "<!DOCTYPE html>"},
	}

	for _, tc := range checks {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(got, tc.want) {
				t.Errorf("output does not contain %q", tc.want)
			}
		})
	}
}

func TestRenderLive(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")
	tmplDir := templateDir(t)

	cfg := config.OverlayConfig{
		PlannedTemplatePath: filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:    filepath.Join(tmplDir, "live.html.tmpl"),
		OutputPath:          outPath,
	}

	r := NewRenderer(cfg)

	vm := LiveViewModel{
		HomeTeamName:       "Dynamo",
		HomeTeamShortName:  "DYN",
		GuestTeamName:      "Aurora",
		GuestTeamShortName: "AUR",
		HomeScore:          18,
		GuestScore:         11,
		HomeSetsWon:        2,
		GuestSetsWon:       1,
		CurrentSetNumber:   4,
		LeftTeamName:       "Dynamo",
		RightTeamName:      "Aurora",
		LeftScore:          18,
		RightScore:         11,
		LeftSetsWon:        2,
		RightSetsWon:       1,
	}

	if err := r.RenderLive(vm); err != nil {
		t.Fatalf("RenderLive returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable: %v", err)
	}

	got := string(content)

	checks := []struct {
		desc string
		want string
	}{
		{"left team name (Dynamo)", "Dynamo"},
		{"right team name (Aurora)", "Aurora"},
		{"left score", "18"},
		{"right score", "11"},
		{"current set number", "4"},
		{"live status", "Live"},
		{"doctype", "<!DOCTYPE html>"},
	}

	for _, tc := range checks {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(got, tc.want) {
				t.Errorf("output does not contain %q", tc.want)
			}
		})
	}
}

func TestRenderAtomicReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")
	tmplDir := templateDir(t)

	// Write an existing file to the output path to ensure it gets replaced.
	existingContent := "<html>old content</html>"
	if err := os.WriteFile(outPath, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	cfg := config.OverlayConfig{
		PlannedTemplatePath: filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:    filepath.Join(tmplDir, "live.html.tmpl"),
		OutputPath:          outPath,
	}

	r := NewRenderer(cfg)

	vm := PlannedViewModel{
		HomeTeamName:  "NewHome",
		GuestTeamName: "NewGuest",
	}

	if err := r.RenderPlanned(vm); err != nil {
		t.Fatalf("RenderPlanned returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable after replacement: %v", err)
	}

	got := string(content)

	// Old content must be gone.
	if strings.Contains(got, existingContent) {
		t.Error("output still contains old content; file was not replaced")
	}

	// New content must be present.
	if !strings.Contains(got, "NewHome") {
		t.Error("output does not contain new home team name")
	}
	if !strings.Contains(got, "NewGuest") {
		t.Error("output does not contain new guest team name")
	}

	// Verify only one overlay HTML file exists in the directory (no leftover
	// temp files).
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmp dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected exactly 1 file in tmp dir, got %d: %v", len(entries), names)
	}
}
