package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

// requireAdmin reads the current admin identity, including its role version.
// The caller must stop when it returns a nil user.
func (r *Router) requireAdmin(ctx context.Context, userID, chatID int64) (*storage.User, error) {
	user, err := r.users.GetUserByTelegramID(userID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("requireAdmin: get user %d: %w", userID, err)
	}
	if user != nil && user.IsAdmin {
		return user, nil
	}
	_, sendErr := r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "You are not authorised to use this command.",
	})
	if sendErr != nil {
		r.logger.WarnContext(ctx, "requireAdmin: send rejection failed",
			"user_id", userID, "chat_id", chatID, "err", sendErr)
	}
	return nil, nil
}
