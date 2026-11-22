package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
)

// handleTakeover transfers game administration to the calling admin and
// invalidates any existing control thread. The new admin must run /game to
// open a fresh control message after takeover.
func (r *Router) handleTakeover(ctx context.Context, _ *bot.Bot, update *models.Update) {
	userID := senderID(update)
	chatID := update.Message.Chat.ID
	r.logger.InfoContext(ctx, "received /takeover", "user_id", userID)

	admin, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil || admin == nil {
		return
	}
	if r.games == nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	var previousAdminID int64
	err = r.games.WithinTx(func(repo *storage.GameRepository) error {
		if err := repo.RequireAdminVersion(userID, admin.AdminVersion); err != nil {
			return err
		}
		game, err := repo.GetCurrentGame()
		if err != nil {
			return err
		}
		if game == nil {
			return errNoCurrentGame
		}
		previousAdminID = game.CurrentAdminUserID
		return repo.SetGameControl(game, userID, 0, 0)
	})
	if errors.Is(err, errNoCurrentGame) {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "No planned or active game is available.",
		})
		return
	}
	if err != nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	if previousAdminID != 0 && previousAdminID != userID {
		newAdminLabel := "another admin"
		user, getUserErr := r.users.GetUserByTelegramID(userID)
		if getUserErr == nil {
			newAdminLabel = takeoverAdminLabel(user)
		}
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: previousAdminID,
			Text:   fmt.Sprintf("Game control was transferred to %s.", newAdminLabel),
		})
	}
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "Takeover successful. Run /game to open a fresh control message.",
	})
}

var errNoCurrentGame = errors.New("no current game")

func takeoverAdminLabel(user *storage.User) string {
	if user == nil {
		return "another admin"
	}
	if username := strings.TrimSpace(user.Username); username != "" {
		return "@" + username
	}
	if displayName := strings.TrimSpace(user.DisplayName); displayName != "" {
		return displayName
	}
	return "another admin"
}
