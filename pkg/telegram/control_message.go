package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

var errStaleGameControl = errors.New("telegram: stale game control message")

func isControlMessage(match *storage.Game, chatID int64, messageID int) bool {
	return match.ControlMessageID != 0 && match.ControlMessageID == messageID &&
		(match.ControlChatID == 0 || match.ControlChatID == chatID)
}

type activeSetReader interface {
	GetActiveSet(gameID uint) (*storage.GameSet, error)
}

func findActiveSet(repo activeSetReader, gameID uint) (*storage.GameSet, error) {
	set, err := repo.GetActiveSet(gameID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return set, err
}

func (r *Router) gameControlView(match *storage.Game, home, guest *storage.Team, set *storage.GameSet, private bool) controlView {
	view := buildGameControlMessage(match, home.Name, guest.Name, set)
	if private && r.scoringPanelURL != "" && match.Status != storage.GameStatusFinished {
		view.Keyboard.InlineKeyboard = append([][]models.InlineKeyboardButton{
			{{Text: "Open scoring panel", WebApp: &models.WebAppInfo{URL: fmt.Sprintf("%s?gameId=%d", r.scoringPanelURL, match.ID)}}},
		}, view.Keyboard.InlineKeyboard...)
	}
	return view
}

// Fetch fresh state under the edit lock so a delayed edit cannot replace a
// more recent bot control with an earlier score or set.
func (r *Router) editGameControl(ctx context.Context, gameID uint, fallbackChatID int64, fallbackPrivate bool) {
	r.controlEditMu.Lock()
	defer r.controlEditMu.Unlock()

	match, err := r.games.GetGameByID(gameID)
	if err != nil {
		r.logger.ErrorContext(ctx, "load game for control message", "game_id", gameID, "err", err)
		return
	}
	chatID, private := fallbackChatID, fallbackPrivate
	if match.ControlChatID != 0 {
		chatID = match.ControlChatID
		private = chatID == match.CurrentAdminUserID
	}
	if match.ControlMessageID == 0 || chatID == 0 {
		return
	}
	home, guest, err := r.loadGameTeams(match)
	if err != nil {
		r.logger.ErrorContext(ctx, "load teams for control message", "game_id", gameID, "err", err)
		return
	}
	set, err := findActiveSet(r.games, gameID)
	if err != nil {
		r.logger.ErrorContext(ctx, "load set for control message", "game_id", gameID, "err", err)
		return
	}

	view := r.gameControlView(match, home, guest, set, private)
	_, err = r.client.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   match.ControlMessageID,
		Text:        view.Text,
		ReplyMarkup: view.Keyboard,
	})
	if err != nil {
		r.logger.ErrorContext(ctx, "edit game control message", "game_id", gameID, "message_id", match.ControlMessageID, "err", err)
	}
}

func (r *Router) loadGameTeams(match *storage.Game) (*storage.Team, *storage.Team, error) {
	home, err := r.teams.GetTeamByID(match.HomeTeamID)
	if err != nil {
		return nil, nil, err
	}
	guest, err := r.teams.GetTeamByID(match.GuestTeamID)
	if err != nil {
		return nil, nil, err
	}
	return home, guest, nil
}
