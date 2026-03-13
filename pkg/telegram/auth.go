package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot"
	"gorm.io/gorm"
)

// requireAdmin checks whether the Telegram user identified by userID is an
// admin. If the user is not found or is not an admin, it sends a rejection
// message to chatID and returns (false, nil). On a database error it returns
// (false, err). The caller must return immediately when this returns false.
func (r *Router) requireAdmin(ctx context.Context, userID int64, chatID int64) (bool, error) {
	user, err := r.users.GetUserByTelegramID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_, sendErr := r.client.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "You are not authorised to use this command.",
			})
			if sendErr != nil {
				r.logger.WarnContext(ctx, "requireAdmin: send rejection failed",
					"user_id", userID, "err", sendErr)
			}
			return false, nil
		}
		return false, fmt.Errorf("requireAdmin: get user %d: %w", userID, err)
	}

	if !user.IsAdmin {
		_, sendErr := r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "You are not authorised to use this command.",
		})
		if sendErr != nil {
			r.logger.WarnContext(ctx, "requireAdmin: send rejection failed",
				"user_id", userID, "err", sendErr)
		}
		return false, nil
	}

	return true, nil
}
