package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/smith3v/dash14/pkg/config"
	"github.com/smith3v/dash14/pkg/importer"
	"github.com/smith3v/dash14/pkg/logging"
	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
	"github.com/smith3v/dash14/pkg/telegram"
	"gorm.io/gorm"
)

// Options are startup options forwarded from the CLI layer.
type Options struct {
	ConfigPath string
	ImportPath string
}

// ImportMode reports whether the app should run teams import and exit.
func (o Options) ImportMode() bool {
	return o.ImportPath != ""
}

type runtimeDeps struct {
	loadConfig   func(path string) (config.Config, error)
	newLogger    func(cfg config.LoggingConfig) (*slog.Logger, func(), error)
	openDB       func(path string) (*gorm.DB, error)
	migrate      func(db *gorm.DB) error
	newLogoStore func(logoDir string) *importer.LogoStore
	newImporter  func(repo *storage.TeamRepository, logos *importer.LogoStore) *importer.Importer
	newTelegram  func(
		token string,
		logger *slog.Logger,
		users *storage.UserRepository,
		teams *storage.TeamRepository,
		games *storage.GameRepository,
		gameChanged func(),
	) (func(context.Context), error)
}

func defaultRuntimeDeps() runtimeDeps {
	return runtimeDeps{
		loadConfig:   config.Load,
		newLogger:    logging.New,
		openDB:       storage.Open,
		migrate:      storage.Migrate,
		newLogoStore: importer.NewLogoStore,
		newImporter:  importer.NewImporter,
		newTelegram: func(
			token string,
			logger *slog.Logger,
			users *storage.UserRepository,
			teams *storage.TeamRepository,
			games *storage.GameRepository,
			gameChanged func(),
		) (func(context.Context), error) {
			b, err := telegram.New(token)
			if err != nil {
				return nil, err
			}
			r := telegram.NewRouter(b, logger, b, users, teams)
			r.SetGameServices(games, gameChanged)
			r.Register()
			return b.Start, nil
		},
	}
}

// Run executes the full dash14 startup flow for runtime and import modes.
func Run(ctx context.Context, opts Options) error {
	return runWithDeps(ctx, opts, defaultRuntimeDeps())
}

func runWithDeps(ctx context.Context, opts Options, deps runtimeDeps) error {
	cfg, err := deps.loadConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("app: load config: %w", err)
	}

	if opts.ImportMode() {
		if err := cfg.ValidateImport(); err != nil {
			return fmt.Errorf("app: validate import config: %w", err)
		}
	} else {
		if err := cfg.ValidateRuntime(); err != nil {
			return fmt.Errorf("app: validate runtime config: %w", err)
		}
	}

	logger, cleanup, err := deps.newLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("app: setup logger: %w", err)
	}
	defer cleanup()

	db, err := deps.openDB(cfg.SQLite.Path)
	if err != nil {
		return fmt.Errorf("app: open sqlite: %w", err)
	}
	if err := deps.migrate(db); err != nil {
		return fmt.Errorf("app: migrate sqlite: %w", err)
	}

	teams := storage.NewTeamRepository(db)
	users := storage.NewUserRepository(db)
	games := storage.NewGameRepository(db)

	if opts.ImportMode() {
		imp := deps.newImporter(teams, deps.newLogoStore(cfg.Overlay.LogoDir))
		if err := imp.ImportTeams(opts.ImportPath); err != nil {
			return fmt.Errorf("app: import teams: %w", err)
		}
		logger.Info("teams import completed", "import_path", opts.ImportPath)
		return nil
	}

	renderer, err := overlay.OpenRenderer(cfg.Overlay)
	if err != nil {
		return err
	}
	publisher := newOverlayPublisher(db, renderer)
	if err := publisher.web.Publish(nil); err != nil {
		return fmt.Errorf("app: publish idle overlay: %w", err)
	}
	if err := publisher.publish(ctx, logger); err != nil {
		logger.Error("initial overlay publication failed", "err", err)
		publisher.notify()
	}

	startTelegram, err := deps.newTelegram(cfg.Telegram.Token, logger, users, teams, games, publisher.notify)
	if err != nil {
		return fmt.Errorf("app: init telegram bot: %w", err)
	}
	address := cfg.HTTP.ListenAddress
	if address == "" {
		address = "127.0.0.1:8081"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("app: listen for overlay HTTP: %w", err)
	}
	logger.Info("overlay HTTP listening", "address", listener.Addr().String())

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	publishDone := make(chan struct{})
	go func() {
		defer close(publishDone)
		publisher.run(runCtx, logger, time.Duration(cfg.Overlay.TemplateCacheRefreshIntervalSeconds)*time.Second)
	}()

	server := &http.Server{
		Handler:           publisher.web,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	httpDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != http.ErrServerClosed {
			cancel()
		}
		httpDone <- err
	}()

	startTelegram(runCtx)
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	<-publishDone
	if err := <-httpDone; err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("app: serve overlay HTTP: %w", err)
	}
	return shutdownErr
}
