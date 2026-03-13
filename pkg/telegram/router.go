package telegram

import (
	"log/slog"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
)

// OverlayRenderer captures the overlay rendering operations used by telegram
// handlers when game state changes.
type OverlayRenderer interface {
	RenderPlanned(vm overlay.PlannedViewModel) error
	RenderLive(vm overlay.LiveViewModel) error
}

// Router dispatches Telegram updates to the correct handler.
// Handler dependencies (database repositories, renderer, etc.) will be added
// as fields here in subsequent tasks.
type Router struct {
	b        *bot.Bot
	logger   *slog.Logger
	client   BotClient
	users    *storage.UserRepository
	teams    *storage.TeamRepository
	games    *storage.GameRepository
	renderer OverlayRenderer
	plans    sync.Map // map[int64]*planState — keyed by Telegram user ID
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

// SetGameServices wires game-related dependencies that are optional in early
// tests but required by /plan completion and /game flows.
func (r *Router) SetGameServices(games *storage.GameRepository, renderer OverlayRenderer) {
	r.games = games
	r.renderer = renderer
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
	// Inline callbacks for the /game control message.
	r.b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "game:", bot.MatchTypePrefix, r.handleGameCallback)
}

// senderID returns the Telegram user ID from an update, or 0 if unavailable.
func senderID(update *models.Update) int64 {
	if update.Message != nil && update.Message.From != nil {
		return update.Message.From.ID
	}
	return 0
}
