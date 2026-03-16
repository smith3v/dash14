package telegram

import (
	"context"

	"github.com/go-telegram/bot"
)

// Broadcast sends text to all subscribed users. Per-user failures are logged
// at warn level but do not abort the broadcast; all subscribed users are
// attempted regardless of earlier failures.
func (r *Router) Broadcast(ctx context.Context, text string) {
	r.BroadcastExcept(ctx, text)
}

// BroadcastExcept sends text to all subscribed users except explicitly
// excluded Telegram user IDs.
func (r *Router) BroadcastExcept(ctx context.Context, text string, excludeUserIDs ...int64) {
	users, err := r.users.ListSubscribedUsers()
	if err != nil {
		r.logger.ErrorContext(ctx, "Broadcast: list subscribed users failed", "err", err)
		return
	}

	excluded := make(map[int64]struct{}, len(excludeUserIDs))
	for _, id := range excludeUserIDs {
		if id == 0 {
			continue
		}
		excluded[id] = struct{}{}
	}

	for _, u := range users {
		if _, skip := excluded[u.TelegramUserID]; skip {
			continue
		}
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
