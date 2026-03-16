// Package telegram implements the Telegram bot for dash14.
// It wraps the github.com/go-telegram/bot library behind a small interface
// so that handlers can be tested without real network calls.
package telegram

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// BotClient covers the Telegram operations the application needs.
// The *bot.Bot concrete type satisfies this interface; tests can supply a fake.
type BotClient interface {
	// SendMessage sends a text message to a chat.
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
	// EditMessageText edits an existing message's text.
	EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error)
	// AnswerCallbackQuery answers an inline button callback.
	AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error)
}

// New creates a *bot.Bot using the provided token. The caller should call
// b.Start(ctx) to begin polling Telegram for updates.
//
// The returned *bot.Bot satisfies BotClient and is also used to register
// handlers via Router.Register.
func New(token string) (*bot.Bot, error) {
	if token == "" {
		return nil, fmt.Errorf("telegram: bot token must not be empty")
	}

	b, err := bot.New(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: create bot: %w", err)
	}

	return b, nil
}
