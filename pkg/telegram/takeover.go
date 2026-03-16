package telegram

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
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
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: previousAdminID,
			Text:   fmt.Sprintf("Game control was transferred to admin %d.", userID),
		})
	}
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "Takeover successful. Run /game to open a fresh control message.",
	})
}
