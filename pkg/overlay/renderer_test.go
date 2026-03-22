package overlay

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smith3v/dash14/pkg/config"
)

// templateDir returns the absolute path to the templates directory relative to
// this test file's location. Tests must reference real templates so that
// template syntax is validated end-to-end.
func templateDir(t *testing.T) string {
	t.Helper()
	// This file lives at pkg/overlay/renderer_test.go; templates/ is two levels up.
	dir, err := filepath.Abs(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatalf("could not resolve templates dir: %v", err)
	}
	return dir
}

func TestRenderPlanned(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "out")
	logoDir := filepath.Join(tmpDir, "logos")
	outPath := filepath.Join(outDir, "overlay.html")
	tmplDir := templateDir(t)

	writeLogoFile(t, filepath.Join(logoDir, "home.png"), "home-logo")
	writeLogoFile(t, filepath.Join(logoDir, "guest.png"), "guest-logo")

	cfg := config.OverlayConfig{
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
		LogoDir:                  logoDir,
	}

	r := NewRenderer(cfg)

	const homeLongName = "Kroefi HS 1"
	const guestLongName = "Spaarnestad HS 14"

	vm := PlannedViewModel{
		HomeTeamName:       homeLongName,
		HomeTeamShortName:  "DYN",
		HomeTeamHometown:   "Assendelft",
		HomeTeamLogoPath:   "home.png",
		GuestTeamName:      guestLongName,
		GuestTeamShortName: "AUR",
		GuestTeamHometown:  "Haarlem",
		GuestTeamLogoPath:  "guest.png",
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
		{"home team name", homeLongName},
		{"guest team name", guestLongName},
		{"home hometown", "Assendelft"},
		{"guest hometown", "Haarlem"},
		{"home logo src", `src="home.png"`},
		{"guest logo src", `src="guest.png"`},
		{"upcoming label", "Upcoming"},
		{"planned font", "font-size: 34px;"},
		{"doctype", "<!DOCTYPE html>"},
	}

	for _, tc := range checks {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(got, tc.want) {
				t.Errorf("output does not contain %q", tc.want)
			}
		})
	}
	for _, unwanted := range []string{"DYN", "AUR"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output unexpectedly contains short name %q", unwanted)
		}
	}

	assertFileContent(t, filepath.Join(outDir, "home.png"), "home-logo")
	assertFileContent(t, filepath.Join(outDir, "guest.png"), "guest-logo")
}

func TestRenderLive(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "out")
	logoDir := filepath.Join(tmpDir, "logos")
	outPath := filepath.Join(outDir, "overlay.html")
	tmplDir := templateDir(t)

	writeLogoFile(t, filepath.Join(logoDir, "home.png"), "home-live")
	writeLogoFile(t, filepath.Join(logoDir, "guest.png"), "guest-live")

	cfg := config.OverlayConfig{
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
		LogoDir:                  logoDir,
	}

	r := NewRenderer(cfg)

	const leftLongName = "Kroefi HS 1"
	const rightLongName = "Spaarnestad HS 14"

	vm := LiveViewModel{
		HomeTeamName:       leftLongName,
		HomeTeamShortName:  "DYN",
		HomeTeamLogoPath:   "home.png",
		GuestTeamName:      rightLongName,
		GuestTeamShortName: "AUR",
		GuestTeamLogoPath:  "guest.png",
		HomeScore:          18,
		GuestScore:         11,
		HomeSetsWon:        2,
		GuestSetsWon:       1,
		CurrentSetNumber:   4,
		LeftTeamName:       leftLongName,
		LeftTeamLabel:      "Home Team",
		RightTeamName:      rightLongName,
		RightTeamLabel:     "Guest Team",
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
		{"left team name", leftLongName},
		{"right team name", rightLongName},
		{"left team label", "Home Team"},
		{"right team label", "Guest Team"},
		{"left score", "18"},
		{"right score", "11"},
		{"current set number", "4"},
		{"live status", "Live"},
		{"live font", "font-size: 26px;"},
		{"doctype", "<!DOCTYPE html>"},
	}

	for _, tc := range checks {
		t.Run(tc.desc, func(t *testing.T) {
			if !strings.Contains(got, tc.want) {
				t.Errorf("output does not contain %q", tc.want)
			}
		})
	}

	assertFileContent(t, filepath.Join(outDir, "home.png"), "home-live")
	assertFileContent(t, filepath.Join(outDir, "guest.png"), "guest-live")
}

func TestRenderIntermission(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "out")
	logoDir := filepath.Join(tmpDir, "logos")
	outPath := filepath.Join(outDir, "overlay.html")
	intermissionPath := filepath.Join(outDir, "intermission.html")
	tmplDir := templateDir(t)

	writeLogoFile(t, filepath.Join(logoDir, "home.png"), "home-break")
	writeLogoFile(t, filepath.Join(logoDir, "guest.png"), "guest-break")

	cfg := config.OverlayConfig{
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
		LogoDir:                  logoDir,
	}

	r := NewRenderer(cfg)

	vm := IntermissionViewModel{
		HomeTeamName:       "Kroefi HS 1",
		HomeTeamShortName:  "DYN",
		HomeTeamHometown:   "Assendelft",
		HomeTeamLogoPath:   "home.png",
		GuestTeamName:      "Spaarnestad HS 14",
		GuestTeamShortName: "AUR",
		GuestTeamHometown:  "Haarlem",
		GuestTeamLogoPath:  "guest.png",
		HomeSetsWon:        2,
		GuestSetsWon:       1,
		SetScores: []SetScoreViewModel{
			{SetNumber: 1, HomeScore: 25, GuestScore: 19},
			{SetNumber: 2, HomeScore: 17, GuestScore: 25},
			{SetNumber: 3, HomeScore: 14, GuestScore: 11},
		},
	}

	if err := r.RenderIntermission(vm); err != nil {
		t.Fatalf("RenderIntermission returned error: %v", err)
	}

	content, err := os.ReadFile(intermissionPath)
	if err != nil {
		t.Fatalf("intermission output file not readable: %v", err)
	}

	got := string(content)
	for _, want := range []string{
		"Kroefi HS 1",
		"Spaarnestad HS 14",
		"Assendelft",
		"Haarlem",
		`src="home.png"`,
		`src="guest.png"`,
		"Game Score",
		"Set 1",
		"25",
		"19",
		"Set 3",
		"14",
		"11",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"DYN", "AUR"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output unexpectedly contains short name %q", unwanted)
		}
	}

	assertFileContent(t, filepath.Join(outDir, "home.png"), "home-break")
	assertFileContent(t, filepath.Join(outDir, "guest.png"), "guest-break")
}

func TestRenderFinished(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "out")
	logoDir := filepath.Join(tmpDir, "logos")
	outPath := filepath.Join(outDir, "overlay.html")
	tmplDir := templateDir(t)

	writeLogoFile(t, filepath.Join(logoDir, "home.png"), "home-finished")
	writeLogoFile(t, filepath.Join(logoDir, "guest.png"), "guest-finished")

	cfg := config.OverlayConfig{
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
		LogoDir:                  logoDir,
	}

	r := NewRenderer(cfg)

	vm := FinishedViewModel{
		HomeTeamName:       "Kroefi HS 1",
		HomeTeamShortName:  "DYN",
		HomeTeamHometown:   "Assendelft",
		HomeTeamLogoPath:   "home.png",
		GuestTeamName:      "Spaarnestad HS 14",
		GuestTeamShortName: "AUR",
		GuestTeamHometown:  "Haarlem",
		GuestTeamLogoPath:  "guest.png",
		HomeSetsWon:        3,
		GuestSetsWon:       1,
		SetScores: []SetScoreViewModel{
			{SetNumber: 1, HomeScore: 25, GuestScore: 19},
			{SetNumber: 2, HomeScore: 17, GuestScore: 25},
			{SetNumber: 3, HomeScore: 25, GuestScore: 20},
			{SetNumber: 4, HomeScore: 25, GuestScore: 22},
		},
	}

	if err := r.RenderFinished(vm); err != nil {
		t.Fatalf("RenderFinished returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("finished output file not readable: %v", err)
	}

	got := string(content)
	for _, want := range []string{
		"Final Score",
		"Kroefi HS 1",
		"Spaarnestad HS 14",
		"3",
		"1",
		"Set 4",
		"22",
		`src="home.png"`,
		`src="guest.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("finished output does not contain %q", want)
		}
	}

	assertFileContent(t, filepath.Join(outDir, "home.png"), "home-finished")
	assertFileContent(t, filepath.Join(outDir, "guest.png"), "guest-finished")
}

func TestRenderPlannedOmitsThirdLineWhenHometownMissing(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")
	tmplDir := templateDir(t)

	cfg := config.OverlayConfig{
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
	}

	r := NewRenderer(cfg)

	if err := r.RenderPlanned(PlannedViewModel{
		HomeTeamName:  "Home Team",
		GuestTeamName: "Guest Team",
	}); err != nil {
		t.Fatalf("RenderPlanned returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable: %v", err)
	}

	if strings.Contains(string(content), `class="third-line"`) {
		t.Fatal("planned output unexpectedly rendered a third line with empty hometowns")
	}
}

func TestRenderIntermissionOmitsThirdLineWhenHometownMissing(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "out")
	outPath := filepath.Join(outDir, "overlay.html")
	intermissionPath := filepath.Join(outDir, "intermission.html")
	tmplDir := templateDir(t)

	cfg := config.OverlayConfig{
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
	}

	r := NewRenderer(cfg)

	if err := r.RenderIntermission(IntermissionViewModel{
		HomeTeamName:  "Home Team",
		GuestTeamName: "Guest Team",
	}); err != nil {
		t.Fatalf("RenderIntermission returned error: %v", err)
	}

	content, err := os.ReadFile(intermissionPath)
	if err != nil {
		t.Fatalf("intermission output file not readable: %v", err)
	}

	if strings.Contains(string(content), `class="third-line"`) {
		t.Fatal("intermission output unexpectedly rendered a third line with empty hometowns")
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
		PlannedTemplatePath:      filepath.Join(tmplDir, "planned.html.tmpl"),
		LiveTemplatePath:         filepath.Join(tmplDir, "live.html.tmpl"),
		IntermissionTemplatePath: filepath.Join(tmplDir, "intermission.html.tmpl"),
		FinishedTemplatePath:     filepath.Join(tmplDir, "finished.html.tmpl"),
		OutputPath:               outPath,
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

func TestRendererUsesCachedTemplatesAfterConstruction(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")

	plannedPath := filepath.Join(tmpDir, "planned.html.tmpl")
	livePath := filepath.Join(tmpDir, "live.html.tmpl")
	intermissionPath := filepath.Join(tmpDir, "intermission.html.tmpl")

	writeTemplateFile(t, plannedPath, "planned-v1 {{.HomeTeamName}} vs {{.GuestTeamName}}")
	writeTemplateFile(t, livePath, "live {{.LeftTeamName}} {{.RightTeamName}}")
	writeTemplateFile(t, intermissionPath, "intermission {{.HomeTeamName}} {{.GuestTeamName}}")

	r := NewRenderer(config.OverlayConfig{
		PlannedTemplatePath:      plannedPath,
		LiveTemplatePath:         livePath,
		IntermissionTemplatePath: intermissionPath,
		FinishedTemplatePath:     intermissionPath,
		OutputPath:               outPath,
	})

	// Mutate the source template after construction. A renderer with an
	// in-memory cache should continue using the template snapshot it parsed when
	// it was created.
	writeTemplateFile(t, plannedPath, "planned-v2 {{.HomeTeamName}} changed")

	if err := r.RenderPlanned(PlannedViewModel{
		HomeTeamName:  "Home",
		GuestTeamName: "Guest",
	}); err != nil {
		t.Fatalf("RenderPlanned returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable: %v", err)
	}

	got := string(content)
	if !strings.Contains(got, "planned-v1 Home vs Guest") {
		t.Fatalf("cached render output = %q, want content from the original parsed template", got)
	}
	if strings.Contains(got, "planned-v2") {
		t.Fatalf("cached render output unexpectedly used modified on-disk template: %q", got)
	}
}

func TestRendererZeroRefreshIntervalKeepsInitialTemplateSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")

	plannedPath := filepath.Join(tmpDir, "planned.html.tmpl")
	livePath := filepath.Join(tmpDir, "live.html.tmpl")
	intermissionPath := filepath.Join(tmpDir, "intermission.html.tmpl")

	writeTemplateFile(t, plannedPath, "planned-zero-v1 {{.HomeTeamName}}")
	writeTemplateFile(t, livePath, "live-zero-v1 {{.LeftTeamName}}")
	writeTemplateFile(t, intermissionPath, "intermission-zero-v1 {{.HomeTeamName}}")

	r := NewRenderer(config.OverlayConfig{
		PlannedTemplatePath:                 plannedPath,
		LiveTemplatePath:                    livePath,
		IntermissionTemplatePath:            intermissionPath,
		FinishedTemplatePath:                intermissionPath,
		OutputPath:                          outPath,
		TemplateCacheRefreshIntervalSeconds: 0,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartTemplateRefresh(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))

	writeTemplateFile(t, plannedPath, "planned-zero-v2 {{.HomeTeamName}}")
	time.Sleep(80 * time.Millisecond)

	if err := r.RenderPlanned(PlannedViewModel{HomeTeamName: "Home"}); err != nil {
		t.Fatalf("RenderPlanned returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "planned-zero-v1 Home") {
		t.Fatalf("zero-interval render output = %q, want cached initial template content", got)
	}
	if strings.Contains(got, "planned-zero-v2") {
		t.Fatalf("zero-interval render output unexpectedly refreshed from disk: %q", got)
	}
}

func TestRendererPositiveRefreshIntervalSwapsFullSnapshotOnlyOnSuccessfulReload(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "overlay.html")

	plannedPath := filepath.Join(tmpDir, "planned.html.tmpl")
	livePath := filepath.Join(tmpDir, "live.html.tmpl")
	intermissionPath := filepath.Join(tmpDir, "intermission.html.tmpl")

	writeTemplateFile(t, plannedPath, "planned-refresh-v1 {{.HomeTeamName}}")
	writeTemplateFile(t, livePath, "live-refresh-v1 {{.LeftTeamName}}")
	writeTemplateFile(t, intermissionPath, "intermission-refresh-v1 {{.HomeTeamName}}")

	r := NewRenderer(config.OverlayConfig{
		PlannedTemplatePath:                 plannedPath,
		LiveTemplatePath:                    livePath,
		IntermissionTemplatePath:            intermissionPath,
		FinishedTemplatePath:                intermissionPath,
		OutputPath:                          outPath,
		TemplateCacheRefreshIntervalSeconds: 1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.refreshInterval = 20 * time.Millisecond
	r.StartTemplateRefresh(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))

	writeTemplateFile(t, plannedPath, "planned-refresh-v2 {{.HomeTeamName}}")
	writeTemplateFile(t, livePath, "{{")
	time.Sleep(80 * time.Millisecond)

	if err := r.RenderPlanned(PlannedViewModel{HomeTeamName: "Home"}); err != nil {
		t.Fatalf("RenderPlanned after failed refresh returned error: %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable after failed refresh: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "planned-refresh-v1 Home") {
		t.Fatalf("failed-refresh output = %q, want original template snapshot", got)
	}
	if strings.Contains(got, "planned-refresh-v2") {
		t.Fatalf("failed-refresh output unexpectedly swapped partial snapshot: %q", got)
	}

	writeTemplateFile(t, livePath, "live-refresh-v2 {{.LeftTeamName}}")
	time.Sleep(80 * time.Millisecond)

	if err := r.RenderPlanned(PlannedViewModel{HomeTeamName: "Home"}); err != nil {
		t.Fatalf("RenderPlanned after successful refresh returned error: %v", err)
	}
	content, err = os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not readable after successful refresh: %v", err)
	}
	got = string(content)
	if !strings.Contains(got, "planned-refresh-v2 Home") {
		t.Fatalf("successful-refresh output = %q, want updated template snapshot", got)
	}
}

func writeLogoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write logo %q: %v", path, err)
	}
}

func writeTemplateFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write template %q: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("content of %q = %q, want %q", path, string(data), want)
	}
}
