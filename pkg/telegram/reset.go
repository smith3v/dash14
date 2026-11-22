package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
)

func (r *Router) handleReset(ctx context.Context, _ *bot.Bot, update *models.Update) {
	userID := senderID(update)
	chatID := update.Message.Chat.ID
	r.logger.InfoContext(ctx, "received /reset", "user_id", userID)

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil {
		r.logger.ErrorContext(ctx, "handleReset: requireAdmin failed", "user_id", userID, "err", err)
		return
	}
	if !ok {
		return
	}

	text := r.resetGame(ctx, userID)
	if _, err := r.client.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		r.logger.WarnContext(ctx, "handleReset: send reply failed", "user_id", userID, "err", err)
	}
}

func (r *Router) resetGame(ctx context.Context, userID int64) string {
	const failed = "Unable to cancel the game. Please try again."
	if r.games == nil {
		r.logger.ErrorContext(ctx, "handleReset: game repository is missing")
		return failed
	}

	r.gameActionMu.Lock()
	defer r.gameActionMu.Unlock()

	var game *storage.Game
	err := r.games.WithinTxContext(ctx, func(repo *storage.GameRepository) error {
		current, err := repo.GetCurrentGame()
		if err != nil || current == nil {
			return err
		}
		game = current
		if game.CurrentAdminUserID != userID {
			return nil
		}
		game.Status = storage.GameStatusCanceled
		game.CurrentAdminUserID = 0
		game.ControlMessageID = 0
		game.ControlChatID = 0
		return repo.SaveGame(game)
	})
	if err != nil {
		r.logger.ErrorContext(ctx, "handleReset: cancel game failed", "user_id", userID, "err", err)
		return failed
	}
	if game == nil {
		return "No planned or active game to cancel."
	}
	if game.Status != storage.GameStatusCanceled {
		if game.CurrentAdminUserID == 0 {
			return "Run /game to take control before using /reset."
		}
		return "Only the current game admin can cancel it. Run /takeover to take control."
	}

	if r.gameChanged != nil {
		r.gameChanged()
	}
	return "Game canceled. Run /plan to plan the next game."
}
