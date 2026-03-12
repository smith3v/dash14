package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/smith3v/dash14/config"
	"github.com/smith3v/dash14/storage"
	"github.com/smith3v/dash14/telegram"
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
	if err := os.WriteFile(plannedTpl, []byte("planned {{.HomeTeamName}} vs {{.GuestTeamName}}"), 0o644); err != nil {
		t.Fatalf("write planned template: %v", err)
	}
	if err := os.WriteFile(liveTpl, []byte("live {{.LeftTeamName}} {{.LeftScore}}"), 0o644); err != nil {
		t.Fatalf("write live template: %v", err)
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
	if err := games.SetCurrentGameID(current.ID); err != nil {
		t.Fatalf("set current game: %v", err)
	}

	cfg := config.Config{
		Telegram: config.TelegramConfig{Token: "token"},
		SQLite:   config.SQLiteConfig{Path: dbPath},
		Overlay: config.OverlayConfig{
			PlannedTemplatePath: plannedTpl,
			LiveTemplatePath:    liveTpl,
			OutputPath:          outputPath,
			LogoDir:             filepath.Join(dir, "logos"),
		},
	}

	telegramStarted := false
	sawRenderedOverlay := false
	deps := defaultRuntimeDeps()
	deps.loadConfig = func(string) (config.Config, error) { return cfg, nil }
	deps.newTelegram = func(string, *slog.Logger, *storage.UserRepository, *storage.TeamRepository, *storage.GameRepository, telegram.OverlayRenderer) (func(context.Context), error) {
		return func(context.Context) {
			telegramStarted = true
			if _, err := os.Stat(outputPath); err == nil {
				sawRenderedOverlay = true
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
}
