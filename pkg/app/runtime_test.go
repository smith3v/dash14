package app

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
	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
	"github.com/smith3v/dash14/pkg/telegram"
)

func TestRunWithDepsImportModeExitsAfterImport(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	logoSrc := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(logoSrc, []byte("png"), 0o644); err != nil {
		t.Fatalf("write logo source: %v", err)
	}

	importPath := filepath.Join(dir, "teams.yaml")
	importYAML := "- key: team-a\n  name: Team A\n  short_name: TA\n  logo: " + logoSrc + "\n  aliases: [Alpha]\n"
	if err := os.WriteFile(importPath, []byte(importYAML), 0o644); err != nil {
		t.Fatalf("write import yaml: %v", err)
	}

	cfg := config.Config{
		SQLite: config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			LogoDir: filepath.Join(dir, "logos"),
		},
	}

	telegramStarted := false
	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		telegramStarted = true
		return func(context.Context) {}, nil
	}

	err := runWithDeps(context.Background(), Options{
		ConfigPath: "unused.yaml",
		ImportPath: importPath,
	}, deps)
	if err != nil {
		t.Fatalf("runWithDeps import mode: %v", err)
	}
	if telegramStarted {
		t.Fatal("telegram runtime should not start in import mode")
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db for verification: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db for verification: %v", err)
	}
	team, err := storage.NewTeamRepository(db).GetTeamByKey("team-a")
	if err != nil {
		t.Fatalf("expected imported team in DB: %v", err)
	}
	if team.Name != "Team A" {
		t.Fatalf("unexpected team name %q", team.Name)
	}
}

func TestRunWithDepsRuntimeRendersOverlayBeforeTelegramStart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}} vs {{.GuestTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission {{.HomeSetsWon}}-{{.GuestSetsWon}} {{range .SetScores}}set{{.SetNumber}} {{.HomeScore}}-{{.GuestScore}} {{end}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		CurrentSetNumber: 1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:                 plannedTpl,
			LiveTemplatePath:                    liveTpl,
			IntermissionTemplatePath:            intermissionTpl,
			FinishedTemplatePath:                finishedTpl,
			OutputPath:                          outputPath,
			LogoDir:                             filepath.Join(dir, "logos"),
			TemplateCacheRefreshIntervalSeconds: 1,
		},
	}

	telegramStarted := false
	sawRenderedOverlay := false
	sawRenderedIntermission := false
	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {
			telegramStarted = true
			if _, err := os.Stat(outputPath); err == nil {
				sawRenderedOverlay = true
			}
			intermissionPath := filepath.Join(filepath.Dir(outputPath), "intermission.html")
			if data, err := os.ReadFile(intermissionPath); err == nil && strings.Contains(string(data), "intermission 0-0") {
				sawRenderedIntermission = true
			}
		}, nil
	}

	err = runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps)
	if err != nil {
		t.Fatalf("runWithDeps runtime mode: %v", err)
	}
	if !telegramStarted {
		t.Fatal("expected telegram runtime to start")
	}
	if !sawRenderedOverlay {
		t.Fatal("expected overlay to be rendered before telegram start")
	}
	if !sawRenderedIntermission {
		t.Fatal("expected intermission overlay to be rendered before telegram start")
	}
}

func TestOverlayRefreshLoopRerendersCurrentOverlayAfterTemplateReload(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished-v1 {{.HomeTeamName}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Very Long Home Team Name", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusFinished,
		Phase:            storage.GamePhaseFinished,
		CurrentSetNumber: 4,
		HomeSetsWon:      3,
		GuestSetsWon:     1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}

	renderer := overlay.NewRenderer(config.OverlayConfig{
		PlannedTemplatePath:      plannedTpl,
		LiveTemplatePath:         liveTpl,
		IntermissionTemplatePath: intermissionTpl,
		FinishedTemplatePath:     finishedTpl,
		OutputPath:               outputPath,
		LogoDir:                  filepath.Join(dir, "logos"),
	})

	if err := renderCurrentOverlay(games, teams, renderer); err != nil {
		t.Fatalf("initial renderCurrentOverlay: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read initial output: %v", err)
	}
	if !strings.Contains(string(data), "finished-v1 Very Long Home Team Name") {
		t.Fatalf("initial output = %q, want finished-v1 content", string(data))
	}

	ctx, cancel := context.WithCancel(context.Background())
	startOverlayRefreshLoop(
		ctx,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		20*time.Millisecond,
		games,
		teams,
		renderer,
	)

	if err := os.WriteFile(finishedTpl, []byte("finished-v2 {{.HomeTeamName}}"), 0o644); err != nil {
		t.Fatalf("write updated finished template: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		data, err = os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("read refreshed output: %v", err)
		}
		if strings.Contains(string(data), "finished-v2 Very Long Home Team Name") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refreshed output = %q, want finished-v2 content", string(data))
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	time.Sleep(30 * time.Millisecond)
}

func TestRunWithDepsRuntimeRejectsMissingIntermissionTemplate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}} vs {{.GuestTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		CurrentSetNumber: 1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:  plannedTpl,
			LiveTemplatePath:     liveTpl,
			FinishedTemplatePath: finishedTpl,
			OutputPath:           outputPath,
			LogoDir:              filepath.Join(dir, "logos"),
		},
	}

	telegramStarted := false
	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {
			telegramStarted = true
		}, nil
	}

	err = runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps)
	if err == nil {
		t.Fatal("runWithDeps runtime mode expected error for missing intermission template, got nil")
	}
	if !strings.Contains(err.Error(), "overlay.intermission_template_path is required") {
		t.Fatalf("runWithDeps runtime mode error = %q, want missing intermission template path", err)
	}
	if telegramStarted {
		t.Fatal("telegram runtime should not start when runtime config is invalid")
	}
}

func TestRunWithDepsRuntimeIntermissionOmitsUnstartedNextSet(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}} vs {{.GuestTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission {{range .SetScores}}set{{.SetNumber}} {{.HomeScore}}-{{.GuestScore}} {{end}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusInProgress,
		CurrentSetNumber: 2,
		HomeSetsWon:      1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := games.CreateSet(&storage.GameSet{
		GameID:     current.ID,
		SetNumber:  1,
		HomeScore:  25,
		GuestScore: 19,
		IsFinished: true,
	}); err != nil {
		t.Fatalf("create finished set: %v", err)
	}
	if err := games.CreateSet(&storage.GameSet{
		GameID:     current.ID,
		SetNumber:  2,
		HomeScore:  0,
		GuestScore: 0,
		IsFinished: false,
	}); err != nil {
		t.Fatalf("create placeholder set: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:      plannedTpl,
			LiveTemplatePath:         liveTpl,
			IntermissionTemplatePath: intermissionTpl,
			FinishedTemplatePath:     finishedTpl,
			OutputPath:               outputPath,
			LogoDir:                  filepath.Join(dir, "logos"),
		},
	}

	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {}, nil
	}

	err = runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps)
	if err != nil {
		t.Fatalf("runWithDeps runtime mode: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(outputPath), "intermission.html"))
	if err != nil {
		t.Fatalf("read intermission output: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "set1 25-19") {
		t.Fatalf("expected finished set in intermission output, got %q", got)
	}
	if strings.Contains(got, "set2 0-0") {
		t.Fatalf("expected placeholder next set to be omitted, got %q", got)
	}
}

func TestRunWithDepsRuntimeRendersBetweenSetsOnMainOverlay(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission-main {{.HomeSetsWon}}-{{.GuestSetsWon}} {{range .SetScores}}set{{.SetNumber}} {{.HomeScore}}-{{.GuestScore}} {{end}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished-main {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusInProgress,
		Phase:            storage.GamePhaseBetweenSets,
		CurrentSetNumber: 2,
		HomeSetsWon:      1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := games.CreateSet(&storage.GameSet{
		GameID:     current.ID,
		SetNumber:  1,
		HomeScore:  25,
		GuestScore: 19,
		IsFinished: true,
	}); err != nil {
		t.Fatalf("create finished set: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:      plannedTpl,
			LiveTemplatePath:         liveTpl,
			IntermissionTemplatePath: intermissionTpl,
			FinishedTemplatePath:     finishedTpl,
			OutputPath:               outputPath,
			LogoDir:                  filepath.Join(dir, "logos"),
		},
	}

	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {}, nil
	}

	if err := runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps); err != nil {
		t.Fatalf("runWithDeps runtime mode: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read main overlay output: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "intermission-main 1-0") {
		t.Fatalf("expected intermission main overlay, got %q", got)
	}
	if strings.Contains(got, "finished-main") || strings.Contains(got, "live ") || strings.Contains(got, "planned ") {
		t.Fatalf("expected only between-sets main overlay content, got %q", got)
	}
}

func TestRunWithDepsRuntimeDerivesBetweenSetsForLegacyGameWithoutActiveSet(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission-main {{.HomeSetsWon}}-{{.GuestSetsWon}} {{range .SetScores}}set{{.SetNumber}} {{.HomeScore}}-{{.GuestScore}} {{end}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished-main {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusInProgress,
		CurrentSetNumber: 2,
		HomeSetsWon:      1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := games.CreateSet(&storage.GameSet{
		GameID:     current.ID,
		SetNumber:  1,
		HomeScore:  25,
		GuestScore: 19,
		IsFinished: true,
	}); err != nil {
		t.Fatalf("create finished set: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:      plannedTpl,
			LiveTemplatePath:         liveTpl,
			IntermissionTemplatePath: intermissionTpl,
			FinishedTemplatePath:     finishedTpl,
			OutputPath:               outputPath,
			LogoDir:                  filepath.Join(dir, "logos"),
		},
	}

	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {}, nil
	}

	if err := runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps); err != nil {
		t.Fatalf("runWithDeps runtime mode: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read main overlay output: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "intermission-main 1-0 set1 25-19") {
		t.Fatalf("expected legacy no-active-set game to render between-sets overlay, got %q", got)
	}
	if strings.Contains(got, "live ") {
		t.Fatalf("expected legacy no-active-set game to avoid live overlay, got %q", got)
	}
}

func TestRunWithDepsRuntimeIgnoresStaleStoredPhaseForPlannedGame(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned-main {{.HomeTeamName}} vs {{.GuestTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live-main {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission-main {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished-main {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		Phase:            storage.GamePhaseBetweenSets,
		CurrentSetNumber: 1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:      plannedTpl,
			LiveTemplatePath:         liveTpl,
			IntermissionTemplatePath: intermissionTpl,
			FinishedTemplatePath:     finishedTpl,
			OutputPath:               outputPath,
			LogoDir:                  filepath.Join(dir, "logos"),
		},
	}

	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {}, nil
	}

	if err := runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps); err != nil {
		t.Fatalf("runWithDeps runtime mode: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read main overlay output: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "planned-main Home Team vs Guest Team") {
		t.Fatalf("expected planned overlay despite stale stored phase, got %q", got)
	}
	if strings.Contains(got, "intermission-main") || strings.Contains(got, "live-main") || strings.Contains(got, "finished-main") {
		t.Fatalf("expected only planned overlay content, got %q", got)
	}
}

func TestRunWithDepsRuntimeDoesNotRenderFinishedGameWithoutCurrentMatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dash14.db")
	outputPath := filepath.Join(dir, "overlay", "current.html")
	plannedTpl := filepath.Join(dir, "planned.tmpl")
	liveTpl := filepath.Join(dir, "live.tmpl")
	intermissionTpl := filepath.Join(dir, "intermission.html.tmpl")
	finishedTpl := filepath.Join(dir, "finished.html.tmpl")
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
	}
	if err := os.WriteFile(intermissionTpl, []byte("intermission-side {{.HomeSetsWon}}-{{.GuestSetsWon}}"), 0o644); err != nil {
		t.Fatalf("write intermission template: %v", err)
	}
	if err := os.WriteFile(finishedTpl, []byte("finished-main {{.HomeSetsWon}}-{{.GuestSetsWon}} {{range .SetScores}}set{{.SetNumber}} {{.HomeScore}}-{{.GuestScore}} {{end}}"), 0o644); err != nil {
		t.Fatalf("write finished template: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	teams := storage.NewTeamRepository(db)
	games := storage.NewGameRepository(db)
	home := &storage.Team{Key: "home", Name: "Home Team", ShortName: "HOME"}
	guest := &storage.Team{Key: "guest", Name: "Guest Team", ShortName: "GUEST"}
	if err := teams.UpsertTeam(home); err != nil {
		t.Fatalf("upsert home: %v", err)
	}
	if err := teams.UpsertTeam(guest); err != nil {
		t.Fatalf("upsert guest: %v", err)
	}
	current := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusFinished,
		Phase:            storage.GamePhaseFinished,
		CurrentSetNumber: 4,
		HomeSetsWon:      3,
		GuestSetsWon:     1,
	}
	if err := games.CreateGame(current); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := games.CreateSet(&storage.GameSet{
		GameID:     current.ID,
		SetNumber:  1,
		HomeScore:  25,
		GuestScore: 19,
		IsFinished: true,
	}); err != nil {
		t.Fatalf("create finished set 1: %v", err)
	}
	if err := games.CreateSet(&storage.GameSet{
		GameID:     current.ID,
		SetNumber:  2,
		HomeScore:  17,
		GuestScore: 25,
		IsFinished: true,
	}); err != nil {
		t.Fatalf("create finished set 2: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath:      plannedTpl,
			LiveTemplatePath:         liveTpl,
			IntermissionTemplatePath: intermissionTpl,
			FinishedTemplatePath:     finishedTpl,
			OutputPath:               outputPath,
			LogoDir:                  filepath.Join(dir, "logos"),
		},
	}

	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {}, nil
	}

	if err := runWithDeps(context.Background(), Options{ConfigPath: "unused.yaml"}, deps); err != nil {
		t.Fatalf("runWithDeps runtime mode: %v", err)
	}

	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		if err != nil {
			t.Fatalf("stat main overlay output: %v", err)
		}
		t.Fatalf("expected no main overlay output for finished-only history, found %s", outputPath)
	}
}
