package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

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
	newRenderer  func(cfg config.OverlayConfig) *overlay.Renderer
	newLogoStore func(logoDir string) *importer.LogoStore
	newImporter  func(repo *storage.TeamRepository, logos *importer.LogoStore) *importer.Importer
	newTelegram  func(
		token string,
		logger *slog.Logger,
		users *storage.UserRepository,
		teams *storage.TeamRepository,
		games *storage.GameRepository,
		renderer telegram.OverlayRenderer,
	) (func(context.Context), error)
	validateTempl func(cfg config.OverlayConfig) error
}

func defaultRuntimeDeps() runtimeDeps {
	return runtimeDeps{
		loadConfig:   config.Load,
		newLogger:    logging.New,
		openDB:       storage.Open,
		migrate:      storage.Migrate,
		newRenderer:  overlay.NewRenderer,
		newLogoStore: importer.NewLogoStore,
		newImporter:  importer.NewImporter,
		newTelegram: func(
			token string,
			logger *slog.Logger,
			users *storage.UserRepository,
			teams *storage.TeamRepository,
			games *storage.GameRepository,
			renderer telegram.OverlayRenderer,
		) (func(context.Context), error) {
			b, err := telegram.New(token)
			if err != nil {
				return nil, err
			}
			r := telegram.NewRouter(b, logger, b, users, teams)
			r.SetGameServices(games, renderer)
			r.Register()
			return b.Start, nil
		},
		validateTempl: validateOverlayTemplates,
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
	renderer := deps.newRenderer(cfg.Overlay)

	if opts.ImportMode() {
		imp := deps.newImporter(teams, deps.newLogoStore(cfg.Overlay.LogoDir))
		if err := imp.ImportTeams(opts.ImportPath); err != nil {
			return fmt.Errorf("app: import teams: %w", err)
		}
		logger.Info("teams import completed", "import_path", opts.ImportPath)
		return nil
	}

	if err := deps.validateTempl(cfg.Overlay); err != nil {
		return fmt.Errorf("app: validate overlay templates: %w", err)
	}
	if err := renderCurrentOverlay(games, teams, renderer); err != nil {
		return fmt.Errorf("app: render current overlay: %w", err)
	}

	startTelegram, err := deps.newTelegram(cfg.Telegram.Token, logger, users, teams, games, renderer)
	if err != nil {
		return fmt.Errorf("app: init telegram bot: %w", err)
	}
	startTelegram(ctx)
	return nil
}

func validateOverlayTemplates(cfg config.OverlayConfig) error {
	if _, err := os.Stat(cfg.PlannedTemplatePath); err != nil {
		return err
	}
	if _, err := os.Stat(cfg.LiveTemplatePath); err != nil {
		return err
	}
	return nil
}

func renderCurrentOverlay(games *storage.GameRepository, teams *storage.TeamRepository, renderer *overlay.Renderer) error {
	current, err := games.GetCurrentGame()
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}

	home, err := teams.GetTeamByID(current.HomeTeamID)
	if err != nil {
		return err
	}
	guest, err := teams.GetTeamByID(current.GuestTeamID)
	if err != nil {
		return err
	}

	if current.Status == storage.GameStatusPlanned {
		return renderer.RenderPlanned(overlay.PlannedViewModel{
			HomeTeamName:       home.Name,
			HomeTeamShortName:  home.ShortName,
			HomeTeamLogoPath:   home.LogoPath,
			GuestTeamName:      guest.Name,
			GuestTeamShortName: guest.ShortName,
			GuestTeamLogoPath:  guest.LogoPath,
		})
	}

	set, err := games.GetActiveSet(current.ID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		set = &storage.GameSet{SetNumber: current.CurrentSetNumber}
	}

	homeScore := set.HomeScore
	guestScore := set.GuestScore
	leftName := home.Name
	leftLabel := "Home Team"
	rightName := guest.Name
	rightLabel := "Guest Team"
	leftScore := homeScore
	rightScore := guestScore
	leftSets := current.HomeSetsWon
	rightSets := current.GuestSetsWon
	if current.HomeTeamSide == "right" {
		leftName = guest.Name
		leftLabel = "Guest Team"
		rightName = home.Name
		rightLabel = "Home Team"
		leftScore = guestScore
		rightScore = homeScore
		leftSets = current.GuestSetsWon
		rightSets = current.HomeSetsWon
	}

	return renderer.RenderLive(overlay.LiveViewModel{
		HomeTeamName:       home.Name,
		HomeTeamShortName:  home.ShortName,
		HomeTeamLogoPath:   home.LogoPath,
		GuestTeamName:      guest.Name,
		GuestTeamShortName: guest.ShortName,
		GuestTeamLogoPath:  guest.LogoPath,
		HomeScore:          homeScore,
		GuestScore:         guestScore,
		HomeSetsWon:        current.HomeSetsWon,
		GuestSetsWon:       current.GuestSetsWon,
		CurrentSetNumber:   current.CurrentSetNumber,
		LeftTeamName:       leftName,
		LeftTeamLabel:      leftLabel,
		RightTeamName:      rightName,
		RightTeamLabel:     rightLabel,
		LeftScore:          leftScore,
		RightScore:         rightScore,
		LeftSetsWon:        leftSets,
		RightSetsWon:       rightSets,
	})
}
