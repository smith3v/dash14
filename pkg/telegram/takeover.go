package telegram

import (
	"context"
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

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil || !ok {
		return
	}
	if r.games == nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	game, err := r.games.GetCurrentGame()
	if err != nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	if game == nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "No planned or active game is available.",
		})
		return
	}

	previousAdminID := game.CurrentAdminUserID
	game.CurrentAdminUserID = userID
	game.ControlMessageID = 0
	if err := r.games.SaveGame(game); err != nil {
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
			Text:   fmt.Sprintf("Game control was transferred to admin %s.", newAdminLabel),
		})
	}
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "Takeover successful. Run /game to open a fresh control message.",
	})
}

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
