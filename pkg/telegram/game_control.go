package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	game2 "github.com/smith3v/dash14/pkg/game"
	"github.com/smith3v/dash14/pkg/storage"
)

// handleGame opens (or refreshes) the game-control thread for the current
// admin. If another admin currently owns the game, the caller receives
// takeover guidance instead.
func (r *Router) handleGame(ctx context.Context, _ *bot.Bot, update *models.Update) {
	userID := senderID(update)
	chatID := update.Message.Chat.ID
	r.logger.InfoContext(ctx, "received /game", "user_id", userID)

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil {
		r.logger.ErrorContext(ctx, "handleGame: requireAdmin failed", "user_id", userID, "err", err)
		return
	}
	if !ok {
		return
	}

	game, home, guest, activeSet, err := r.loadControlContext()
	if err != nil {
		r.logger.ErrorContext(ctx, "handleGame: load control context failed", "user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}
	if game == nil {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "No planned or active game is available.",
		})
		return
	}

	if game.CurrentAdminUserID != 0 && game.CurrentAdminUserID != userID {
		owner := "Another admin"
		u, ownerErr := r.users.GetUserByTelegramID(game.CurrentAdminUserID)
		if ownerErr == nil {
			if u.Username != "" {
				owner = "@" + u.Username
			} else {
				owner = fmt.Sprintf("Admin %d", game.CurrentAdminUserID)
			}
		}
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text: fmt.Sprintf(
				"%s currently manages the game. Run /takeover if you would like to manage the game. The game control will transfer to you.",
				owner,
			),
		})
		return
	}

	view := r.gameControlView(game, home, guest, activeSet, update.Message.Chat.Type == models.ChatTypePrivate)
	msg, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        view.Text,
		ReplyMarkup: view.Keyboard,
	})
	if err != nil {
		r.logger.ErrorContext(ctx, "handleGame: send control message failed", "user_id", userID, "err", err)
		return
	}

	if err := r.games.WithinTx(func(repo *storage.GameRepository) error {
		return repo.SetGameControl(game, userID, chatID, msg.ID)
	}); err != nil {
		r.logger.ErrorContext(ctx, "handleGame: save game failed", "game_id", game.ID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
	}
}

// handleGameCallback processes button actions from the current control message.
func (r *Router) handleGameCallback(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.Message.Message == nil {
		return
	}

	cb := update.CallbackQuery
	chatID := cb.Message.Message.Chat.ID
	messageID := cb.Message.Message.ID
	userID := cb.From.ID
	data, targetSet := decodeLifecycleCallback(cb.Data)

	_, _ = r.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
	})

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil || !ok {
		return
	}

	game, home, guest, activeSet, err := r.loadControlContext()
	if err != nil || game == nil {
		return
	}
	if !isControlMessage(game, chatID, messageID) {
		_, _ = r.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "This control message is stale. Run /game.",
		})
		return
	}
	if game.CurrentAdminUserID != userID {
		_, _ = r.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Only the current game admin can use these controls.",
		})
		return
	}

	expected := lifecycleRevision(game, activeSet)
	var committedGame *storage.Game
	var committedSet *storage.GameSet
	err = r.games.WithinTx(func(repo *storage.GameRepository) error {
		txGame, err := repo.GetGameByID(game.ID)
		if err != nil {
			return err
		}
		if txGame.Status == storage.GameStatusFinished || txGame.CurrentAdminUserID != userID ||
			!isControlMessage(txGame, chatID, messageID) {
			return errStaleGameControl
		}
		txSet, err := findActiveSet(repo, game.ID)
		if err != nil {
			return err
		}
		if lifecycleRevision(txGame, txSet) != expected ||
			(targetSet != 0 && txGame.CurrentSetNumber != targetSet) {
			return errStaleGameControl
		}

		switch {
		case isLifecycleAction(data):
			txSet, err = saveLifecycleAction(repo, txGame, txSet, data)
		case data == switchSides || isScoringAction(data) &&
			txGame.EffectivePhase(txSet != nil) == storage.GamePhaseSetInProgress:
			err = saveScoringAction(repo, txGame, txSet, data)
		default:
			return errUnhandledGameCallback
		}
		if err != nil {
			return err
		}

		committedGame, committedSet = txGame, txSet
		return nil
	})
	if errors.Is(err, errUnhandledGameCallback) {
		return
	}
	if errors.Is(err, errStaleGameControl) {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "This control message is stale. Run /game.",
		})
		return
	}
	if errors.Is(err, errSetAwaitingConfirmation) || errors.Is(err, errLifecycleActionUnavailable) {
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "That action is unavailable. Check the score and current set.",
		})
		return
	}
	if err != nil {
		r.logger.ErrorContext(ctx, "handleGameCallback: transactional write failed",
			"game_id", game.ID, "action", data, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Action failed. Match state was not changed. Please try again.",
		})
		return
	}

	if r.gameChanged != nil {
		r.gameChanged()
	}

	broadcastText := gameActionBroadcast(data, committedGame, home, guest, committedSet)
	r.editGameControl(ctx, committedGame.ID, chatID, cb.Message.Message.Chat.Type == models.ChatTypePrivate)

	if strings.TrimSpace(broadcastText) != "" {
		r.BroadcastExcept(ctx, broadcastText, committedGame.CurrentAdminUserID)
	}
}

var errUnhandledGameCallback = errors.New("telegram: unhandled game callback")

type controlView struct {
	Text     string
	Keyboard *models.InlineKeyboardMarkup
}

func buildGameControlMessage(game *storage.Game, homeName, guestName string, activeSet *storage.GameSet) controlView {
	phase := game.EffectivePhase(activeSet != nil && !activeSet.IsFinished)

	if phase == storage.GamePhaseFinished {
		text := fmt.Sprintf(
			"Game finished\nFinal sets %d-%d\nHome: %s\nGuest: %s",
			game.HomeSetsWon,
			game.GuestSetsWon,
			homeName,
			guestName,
		)
		return controlView{
			Text:     text,
			Keyboard: &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
		}
	}

	homeScore := 0
	guestScore := 0
	finishable := false
	if activeSet != nil {
		homeScore = activeSet.HomeScore
		guestScore = activeSet.GuestScore
		finishable = game2.IsSetFinishable(currentSetScore(game, activeSet))
	}

	text := fmt.Sprintf(
		"Game controls\nPhase: %s\nSet %d | Score %d-%d\nSets %d-%d\nHome: %s\nGuest: %s",
		phase,
		game.CurrentSetNumber,
		homeScore,
		guestScore,
		game.HomeSetsWon,
		game.GuestSetsWon,
		homeName,
		guestName,
	)

	rows := make([][]models.InlineKeyboardButton, 0, 5)
	canAdjustScore := phase == storage.GamePhaseSetInProgress && activeSet != nil
	if canAdjustScore {
		homeButtons := []models.InlineKeyboardButton{
			{Text: fmt.Sprintf("%s -1", homeName), CallbackData: scoreHomeDown},
		}
		guestButtons := []models.InlineKeyboardButton{
			{Text: fmt.Sprintf("%s -1", guestName), CallbackData: scoreGuestDown},
		}
		if !finishable {
			homeButtons = append(homeButtons, models.InlineKeyboardButton{
				Text: fmt.Sprintf("%s +1", homeName), CallbackData: scoreHomeUp,
			})
			guestButtons = append(guestButtons, models.InlineKeyboardButton{
				Text: fmt.Sprintf("%s +1", guestName), CallbackData: scoreGuestUp,
			})
		}
		rows = append(rows, homeButtons, guestButtons)
	}
	if action := nextLifecycleAction(game, activeSet); action != "" {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: lifecycleActionLabel(action), CallbackData: fmt.Sprintf("%s:%d", action, game.CurrentSetNumber)},
		})
	}
	if phase != storage.GamePhaseFinished {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Reverse overlay sides", CallbackData: "game:reverse"},
		})
	}

	return controlView{
		Text:     text,
		Keyboard: &models.InlineKeyboardMarkup{InlineKeyboard: rows},
	}
}

func (r *Router) loadControlContext() (*storage.Game, *storage.Team, *storage.Team, *storage.GameSet, error) {
	if r.games == nil || r.teams == nil {
		return nil, nil, nil, nil, fmt.Errorf("game or team repository is nil")
	}
	game, err := r.games.GetCurrentGame()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if game == nil {
		return nil, nil, nil, nil, nil
	}
	home, guest, err := r.loadGameTeams(game)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	activeSet, err := findActiveSet(r.games, game.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return game, home, guest, activeSet, nil
}

func toGameState(g *storage.Game, phase storage.GamePhase) game2.GameState {
	return game2.GameState{
		HomeTeamID:         g.HomeTeamID,
		GuestTeamID:        g.GuestTeamID,
		HomeSide:           g.HomeTeamSide,
		GuestSide:          g.GuestTeamSide,
		HomeSetsWon:        g.HomeSetsWon,
		GuestSetsWon:       g.GuestSetsWon,
		CurrentSetNumber:   g.CurrentSetNumber,
		Status:             string(g.Status),
		Phase:              string(phase),
		SideSwitchedInSet5: g.SideSwitchedInSet5,
	}
}

func applyGameState(dst *storage.Game, src game2.GameState) {
	dst.HomeTeamSide = src.HomeSide
	dst.GuestTeamSide = src.GuestSide
	dst.HomeSetsWon = src.HomeSetsWon
	dst.GuestSetsWon = src.GuestSetsWon
	dst.CurrentSetNumber = src.CurrentSetNumber
	dst.Status = storage.GameStatus(src.Status)
	dst.SideSwitchedInSet5 = src.SideSwitchedInSet5
}

func formatBroadcastScore(game *storage.Game, home, guest *storage.Team, set *storage.GameSet) string {
	return fmt.Sprintf(
		"🏠 %s <b>%d-%d</b> ✈️ %s\n<i>Current set: %d | Game score: %d-%d</i>",
		home.Name,
		set.HomeScore,
		set.GuestScore,
		guest.Name,
		game.CurrentSetNumber,
		game.HomeSetsWon,
		game.GuestSetsWon,
	)
}
