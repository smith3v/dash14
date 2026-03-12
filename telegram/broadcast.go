package telegram

import (
	"context"

	"github.com/go-telegram/bot"
)

// Broadcast sends text to all subscribed users. Per-user failures are logged
// at warn level but do not abort the broadcast; all subscribed users are
// attempted regardless of earlier failures.
func (r *Router) Broadcast(ctx context.Context, text string) {
	users, err := r.users.ListSubscribedUsers()
	if err != nil {
		r.logger.ErrorContext(ctx, "Broadcast: list subscribed users failed", "err", err)
		return
	}

	for _, u := range users {
		_, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: u.TelegramUserID,
			Text:   text,
		})
		if err != nil {
			r.logger.WarnContext(ctx, "Broadcast: send to user failed",
				"user_id", u.TelegramUserID, "err", err)
		}
	}
}
