package overlay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smith3v/dash14/pkg/config"
	"github.com/smith3v/dash14/pkg/storage"
)

func TestWebPublishesMatchingPagesAndGameState(t *testing.T) {
	web := newTestWeb(t)
	state := func() gameResponse {
		t.Helper()
		response := requestOverlay(web, "/api/v1/game")
		var data gameResponse
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("API status and headers = %d %v", response.Code, response.Header())
		}
		for path, revision := range map[string]string{
			"/": data.MainRevision, "/overlay.html": data.MainRevision,
			"/intermission.html": data.IntermissionRevision,
		} {
			response := requestOverlay(web, path)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-layout-revision="`+revision+`"`) {
				t.Fatalf("%s failed to serve current revision %s", path, revision)
			}
			if !strings.Contains(response.Body.String(), `src="/overlay-updater.js"`) ||
				strings.Contains(response.Body.String(), "window.location.reload") {
				t.Fatalf("%s should load only the shared overlay updater", path)
			}
		}
		return data
	}
	if err := web.Publish(nil); err != nil {
		t.Fatal(err)
	}
	idle := state()
	if idle.Game != nil {
		t.Fatal("expected idle game")
	}

	match := &Match{
		Game: storage.Game{ID: 1, Status: storage.GameStatusPlanned, HomeTeamSide: "left", CurrentSetNumber: 1},
		Home: TeamIdentity{Name: "Home"}, Guest: TeamIdentity{Name: "Guest"},
	}
	publish := func() gameResponse {
		t.Helper()
		if err := web.Publish(match); err != nil {
			t.Fatal(err)
		}
		return state()
	}
	planned := publish()
	match.Game.Status = storage.GameStatusInProgress
	match.Sets = []storage.GameSet{{ID: 1, SetNumber: 1}}
	live := publish()
	if planned.MainRevision == idle.MainRevision || live.MainRevision == planned.MainRevision ||
		live.IntermissionRevision != planned.IntermissionRevision || len(live.Game.SetScores) != 0 {
		t.Fatalf("unexpected planned-to-live change: %+v -> %+v", planned, live)
	}

	match.Sets[0].HomeScore++
	point := publish()
	if point.MainRevision != live.MainRevision || point.IntermissionRevision != live.IntermissionRevision ||
		point.Game.HomePoints != 1 || len(point.Game.SetScores) != 1 {
		t.Fatalf("unexpected point change: %+v", point)
	}
	match.Game.HomeTeamSide = "right"
	reversed := publish()
	if reversed.MainRevision == point.MainRevision || reversed.IntermissionRevision != point.IntermissionRevision {
		t.Fatalf("unexpected side-switch change: %+v", reversed)
	}

	match.Sets[0].IsFinished = true
	between := publish()
	match.Game.Status = storage.GameStatusFinished
	finished := publish()
	if between.MainRevision == reversed.MainRevision || finished.MainRevision == between.MainRevision ||
		between.Game.Phase != storage.GamePhaseBetweenSets || finished.Game.Phase != storage.GamePhaseFinished ||
		finished.IntermissionRevision != live.IntermissionRevision {
		t.Fatalf("unexpected phase transitions: %+v -> %+v", between, finished)
	}

	// A valid template that fails at execution must preserve the previous bundle.
	path := web.renderer.cfg.FinishedTemplatePath
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(original, []byte("{{.Missing}}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := web.renderer.RefreshTemplates(); err != nil {
		t.Fatal(err)
	}
	if err := web.Publish(match); err == nil {
		t.Fatal("expected template execution error")
	}
	if got := state(); got.MainRevision != finished.MainRevision {
		t.Fatal("failed publication replaced the snapshot")
	}
	if err := os.WriteFile(path, append(original, []byte("<!-- new revision -->")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := web.renderer.RefreshTemplates(); err != nil {
		t.Fatal(err)
	}
	updated := publish()
	if updated.MainRevision == finished.MainRevision || updated.IntermissionRevision != finished.IntermissionRevision {
		t.Fatal("template edit should affect only the main page")
	}
}

func TestWebPublishesOverlayLayouts(t *testing.T) {
	web := newTestWeb(t)
	match := Match{
		Game:  storage.Game{ID: 1, HomeTeamSide: "left", CurrentSetNumber: 4, HomeSetsWon: 2, GuestSetsWon: 1},
		Home:  TeamIdentity{Name: "Very Long Home Team Name", ShortName: "DYN", Hometown: "Assendelft", LogoPath: "home.png"},
		Guest: TeamIdentity{Name: "Very Long Guest Team Name", ShortName: "AUR", Hometown: "Haarlem", LogoPath: "guest.png"},
	}
	finishedSets := []storage.GameSet{
		{ID: 1, SetNumber: 1, HomeScore: 25, GuestScore: 19, IsFinished: true},
		{ID: 2, SetNumber: 2, HomeScore: 17, GuestScore: 25, IsFinished: true},
		{ID: 3, SetNumber: 3, HomeScore: 25, GuestScore: 20, IsFinished: true},
	}
	liveSets := append(append([]storage.GameSet(nil), finishedSets...),
		storage.GameSet{ID: 4, SetNumber: 4, HomeScore: 18, GuestScore: 11})
	completedSets := append(append([]storage.GameSet(nil), finishedSets...),
		storage.GameSet{ID: 4, SetNumber: 4, HomeScore: 25, GuestScore: 22, IsFinished: true})
	teamDetails := []string{"Assendelft", "Haarlem", `src="home.png"`, `src="guest.png"`}
	tests := []struct {
		name   string
		status storage.GameStatus
		sets   []storage.GameSet
		main   []string
		side   []string
	}{
		{"planned", storage.GameStatusPlanned, nil,
			[]string{"Upcoming", "font-size: 34px;", "Assendelft", "Haarlem", `src="home.png"`, `src="guest.png"`},
			[]string{`data-score="homeSetsWon">2<`, `data-score="guestSetsWon">1<`}},
		{"live", storage.GameStatusInProgress, liveSets,
			[]string{"Home Team", "Guest Team", "Live", "font-size: 26px;",
				`data-score="homePoints">18<`, `data-score="guestPoints">11<`, `class="center-value">4<`},
			[]string{`data-set="4"`, `data-set-home>18<`, `data-set-guest>11<`}},
		{"between sets", storage.GameStatusInProgress, finishedSets,
			[]string{"Game Score", "Set 1", `data-set-home>25<`, `data-set-guest>19<`},
			[]string{`data-score="homeSetsWon">2<`, `data-score="guestSetsWon">1<`}},
		{"finished", storage.GameStatusFinished, completedSets,
			[]string{"Final Score", `data-score="homeSetsWon">3<`, "Set 4", `data-set-guest>22<`,
				`src="home.png"`, `src="guest.png"`},
			[]string{`data-score="homeSetsWon">3<`, "Set 4", `data-set-guest>22<`}},
	}
	common := []string{"<!DOCTYPE html>", match.Home.Name, match.Guest.Name}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			game := match
			game.Game.Status, game.Sets = tc.status, tc.sets
			if tc.status == storage.GameStatusFinished {
				game.Game.HomeSetsWon = 3
			}
			publishMatch(t, web, &game)
			main := overlayBody(t, web, "/overlay.html")
			side := overlayBody(t, web, "/intermission.html")
			assertOverlayContains(t, main, common)
			assertOverlayContains(t, main, tc.main)
			assertOverlayContains(t, side, common)
			assertOverlayContains(t, side, teamDetails)
			assertOverlayContains(t, side, []string{"Game Score"})
			assertOverlayContains(t, side, tc.side)
			for _, shortName := range []string{match.Home.ShortName, match.Guest.ShortName} {
				if strings.Contains(main, shortName) || strings.Contains(side, shortName) {
					t.Errorf("published pages contain short name %q", shortName)
				}
			}
		})
	}

	match.Game.Status = storage.GameStatusPlanned
	match.Home.Hometown, match.Guest.Hometown = "", ""
	publishMatch(t, web, &match)
	for _, path := range []string{"/overlay.html", "/intermission.html"} {
		if strings.Contains(overlayBody(t, web, path), `class="third-line"`) {
			t.Errorf("%s rendered a third line with empty hometowns", path)
		}
	}
}

func TestWebExportsPublishedPagesAndLogos(t *testing.T) {
	web := newTestWeb(t)
	outputPath := web.renderer.cfg.OutputPath
	outDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home.png", "guest.png"} {
		writeTestFile(t, filepath.Join(web.renderer.cfg.LogoDir, name), "logo "+name)
	}
	pages := []struct {
		file string
		url  string
	}{
		{outputPath, "/overlay.html"},
		{web.renderer.intermissionOutputPath(), "/intermission.html"},
	}
	previous := "previous overlay"
	for _, page := range pages {
		writeTestFile(t, page.file, previous)
	}
	oldMain, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer oldMain.Close()

	publishMatch(t, web, &Match{
		Game:  storage.Game{ID: 1, Status: storage.GameStatusPlanned},
		Home:  TeamIdentity{Name: "Home", LogoPath: "home.png"},
		Guest: TeamIdentity{Name: "Guest", LogoPath: "guest.png"},
	})
	for _, page := range pages {
		assertFileContents(t, page.file, previous)
	}
	if err := web.Export(); err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		assertFileContents(t, page.file, overlayBody(t, web, page.url))
	}
	oldContent, err := io.ReadAll(oldMain)
	if err != nil || string(oldContent) != previous {
		t.Fatalf("open file changed during atomic replacement: %q, err=%v", oldContent, err)
	}
	for _, name := range []string{"home.png", "guest.png"} {
		assertFileContents(t, filepath.Join(outDir, name), "logo "+name)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(pages)+2 {
		t.Fatalf("expected only exported pages and logos, got %v", entries)
	}
}

func TestWebPublishesWithLastValidTemplateSet(t *testing.T) {
	web := newTestWeb(t)
	match := &Match{
		Game:  storage.Game{ID: 1, Status: storage.GameStatusPlanned},
		Home:  TeamIdentity{Name: "Home"},
		Guest: TeamIdentity{Name: "Guest"},
	}
	publishMatch(t, web, match)
	original := overlayBody(t, web, "/overlay.html")
	planned, live := web.renderer.cfg.PlannedTemplatePath, web.renderer.cfg.LiveTemplatePath
	writeTestFile(t, planned, "updated {{.HomeTeamName}}")
	writeTestFile(t, live, "{{")
	publishMatch(t, web, match)
	if got := overlayBody(t, web, "/overlay.html"); got != original {
		t.Fatal("publishing read templates from disk before refresh")
	}

	if err := web.renderer.RefreshTemplates(); err == nil {
		t.Fatal("expected malformed live template to fail refresh")
	}
	publishMatch(t, web, match)
	if got := overlayBody(t, web, "/overlay.html"); got != original {
		t.Fatal("failed refresh replaced part of the previous template set")
	}

	writeTestFile(t, live, "updated {{.LeftTeamName}}")
	if err := web.renderer.RefreshTemplates(); err != nil {
		t.Fatal(err)
	}
	publishMatch(t, web, match)
	if got := overlayBody(t, web, "/overlay.html"); got != "updated Home" {
		t.Fatalf("published page = %q, want updated template", got)
	}
}

func assertOverlayContains(t *testing.T, body string, want []string) {
	t.Helper()
	for _, value := range want {
		if !strings.Contains(body, value) {
			t.Errorf("overlay does not contain %q", value)
		}
	}
}

func requestOverlay(web *Web, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	web.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func overlayBody(t *testing.T, web *Web, path string) string {
	t.Helper()
	response := requestOverlay(web, path)
	if response.Code != http.StatusOK {
		t.Fatalf("%s status = %d", path, response.Code)
	}
	return response.Body.String()
}

func publishMatch(t *testing.T, web *Web, match *Match) {
	t.Helper()
	if err := web.Publish(match); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("content of %s = %q, want %q", path, got, want)
	}
}

func newTestWeb(t *testing.T) *Web {
	t.Helper()
	dir := t.TempDir()
	copyTemplate := func(name string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join("..", "..", "templates", name+".html.tmpl"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		writeTestFile(t, path, string(content))
		return path
	}
	renderer, err := OpenRenderer(config.OverlayConfig{
		PlannedTemplatePath:      copyTemplate("planned"),
		LiveTemplatePath:         copyTemplate("live"),
		IntermissionTemplatePath: copyTemplate("intermission"),
		FinishedTemplatePath:     copyTemplate("finished"),
		OutputPath:               filepath.Join(dir, "out", "overlay.html"),
		LogoDir:                  dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewWeb(renderer)
}
