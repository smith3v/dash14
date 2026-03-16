package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
)

// planState holds the conversation state for a single admin's in-progress /plan
// wizard. Fields are populated as the wizard advances through its steps.
type planState struct {
	// HomeTeam is set once the admin has selected (or auto-selected) the home
	// team. While it is nil the wizard is waiting for a home-team search query.
	HomeTeam *storage.Team
}

// planSearchLimit is the maximum number of search results we request from the
// repository. We ask for one more than the display cap (8) so we can
// distinguish between "exactly 8" and ">8" with a single query.
const planSearchLimit = 9

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
	if r.games == nil {
		r.logger.ErrorContext(ctx, "handlePlan: game repository is nil", "user_id", userID)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	nonFinished, err := r.games.GetNonFinishedGame()
	if err != nil {
		r.logger.ErrorContext(ctx, "handlePlan: failed to check current game",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	if nonFinished != nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Cannot run /plan: another game is still planned or in progress.",
		})
		return
	}

	// Reset (or create) the plan state for this admin.
	r.plans.Store(userID, &planState{})

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
		// Auto-select the single result.
		selected := &teams[0]
		state.HomeTeam = selected
		r.plans.Store(userID, state)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Home team: %s\nPlease enter the guest team name:", selected.Name),
		})

	case len(teams) <= 8:
		// Show an inline keyboard with one button per team.
		buttons := make([][]models.InlineKeyboardButton, 0, len(teams))
		for _, team := range teams {
			btn := models.InlineKeyboardButton{
				Text:         team.Name,
				CallbackData: fmt.Sprintf("plan:home:%d", team.ID),
			}
			buttons = append(buttons, []models.InlineKeyboardButton{btn})
		}
		keyboard := &models.InlineKeyboardMarkup{InlineKeyboard: buttons}
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "Select the home team:",
			ReplyMarkup: keyboard,
		})

	default:
		// More than 8 results — ask the admin to narrow the search.
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Too many results. Please refine your search.",
		})
	}
}

// handlePlanCallback handles inline keyboard callbacks whose data starts with
// "plan:". Currently only "plan:home:<teamID>" is handled; guest callbacks
// will be added in Task 17.
func (r *Router) handlePlanCallback(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID == 0 {
		return
	}

	userID := update.CallbackQuery.From.ID
	chatID := update.CallbackQuery.Message.Message.Chat.ID
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

	switch {
	case strings.HasPrefix(data, "plan:home:"):
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

		state.HomeTeam = team
		r.plans.Store(userID, state)

		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Home team: %s\nPlease enter the guest team name:", team.Name),
		})

	case strings.HasPrefix(data, "plan:guest:"):
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
		buttons := make([][]models.InlineKeyboardButton, 0, len(teams))
		for _, team := range teams {
			btn := models.InlineKeyboardButton{
				Text:         team.Name,
				CallbackData: fmt.Sprintf("plan:guest:%d", team.ID),
			}
			buttons = append(buttons, []models.InlineKeyboardButton{btn})
		}
		keyboard := &models.InlineKeyboardMarkup{InlineKeyboard: buttons}
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "Select the guest team:",
			ReplyMarkup: keyboard,
		})

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
	if r.games == nil || r.renderer == nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: dependencies missing",
			"user_id", userID, "has_games", r.games != nil, "has_renderer", r.renderer != nil)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	nonFinished, err := r.games.GetNonFinishedGame()
	if err != nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: check non-finished game failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	if nonFinished != nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Cannot run /plan: another game is still planned or in progress.",
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
	if err := r.games.CreateGame(game); err != nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: create game failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	if err := r.games.SetCurrentGameID(game.ID); err != nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: set current game failed",
			"user_id", userID, "game_id", game.ID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	vm := overlay.PlannedViewModel{
		HomeTeamName:       state.HomeTeam.Name,
		HomeTeamShortName:  state.HomeTeam.ShortName,
		HomeTeamLogoPath:   state.HomeTeam.LogoPath,
		GuestTeamName:      guestTeam.Name,
		GuestTeamShortName: guestTeam.ShortName,
		GuestTeamLogoPath:  guestTeam.LogoPath,
	}
	if err := r.renderer.RenderPlanned(vm); err != nil {
		r.logger.ErrorContext(ctx, "finalizePlannedGame: render planned overlay failed",
			"user_id", userID, "game_id", game.ID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Game was planned, but overlay rendering failed. Please check logs.",
		})
		return
	}

	r.plans.Delete(userID)
	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text: fmt.Sprintf(
			"Planned game created: %s vs %s.",
			state.HomeTeam.Name,
			guestTeam.Name,
		),
	})
}
