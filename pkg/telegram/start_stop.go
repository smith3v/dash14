package telegram

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// handleStart handles the /start command.
// It upserts the user record (creating it with Subscribed=true on first
// contact) and then explicitly sets Subscribed=true so that a returning user
// who previously ran /stop is re-subscribed.
func (r *Router) handleStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := senderID(update)
	chatID := update.Message.Chat.ID
	chatType := update.Message.Chat.Type

	username := ""
	displayName := ""
	if update.Message.From != nil {
		username = update.Message.From.Username
		displayName = telegramDisplayName(update.Message.From)
	}

	r.logger.InfoContext(ctx, "received /start", "user_id", userID)

	if chatType != models.ChatTypePrivate {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Subscriptions only work in a private chat with this bot. Open the bot directly and send /start.",
		})
		return
	}

	if err := r.users.UpsertTelegramUser(userID, username, displayName); err != nil {
		r.logger.ErrorContext(ctx, "handleStart: upsert user failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	if err := r.users.SetSubscription(userID, true); err != nil {
		r.logger.ErrorContext(ctx, "handleStart: set subscription failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "You are now subscribed to match updates.",
	})
}

// handleStop handles the /stop command.
// It upserts the user record (to ensure they exist) and then marks them as
// unsubscribed so they no longer receive broadcast messages.
func (r *Router) handleStop(ctx context.Context, b *bot.Bot, update *models.Update) {
	userID := senderID(update)
	chatID := update.Message.Chat.ID
	chatType := update.Message.Chat.Type

	username := ""
	displayName := ""
	if update.Message.From != nil {
		username = update.Message.From.Username
		displayName = telegramDisplayName(update.Message.From)
	}

	r.logger.InfoContext(ctx, "received /stop", "user_id", userID)

	if chatType != models.ChatTypePrivate {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Subscriptions only work in a private chat with this bot. Open the bot directly and send /stop.",
		})
		return
	}

	if err := r.users.UpsertTelegramUser(userID, username, displayName); err != nil {
		r.logger.ErrorContext(ctx, "handleStop: upsert user failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	if err := r.users.SetSubscription(userID, false); err != nil {
		r.logger.ErrorContext(ctx, "handleStop: set subscription failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "You have unsubscribed from match updates.",
	})
}

func telegramDisplayName(user *models.User) string {
	if user == nil {
		return ""
	}

	fullName := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	return fullName
}
