package telegram

import (
	"context"
	"log/slog"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/storage"
)

// Router dispatches Telegram updates to the correct handler.
// Handler dependencies (database repositories, renderer, etc.) will be added
// as fields here in subsequent tasks.
type Router struct {
	b      *bot.Bot
	logger *slog.Logger
	client BotClient
	users  *storage.UserRepository
	teams  *storage.TeamRepository
	plans  sync.Map // map[int64]*planState — keyed by Telegram user ID
}

// NewRouter creates a Router that registers handlers on b.
// client is the BotClient used to send messages (typically b itself, or a
// FakeBot in tests). users is the UserRepository for subscriber management.
// teams is the TeamRepository used by the /plan wizard.
func NewRouter(b *bot.Bot, logger *slog.Logger, client BotClient, users *storage.UserRepository, teams *storage.TeamRepository) *Router {
	return &Router{
		b:      b,
		logger: logger,
		client: client,
		users:  users,
		teams:  teams,
	}
}

// Register wires all command and callback handlers onto the bot.
// The implementations are stubs; full handler logic is added in later tasks.
func (r *Router) Register() {
	r.b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, r.handleStart)
	r.b.RegisterHandler(bot.HandlerTypeMessageText, "/stop", bot.MatchTypeExact, r.handleStop)
	r.b.RegisterHandler(bot.HandlerTypeMessageText, "/plan", bot.MatchTypeExact, r.handlePlan)
	r.b.RegisterHandler(bot.HandlerTypeMessageText, "/game", bot.MatchTypeExact, r.handleGame)
	r.b.RegisterHandler(bot.HandlerTypeMessageText, "/takeover", bot.MatchTypeExact, r.handleTakeover)
	// Non-command text is routed to the plan wizard when the user has an
	// active plan state; otherwise it is silently ignored.
	r.b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, r.handlePlanText)
	// Inline keyboard callbacks for the plan wizard.
	r.b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "plan:", bot.MatchTypePrefix, r.handlePlanCallback)
}

// handleGame is the stub handler for /game.
// It opens or refreshes the inline game control thread for admins.
func (r *Router) handleGame(ctx context.Context, b *bot.Bot, update *models.Update) {
	r.logger.InfoContext(ctx, "received /game", "user_id", senderID(update))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "not implemented yet",
	})
}

// handleTakeover is the stub handler for /takeover.
// It transfers game administration to the calling admin.
func (r *Router) handleTakeover(ctx context.Context, b *bot.Bot, update *models.Update) {
	r.logger.InfoContext(ctx, "received /takeover", "user_id", senderID(update))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "not implemented yet",
	})
}

// senderID returns the Telegram user ID from an update, or 0 if unavailable.
func senderID(update *models.Update) int64 {
	if update.Message != nil && update.Message.From != nil {
		return update.Message.From.ID
	}
	return 0
}
