package overlay

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/smith3v/dash14/pkg/storage"
)

//go:embed updater.js
var updater string

// Page identifies the HTML representation the browser should keep current.
type Page struct {
	View     string
	Revision string
}

// Match is the committed game and the public team and set data needed to render it.
type Match struct {
	Game        storage.Game
	Home, Guest TeamIdentity
	Sets        []storage.GameSet
}

type gameData struct {
	Phase        storage.GamePhase   `json:"phase"`
	CurrentSet   int                 `json:"currentSet"`
	HomeSide     string              `json:"homeSide"`
	HomePoints   int                 `json:"homePoints"`
	GuestPoints  int                 `json:"guestPoints"`
	HomeSetsWon  int                 `json:"homeSetsWon"`
	GuestSetsWon int                 `json:"guestSetsWon"`
	SetScores    []SetScoreViewModel `json:"setScores"`
}

type gameResponse struct {
	MainRevision         string    `json:"mainRevision"`
	IntermissionRevision string    `json:"intermissionRevision"`
	Game                 *gameData `json:"game"`
}

type webSnapshot struct {
	main, intermission, state []byte
	logos                     map[string]bool
}

// Web serves a pair of pages and their polling data from one immutable snapshot.
// Only the runtime publisher calls Publish and refreshes the renderer.
type Web struct {
	renderer *Renderer
	snapshot atomic.Pointer[webSnapshot]
}

func NewWeb(renderer *Renderer) *Web {
	return &Web{renderer: renderer}
}

func (web *Web) Publish(match *Match) error {
	if match == nil {
		main := revision("idle", updater)
		side := revision("idle-intermission", updater)
		return web.store(webSnapshot{
			main:         idlePage("main", main),
			intermission: idlePage("intermission", side),
		}, gameResponse{MainRevision: main, IntermissionRevision: side})
	}

	game := match.Game
	home, guest := match.Home, match.Guest
	active := storage.GameSet{}
	for _, set := range match.Sets {
		if !set.IsFinished {
			active = set
			break
		}
	}

	data := &gameData{
		Phase:        game.EffectivePhase(active.ID != 0),
		CurrentSet:   game.CurrentSetNumber,
		HomeSide:     game.HomeTeamSide,
		HomePoints:   active.HomeScore,
		GuestPoints:  active.GuestScore,
		HomeSetsWon:  game.HomeSetsWon,
		GuestSetsWon: game.GuestSetsWon,
		SetScores:    BuildSetScoreHistory(match.Sets),
	}
	templates := web.renderer.templateSnapshot()
	intermission := BuildIntermissionViewModel(home, guest, data.HomeSetsWon, data.GuestSetsWon, data.SetScores)
	teamDetails := revision(game.ID, game.HomeTeamID, game.GuestTeamID, home, guest)
	sideRevision := revision(updater, teamDetails, templates.hashes[templates.intermission])
	intermission.Page = Page{View: "intermission", Revision: sideRevision}

	var mainTemplate *template.Template
	switch data.Phase {
	case storage.GamePhasePlanned:
		mainTemplate = templates.planned
	case storage.GamePhaseSetInProgress:
		mainTemplate = templates.live
	case storage.GamePhaseBetweenSets:
		mainTemplate = templates.intermission
	case storage.GamePhaseFinished:
		mainTemplate = templates.finished
	}
	side := ""
	if data.Phase == storage.GamePhaseSetInProgress {
		side = data.HomeSide
	}
	mainRevision := revision(updater, teamDetails, templates.hashes[mainTemplate], data.Phase, data.CurrentSet, side)
	page := Page{View: "main", Revision: mainRevision}
	var mainModel any
	switch data.Phase {
	case storage.GamePhasePlanned:
		mainModel = PlannedViewModel{
			Page: page, HomeTeamName: home.Name, HomeTeamShortName: home.ShortName,
			HomeTeamHometown: home.Hometown, HomeTeamLogoPath: home.LogoPath,
			GuestTeamName: guest.Name, GuestTeamShortName: guest.ShortName,
			GuestTeamHometown: guest.Hometown, GuestTeamLogoPath: guest.LogoPath,
		}
	case storage.GamePhaseSetInProgress:
		live := BuildLiveViewModel(home, guest, side, data.HomePoints, data.GuestPoints, data.HomeSetsWon, data.GuestSetsWon, data.CurrentSet)
		live.Page = page
		mainModel = live
	case storage.GamePhaseBetweenSets:
		between := intermission
		between.Page = page
		mainModel = between
	case storage.GamePhaseFinished:
		finished := BuildFinishedViewModel(home, guest, data.HomeSetsWon, data.GuestSetsWon, data.SetScores)
		finished.Page = page
		mainModel = finished
	}
	main, err := renderPage(mainTemplate, mainModel)
	if err != nil {
		return err
	}
	breakPage, err := renderPage(templates.intermission, intermission)
	if err != nil {
		return err
	}

	return web.store(webSnapshot{
		main: main, intermission: breakPage,
		logos: map[string]bool{home.LogoPath: true, guest.LogoPath: true},
	}, gameResponse{MainRevision: mainRevision, IntermissionRevision: sideRevision, Game: data})
}

// Export writes a static copy of the current match presentation for inspection.
func (web *Web) Export() error {
	snapshot := web.snapshot.Load()
	if snapshot == nil || snapshot.logos == nil {
		return nil
	}
	for name := range snapshot.logos {
		if isLogoName(name) {
			if err := web.renderer.publishLogo(name); err != nil {
				return err
			}
		}
	}

	if err := writeOverlay(web.renderer.cfg.OutputPath, snapshot.main); err != nil {
		return err
	}
	return writeOverlay(web.renderer.intermissionOutputPath(), snapshot.intermission)
}

func (web *Web) store(snapshot webSnapshot, state gameResponse) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	snapshot.state = data
	web.snapshot.Store(&snapshot)
	return nil
}

func renderPage(tmpl *template.Template, model any) ([]byte, error) {
	var page bytes.Buffer
	if err := tmpl.Execute(&page, model); err != nil {
		return nil, fmt.Errorf("overlay: render %s: %w", tmpl.Name(), err)
	}
	return page.Bytes(), nil
}

func revision(parts ...any) string {
	// The inputs are strings, integers and TeamIdentity values.
	data, _ := json.Marshal(parts)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func idlePage(view, revision string) []byte {
	return fmt.Appendf(nil, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Dash14</title><script src="/overlay-updater.js" defer></script></head><body data-overlay-view="%s" data-layout-revision="%s"></body></html>`, view, revision)
}

func (web *Web) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/overlay-updater.js" {
		serveBytes(w, r, "text/javascript; charset=utf-8", []byte(updater))
		return
	}
	snapshot := web.snapshot.Load()
	if snapshot == nil {
		http.Error(w, "Overlay is starting", http.StatusServiceUnavailable)
		return
	}

	switch r.URL.Path {
	case "/api/v1/game":
		serveBytes(w, r, "application/json", snapshot.state)
	case "/intermission.html":
		serveBytes(w, r, "text/html; charset=utf-8", snapshot.intermission)
	case "/", "/overlay.html", "/" + filepath.Base(web.renderer.cfg.OutputPath):
		serveBytes(w, r, "text/html; charset=utf-8", snapshot.main)
	default:
		web.serveLogo(w, r, snapshot)
	}
}

func (web *Web) serveLogo(w http.ResponseWriter, r *http.Request, snapshot *webSnapshot) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !snapshot.logos[name] || !isLogoName(name) {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(web.renderer.cfg.LogoDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func isLogoName(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico":
		return true
	}
	return false
}

func serveBytes(w http.ResponseWriter, r *http.Request, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		return
	}
	_, _ = w.Write(data)
}
