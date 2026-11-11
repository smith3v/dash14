package telegram

import (
	"log/slog"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
)

// GameStore captures the game persistence operations used by Telegram flows.
type GameStore interface {
	WithinTx(fn func(repo *storage.GameRepository) error) error
	CreateGame(game *storage.Game) error
	CreateSet(set *storage.GameSet) error
	GetCurrentGame() (*storage.Game, error)
	GetNonFinishedGame() (*storage.Game, error)
	GetGameByID(id uint) (*storage.Game, error)
	GetActiveSet(gameID uint) (*storage.GameSet, error)
	SaveGame(game *storage.Game) error
	SaveSet(set *storage.GameSet) error
}

// Router dispatches Telegram updates to the correct handler.
// Handler dependencies (database repositories, renderer, etc.) will be added
// as fields here in subsequent tasks.
type Router struct {
	b                   *bot.Bot
	logger              *slog.Logger
	client              BotClient
	users               *storage.UserRepository
	teams               *storage.TeamRepository
	games               GameStore
	gameChanged         func()
	plans               sync.Map // map[int64]*planState — keyed by Telegram user ID
	broadcastQueueSize  int
	broadcastJobs       chan broadcastJob
	broadcastWorkerOnce sync.Once
}

const defaultBroadcastQueueSize = 32

// NewRouter creates a Router that registers handlers on b.
// client is the BotClient used to send messages (typically b itself, or a
// FakeBot in tests). users is the UserRepository for subscriber management.
// teams is the TeamRepository used by the /plan wizard.
func NewRouter(b *bot.Bot, logger *slog.Logger, client BotClient, users *storage.UserRepository, teams *storage.TeamRepository) *Router {
	return &Router{
		b:                  b,
		logger:             logger,
		client:             client,
		users:              users,
		teams:              teams,
		broadcastQueueSize: defaultBroadcastQueueSize,
	}
}

// SetGameServices wires game-related dependencies that are optional in early
// tests but required by /plan completion and /game flows. gameChanged, when
// present, signals a committed change and must return without waiting for work.
func (r *Router) SetGameServices(games GameStore, gameChanged func()) {
	r.games = games
	r.gameChanged = gameChanged
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
