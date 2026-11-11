package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

type overlayPublisher struct {
	db       *gorm.DB
	renderer *overlay.Renderer
	web      *overlay.Web
	changed  chan struct{}
}

func newOverlayPublisher(db *gorm.DB, renderer *overlay.Renderer) *overlayPublisher {
	return &overlayPublisher{db: db, renderer: renderer, web: overlay.NewWeb(renderer), changed: make(chan struct{}, 1)}
}

func (p *overlayPublisher) notify() {
	select {
	case p.changed <- struct{}{}:
	default:
	}
}

func (p *overlayPublisher) run(ctx context.Context, logger *slog.Logger, interval time.Duration) {
	var refresh <-chan time.Time
	if interval > 0 {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		refresh = ticker.C
	}
	var retry <-chan time.Time
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-p.changed:
		case <-retry:
		case <-refresh:
			if err := p.renderer.RefreshTemplates(); err != nil {
				logger.Error("overlay template refresh failed", "err", err)
				continue
			}
		}

		if err := p.publish(ctx, logger); err != nil {
			logger.Error("overlay publication failed", "err", err)
			retry = time.After(2 * time.Second)
		} else {
			retry = nil
		}
	}
}

func (p *overlayPublisher) publish(ctx context.Context, logger *slog.Logger) error {
	var match *overlay.Match
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		games := storage.NewGameRepository(tx)
		game, err := games.GetCurrentGame()
		if err != nil {
			return err
		}
		if game == nil {
			game = &storage.Game{}
			err = tx.Where("status = ?", storage.GameStatusFinished).Order("id DESC").First(game).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
		}

		teams := storage.NewTeamRepository(tx)
		home, err := teams.GetTeamByID(game.HomeTeamID)
		if err != nil {
			return err
		}
		guest, err := teams.GetTeamByID(game.GuestTeamID)
		if err != nil {
			return err
		}
		sets, err := games.ListSetsByGameID(game.ID)
		if err != nil {
			return err
		}
		match = &overlay.Match{Game: *game, Home: teamIdentity(home), Guest: teamIdentity(guest), Sets: sets}
		return nil
	})
	if err != nil {
		return err
	}
	if err := p.web.Publish(match); err != nil {
		return err
	}
	if err := p.web.Export(); err != nil {
		logger.Warn("overlay export failed", "err", err)
	}
	return nil
}

func teamIdentity(team *storage.Team) overlay.TeamIdentity {
	return overlay.TeamIdentity{Name: team.Name, ShortName: team.ShortName, Hometown: team.Hometown, LogoPath: team.LogoPath}
}
