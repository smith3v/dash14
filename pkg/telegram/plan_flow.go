package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
)

// planState holds the conversation state for a single admin's in-progress /plan
// wizard. Fields are populated as the wizard advances through its steps.
type planState struct {
	// HomeTeam is set once the admin has selected (or auto-selected) the home
	// team. While it is nil the wizard is waiting for a home-team search query.
	HomeTeam             *storage.Team
	Replacement          *plannedGameReplacement
	SelectionChatID      int64
	SelectionMessageID   int
	LatestCanceledGameID uint
	Saving               bool
}

type plannedGameReplacement struct {
	GameID                   uint
	ExpectedHomeTeamID       uint
	ExpectedGuestTeamID      uint
	PreviousAdminUserID      int64
	PreviousControlMessageID int
	ConfirmationChatID       int64
	ConfirmationMessageID    int
	AwaitingConfirmation     bool
}

// planSearchLimit is the maximum number of search results we request from the
// repository. We ask for one more than the display cap (8) so we can
// distinguish between "exactly 8" and ">8" with a single query.
const planSearchLimit = 9

var errPlanPredatesCancellation = errors.New("telegram: plan started before game cancellation")

// handlePlan handles the /plan command.
// Only admins are permitted. On success it initialises a fresh plan state for
// the user and asks them to enter the home team name.
func (r *Router) handlePlan(ctx context.Context, _ *bot.Bot, update *models.Update) {
	userID := senderID(update)
	chatID := update.Message.Chat.ID

	r.logger.InfoContext(ctx, "received /plan", "user_id", userID)

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil {
		r.logger.ErrorContext(ctx, "handlePlan: requireAdmin failed",
			"user_id", userID, "err", err)
		return
	}
	if !ok {
		return
	}
	if r.games == nil || r.teams == nil {
		r.logger.ErrorContext(ctx, "handlePlan: dependencies missing",
			"user_id", userID, "has_games", r.games != nil, "has_teams", r.teams != nil)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	// A new /plan command always supersedes this admin's previous wizard.
	r.plans.Delete(userID)

	current, err := r.games.GetCurrentGame()
	if err != nil {
		r.logger.ErrorContext(ctx, "handlePlan: get current game failed", "user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	if current == nil {
		canceledID, err := r.games.LatestCanceledGameID()
		if err != nil {
			r.logger.ErrorContext(ctx, "handlePlan: get latest canceled game failed", "user_id", userID, "err", err)
			_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "Something went wrong. Please try again.",
			})
			return
		}
		r.plans.Store(userID, &planState{LatestCanceledGameID: canceledID})
		r.promptPlanHomeTeam(ctx, chatID)
		return
	}

	home, err := r.teams.GetTeamByID(current.HomeTeamID)
	if err != nil {
		r.logger.ErrorContext(ctx, "handlePlan: get current home team failed",
			"user_id", userID, "game_id", current.ID, "team_id", current.HomeTeamID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	guest, err := r.teams.GetTeamByID(current.GuestTeamID)
	if err != nil {
		r.logger.ErrorContext(ctx, "handlePlan: get current guest team failed",
			"user_id", userID, "game_id", current.ID, "team_id", current.GuestTeamID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	switch current.Status {
	case storage.GameStatusInProgress:
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(
				"A game is currently in progress: %s vs %s.\nFinish this game or use /reset before planning another one.",
				home.Name,
				guest.Name,
			),
		})

	case storage.GameStatusPlanned:
		state := &planState{Replacement: &plannedGameReplacement{
			GameID:                   current.ID,
			ExpectedHomeTeamID:       current.HomeTeamID,
			ExpectedGuestTeamID:      current.GuestTeamID,
			PreviousAdminUserID:      current.CurrentAdminUserID,
			PreviousControlMessageID: current.ControlMessageID,
			ConfirmationChatID:       chatID,
			AwaitingConfirmation:     true,
		}}
		msg, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(
				"A game is already planned: %s vs %s.\nWould you like to plan another game instead?",
				home.Name,
				guest.Name,
			),
			ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
				{Text: "Yes, plan another", CallbackData: "plan:replace:start"},
				{Text: "No, keep current", CallbackData: "plan:replace:keep"},
			}}},
		})
		if err != nil || msg == nil {
			r.logger.ErrorContext(ctx, "handlePlan: send replacement confirmation failed",
				"user_id", userID, "game_id", current.ID, "err", err)
			return
		}
		state.Replacement.ConfirmationMessageID = msg.ID
		r.plans.Store(userID, state)

	default:
		r.logger.ErrorContext(ctx, "handlePlan: unsupported current game status",
			"user_id", userID, "game_id", current.ID, "status", current.Status)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
	}
}

func (r *Router) promptPlanHomeTeam(ctx context.Context, chatID int64) {
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "Please enter the home team name:",
	})
}

// handlePlanText handles non-command plan input for users who have an active
// /plan session. Unknown bot commands are answered with the default help
// message; plain text without an active plan state is ignored.
//
// This handler is registered with MatchTypePrefix and pattern "" so it
// catches every non-command message. Command messages are intercepted by the
// more-specific exact-match handlers before reaching this handler.
func (r *Router) handlePlanText(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	// Exact-match handlers process known commands before this catch-all handler.
	// Any bot command that still reaches this point is unknown and should get
	// the default help response.
	if update.Message.Text != "" && len(update.Message.Entities) > 0 {
		for _, e := range update.Message.Entities {
			if e.Type == models.MessageEntityTypeBotCommand && e.Offset == 0 {
				r.handleUnknownCommand(ctx, update)
				return
			}
		}
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	query := strings.TrimSpace(update.Message.Text)

	raw, ok := r.plans.Load(userID)
	if !ok {
		// No active plan session for this user; ignore the message.
		return
	}
	state := raw.(*planState)
	if state.Saving || (state.Replacement != nil && state.Replacement.AwaitingConfirmation) {
		return
	}

	if state.HomeTeam == nil {
		r.searchAndSelectHomeTeam(ctx, userID, chatID, query, state)
		return
	}

	r.searchAndSelectGuestTeam(ctx, userID, chatID, query, state)
}

// searchAndSelectHomeTeam performs a team search for the home-team step of the
// wizard and applies the appropriate response:
//
//   - 0 results  → ask the admin to try again
//   - 1 result   → auto-select and confirm, then ask for the guest team
//   - 2–8 results → present an inline keyboard so the admin can pick
//   - >8 results  → ask the admin to refine the query
func (r *Router) searchAndSelectHomeTeam(
	ctx context.Context,
	userID, chatID int64,
	query string,
	state *planState,
) {
	if r.teams == nil {
		r.logger.ErrorContext(ctx, "searchAndSelectHomeTeam: teams repository is nil", "user_id", userID)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	teams, err := r.teams.SearchTeams(query, planSearchLimit)
	if err != nil {
		r.logger.ErrorContext(ctx, "searchAndSelectHomeTeam: search failed",
			"user_id", userID, "query", query, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	switch {
	case len(teams) == 0:
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "No teams found. Try again.",
		})

	case len(teams) == 1:
		r.selectPlanHomeTeam(ctx, userID, chatID, state, &teams[0])

	case len(teams) <= 8:
		r.sendPlanTeamChoices(ctx, userID, chatID, state, "home", teams)

	default:
		// More than 8 results — ask the admin to narrow the search.
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Too many results. Please refine your search.",
		})
	}
}

func (r *Router) sendPlanTeamChoices(ctx context.Context, userID, chatID int64, state *planState, step string, teams []storage.Team) {
	buttons := make([][]models.InlineKeyboardButton, 0, len(teams))
	for _, team := range teams {
		buttons = append(buttons, []models.InlineKeyboardButton{{
			Text:         team.Name,
			CallbackData: fmt.Sprintf("plan:%s:%d", step, team.ID),
		}})
	}
	msg, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        fmt.Sprintf("Select the %s team:", step),
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: buttons},
	})
	if err != nil || msg == nil {
		r.logger.WarnContext(ctx, "send plan team choices failed", "user_id", userID, "step", step, "err", err)
		return
	}
	next := *state
	next.SelectionChatID = chatID
	next.SelectionMessageID = msg.ID
	r.plans.CompareAndSwap(userID, state, &next)
}

func (r *Router) selectPlanHomeTeam(ctx context.Context, userID, chatID int64, state *planState, team *storage.Team) {
	next := *state
	next.HomeTeam = team
	next.SelectionChatID = 0
	next.SelectionMessageID = 0
	if !r.plans.CompareAndSwap(userID, state, &next) {
		return
	}
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Home team: %s\nPlease enter the guest team name:", team.Name),
	})
}

// handlePlanCallback handles team selections and replacement confirmations.
func (r *Router) handlePlanCallback(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID == 0 || update.CallbackQuery.Message.Message == nil {
		return
	}

	userID := update.CallbackQuery.From.ID
	message := update.CallbackQuery.Message.Message
	chatID := message.Chat.ID
	data := update.CallbackQuery.Data

	// Acknowledge the callback query so Telegram removes the loading indicator.
	_, _ = r.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	})

	raw, ok := r.plans.Load(userID)
	if !ok {
		return
	}
	state := raw.(*planState)
	if state.Saving {
		return
	}

	switch {
	case data == "plan:replace:keep":
		if !matchesReplacementConfirmation(state.Replacement, chatID, message.ID) {
			return
		}
		if !r.plans.CompareAndDelete(userID, state) {
			return
		}
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "The current planned game was kept.",
		})

	case data == "plan:replace:start":
		if !matchesReplacementConfirmation(state.Replacement, chatID, message.ID) {
			return
		}
		current, err := r.games.GetCurrentGame()
		if err != nil {
			r.logger.ErrorContext(ctx, "handlePlanCallback: get current replacement game failed",
				"user_id", userID, "game_id", state.Replacement.GameID, "err", err)
			if !r.plans.CompareAndDelete(userID, state) {
				return
			}
			_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "Something went wrong. Please run /plan again.",
			})
			return
		}
		if !matchesPlannedGameReplacement(current, state.Replacement) {
			if !r.plans.CompareAndDelete(userID, state) {
				return
			}
			_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "The planned game changed and cannot be replaced. Please run /plan again.",
			})
			return
		}
		next := *state
		replacement := *state.Replacement
		replacement.AwaitingConfirmation = false
		next.Replacement = &replacement
		if r.plans.CompareAndSwap(userID, state, &next) {
			r.promptPlanHomeTeam(ctx, chatID)
		}

	case strings.HasPrefix(data, "plan:home:"):
		if state.HomeTeam != nil || !state.matchesTeamSelection(chatID, message.ID) {
			return
		}
		idStr := strings.TrimPrefix(data, "plan:home:")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			r.logger.WarnContext(ctx, "handlePlanCallback: invalid team id in callback",
				"user_id", userID, "data", data)
			return
		}

		team, err := r.teams.GetTeamByID(uint(id))
		if err != nil {
			r.logger.ErrorContext(ctx, "handlePlanCallback: get team by id failed",
				"user_id", userID, "team_id", id, "err", err)
			_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "Something went wrong. Please try again.",
			})
			return
		}

		r.selectPlanHomeTeam(ctx, userID, chatID, state, team)

	case strings.HasPrefix(data, "plan:guest:"):
		if state.HomeTeam == nil || !state.matchesTeamSelection(chatID, message.ID) {
			return
		}
		idStr := strings.TrimPrefix(data, "plan:guest:")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			r.logger.WarnContext(ctx, "handlePlanCallback: invalid guest team id in callback",
				"user_id", userID, "data", data)
			return
		}
		team, err := r.teams.GetTeamByID(uint(id))
		if err != nil {
			r.logger.ErrorContext(ctx, "handlePlanCallback: get guest team by id failed",
				"user_id", userID, "team_id", id, "err", err)
			_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "Something went wrong. Please try again.",
			})
			return
		}
		r.finalizePlannedGame(ctx, userID, chatID, state, team)
	}
}

func (s *planState) matchesTeamSelection(chatID int64, messageID int) bool {
	return s.SelectionMessageID != 0 && s.SelectionMessageID == messageID && s.SelectionChatID == chatID
}

func matchesReplacementConfirmation(replacement *plannedGameReplacement, chatID int64, messageID int) bool {
	return replacement != nil &&
		replacement.AwaitingConfirmation &&
		replacement.ConfirmationChatID == chatID &&
		replacement.ConfirmationMessageID != 0 &&
		replacement.ConfirmationMessageID == messageID
}

func matchesPlannedGameReplacement(game *storage.Game, replacement *plannedGameReplacement) bool {
	return game != nil &&
		game.ID == replacement.GameID &&
		game.Status == storage.GameStatusPlanned &&
		game.HomeTeamID == replacement.ExpectedHomeTeamID &&
		game.GuestTeamID == replacement.ExpectedGuestTeamID
}

// searchAndSelectGuestTeam performs the guest-team search step. It mirrors the
// home-team flow, but finalises game creation once a guest team is selected.
func (r *Router) searchAndSelectGuestTeam(
	ctx context.Context,
	userID, chatID int64,
	query string,
	state *planState,
) {
	if r.teams == nil {
		r.logger.ErrorContext(ctx, "searchAndSelectGuestTeam: teams repository is nil", "user_id", userID)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	teams, err := r.teams.SearchTeams(query, planSearchLimit)
	if err != nil {
		r.logger.ErrorContext(ctx, "searchAndSelectGuestTeam: search failed",
			"user_id", userID, "query", query, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	switch {
	case len(teams) == 0:
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "No teams found. Try again.",
		})

	case len(teams) == 1:
		selected := &teams[0]
		r.finalizePlannedGame(ctx, userID, chatID, state, selected)

	case len(teams) <= 8:
		r.sendPlanTeamChoices(ctx, userID, chatID, state, "guest", teams)

	default:
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Too many results. Please refine your search.",
		})
	}
}

// finalizePlannedGame completes the /plan flow by creating a planned game and
// rendering the planned overlay.
func (r *Router) finalizePlannedGame(
	ctx context.Context,
	userID, chatID int64,
	state *planState,
	guestTeam *storage.Team,
) {
	if state.HomeTeam == nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Please select the home team first.",
		})
		return
	}
	if r.games == nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: dependencies missing",
			"user_id", userID, "has_games", r.games != nil)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	game := &storage.Game{
		HomeTeamID:         state.HomeTeam.ID,
		GuestTeamID:        guestTeam.ID,
		HomeTeamSide:       "left",
		GuestTeamSide:      "right",
		CurrentSetNumber:   1,
		Status:             storage.GameStatusPlanned,
		CurrentAdminUserID: userID,
	}
	isReplacement := state.Replacement != nil
	if isReplacement {
		game.ID = state.Replacement.GameID
	}

	saving := *state
	saving.Saving = true
	if !r.plans.CompareAndSwap(userID, state, &saving) {
		return
	}

	if err := r.games.WithinTx(func(repo *storage.GameRepository) error {
		if isReplacement {
			return repo.ReplacePlannedGame(
				state.Replacement.GameID,
				state.Replacement.ExpectedHomeTeamID,
				state.Replacement.ExpectedGuestTeamID,
				state.HomeTeam.ID,
				guestTeam.ID,
				userID,
			)
		}
		canceledID, err := repo.LatestCanceledGameID()
		if err != nil {
			return err
		}
		if canceledID != state.LatestCanceledGameID {
			return errPlanPredatesCancellation
		}
		return repo.CreateGame(game)
	}); err != nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: transactional write failed",
			"user_id", userID, "game_id", game.ID, "is_replacement", isReplacement, "err", err)
		text := "Planning failed. Match state was not changed. Please try again."
		switch {
		case errors.Is(err, storage.ErrPlannedGameChanged):
			r.plans.CompareAndDelete(userID, &saving)
			text = "The planned game changed and was not replaced. Please run /plan again."
		case errors.Is(err, errPlanPredatesCancellation):
			r.plans.CompareAndDelete(userID, &saving)
			text = "A game was canceled while you were planning. Please run /plan again."
		default:
			r.plans.CompareAndSwap(userID, &saving, state)
			if !isReplacement && isSingleActiveGameConstraintError(err) {
				text = "Cannot run /plan: another game is still planned or in progress."
			}
		}
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   text,
		})
		return
	}

	r.plans.CompareAndDelete(userID, &saving)
	if r.gameChanged != nil {
		r.gameChanged()
	}

	if isReplacement &&
		state.Replacement.PreviousControlMessageID != 0 &&
		state.Replacement.PreviousAdminUserID == userID &&
		chatID == userID {
		_, err := r.client.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: state.Replacement.PreviousControlMessageID,
			Text:      "This planned game was replaced. Run /game for current controls.",
			ReplyMarkup: &models.InlineKeyboardMarkup{
				InlineKeyboard: [][]models.InlineKeyboardButton{},
			},
		})
		if err != nil {
			r.logger.ErrorContext(ctx, "finalizePlannedGame: disable previous control message failed",
				"user_id", userID,
				"game_id", game.ID,
				"message_id", state.Replacement.PreviousControlMessageID,
				"err", err,
			)
		}
	}

	result := "created"
	if isReplacement {
		result = "updated"
	}
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(
			"Planned game %s: %s vs %s.",
			result,
			state.HomeTeam.Name,
			guestTeam.Name,
		),
	})
}

func isSingleActiveGameConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "idx_games_single_current") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}
