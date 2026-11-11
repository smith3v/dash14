package overlay

import (
	"encoding/json"
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
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		web.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}
	state := func() gameResponse {
		t.Helper()
		response := request("/api/v1/game")
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
			response := request(path)
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
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
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
