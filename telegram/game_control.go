package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	gamepkg "github.com/smith3v/dash14/game"
	"github.com/smith3v/dash14/overlay"
	"github.com/smith3v/dash14/storage"
	"gorm.io/gorm"
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

	view := buildGameControlMessage(game, home.Name, guest.Name, activeSet)
	msg, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        view.Text,
		ReplyMarkup: view.Keyboard,
	})
	if err != nil {
		r.logger.ErrorContext(ctx, "handleGame: send control message failed", "user_id", userID, "err", err)
		return
	}

	game.CurrentAdminUserID = userID
	game.ControlMessageID = msg.ID
	if err := r.games.SaveGame(game); err != nil {
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
	data := cb.Data

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
	if game.ControlMessageID != 0 && messageID != game.ControlMessageID {
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

	broadcastText := ""

	switch data {
	case "game:start":
		if game.Status != storage.GameStatusPlanned {
			return
		}
		gState, setState, err := gamepkg.StartPlannedGame(toGameState(game))
		if err != nil {
			return
		}
		applyGameState(game, gState)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		newSet := &storage.GameSet{
			GameID:     game.ID,
			SetNumber:  setState.SetNumber,
			HomeScore:  setState.HomeScore,
			GuestScore: setState.GuestScore,
			IsFinished: false,
		}
		if err := r.games.CreateSet(newSet); err != nil {
			return
		}
		activeSet = newSet
		broadcastText = fmt.Sprintf("Game started: %s vs %s", home.Name, guest.Name)

	case "game:home:+1", "game:home:-1", "game:guest:+1", "game:guest:-1":
		if game.Status != storage.GameStatusInProgress || activeSet == nil {
			return
		}
		score := gamepkg.SetScore{
			HomeScore:          activeSet.HomeScore,
			GuestScore:         activeSet.GuestScore,
			SetNumber:          activeSet.SetNumber,
			SideSwitchedInSet5: game.SideSwitchedInSet5,
		}
		var result gamepkg.ScoreResult
		switch data {
		case "game:home:+1":
			result = gamepkg.IncrementHome(score)
		case "game:home:-1":
			result = gamepkg.DecrementHome(score)
		case "game:guest:+1":
			result = gamepkg.IncrementGuest(score)
		case "game:guest:-1":
			result = gamepkg.DecrementGuest(score)
		}

		activeSet.HomeScore = result.Set.HomeScore
		activeSet.GuestScore = result.Set.GuestScore
		if err := r.games.SaveSet(activeSet); err != nil {
			return
		}

		if result.SideSwitch {
			game.HomeTeamSide, game.GuestTeamSide = game.GuestTeamSide, game.HomeTeamSide
		}
		game.SideSwitchedInSet5 = result.Set.SideSwitchedInSet5
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		broadcastText = formatBroadcastScore(game, home, guest, activeSet)

	case "game:set:finish":
		if game.Status != storage.GameStatusInProgress || activeSet == nil {
			return
		}
		setState := gamepkg.SetState{
			SetScore: gamepkg.SetScore{
				HomeScore:          activeSet.HomeScore,
				GuestScore:         activeSet.GuestScore,
				SetNumber:          activeSet.SetNumber,
				SideSwitchedInSet5: game.SideSwitchedInSet5,
			},
			IsFinished: activeSet.IsFinished,
		}
		res, err := gamepkg.ConfirmSetFinished(toGameState(game), setState)
		if err != nil {
			return
		}
		activeSet.IsFinished = true
		if err := r.games.SaveSet(activeSet); err != nil {
			return
		}

		applyGameState(game, res.Game)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		if res.NextSet != nil {
			next := &storage.GameSet{
				GameID:     game.ID,
				SetNumber:  res.NextSet.SetNumber,
				HomeScore:  res.NextSet.HomeScore,
				GuestScore: res.NextSet.GuestScore,
				IsFinished: false,
			}
			if err := r.games.CreateSet(next); err != nil {
				return
			}
			activeSet = next
		} else {
			activeSet = nil
		}
		broadcastText = fmt.Sprintf(
			"Set %d finished: %s %d-%d %s | Sets %d-%d",
			setState.SetNumber,
			home.Name,
			setState.HomeScore,
			setState.GuestScore,
			guest.Name,
			game.HomeSetsWon,
			game.GuestSetsWon,
		)

	case "game:game:finish":
		res, err := gamepkg.ConfirmGameFinished(toGameState(game))
		if err != nil {
			return
		}
		applyGameState(game, res.Game)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		if err := r.games.ClearCurrentGameID(); err != nil {
			return
		}
		broadcastText = fmt.Sprintf("Game finished: %s vs %s", home.Name, guest.Name)

	case "game:reverse":
		res := gamepkg.ReverseOverlaySides(toGameState(game))
		applyGameState(game, res.Game)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		broadcastText = fmt.Sprintf("Overlay sides reversed for %s vs %s", home.Name, guest.Name)

	default:
		return
	}

	if err := r.renderOverlay(game, home, guest, activeSet); err != nil {
		r.logger.ErrorContext(ctx, "handleGameCallback: render overlay failed", "game_id", game.ID, "err", err)
	}

	view := buildGameControlMessage(game, home.Name, guest.Name, activeSet)
	_, _ = r.client.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   game.ControlMessageID,
		Text:        view.Text,
		ReplyMarkup: view.Keyboard,
	})

	if strings.TrimSpace(broadcastText) != "" {
		r.BroadcastExcept(ctx, broadcastText, game.CurrentAdminUserID)
	}
}

type controlView struct {
	Text     string
	Keyboard *models.InlineKeyboardMarkup
}

func buildGameControlMessage(game *storage.Game, homeName, guestName string, activeSet *storage.GameSet) controlView {
	homeScore := 0
	guestScore := 0
	finishable := false
	if activeSet != nil {
		homeScore = activeSet.HomeScore
		guestScore = activeSet.GuestScore
		finishable = gamepkg.IsSetFinishable(gamepkg.SetScore{
			HomeScore:          activeSet.HomeScore,
			GuestScore:         activeSet.GuestScore,
			SetNumber:          activeSet.SetNumber,
			SideSwitchedInSet5: game.SideSwitchedInSet5,
		})
	}

	text := fmt.Sprintf(
		"Game controls\nStatus: %s\nSet %d | Score %d-%d\nSets %d-%d\nHome: %s\nGuest: %s",
		game.Status,
		game.CurrentSetNumber,
		homeScore,
		guestScore,
		game.HomeSetsWon,
		game.GuestSetsWon,
		homeName,
		guestName,
	)

	rows := [][]models.InlineKeyboardButton{
		{
			{Text: fmt.Sprintf("%s -1", homeName), CallbackData: "game:home:-1"},
			{Text: fmt.Sprintf("%s +1", homeName), CallbackData: "game:home:+1"},
		},
		{
			{Text: fmt.Sprintf("%s -1", guestName), CallbackData: "game:guest:-1"},
			{Text: fmt.Sprintf("%s +1", guestName), CallbackData: "game:guest:+1"},
		},
	}
	if game.Status == storage.GameStatusPlanned {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Start the game", CallbackData: "game:start"},
		})
	}
	if finishable {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Is set finished?", CallbackData: "game:set:finish"},
		})
	}
	if game.HomeSetsWon >= 3 || game.GuestSetsWon >= 3 {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Is game finished?", CallbackData: "game:game:finish"},
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "Reverse overlay sides", CallbackData: "game:reverse"},
	})

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
	home, err := r.teams.GetTeamByID(game.HomeTeamID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	guest, err := r.teams.GetTeamByID(game.GuestTeamID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	activeSet, err := r.games.GetActiveSet(game.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return game, home, guest, nil, nil
		}
		return nil, nil, nil, nil, err
	}
	return game, home, guest, activeSet, nil
}

func toGameState(g *storage.Game) gamepkg.GameState {
	return gamepkg.GameState{
		HomeTeamID:         g.HomeTeamID,
		GuestTeamID:        g.GuestTeamID,
		HomeSide:           g.HomeTeamSide,
		GuestSide:          g.GuestTeamSide,
		HomeSetsWon:        g.HomeSetsWon,
		GuestSetsWon:       g.GuestSetsWon,
		CurrentSetNumber:   g.CurrentSetNumber,
		Status:             string(g.Status),
		SideSwitchedInSet5: g.SideSwitchedInSet5,
	}
}

func applyGameState(dst *storage.Game, src gamepkg.GameState) {
	dst.HomeTeamSide = src.HomeSide
	dst.GuestTeamSide = src.GuestSide
	dst.HomeSetsWon = src.HomeSetsWon
	dst.GuestSetsWon = src.GuestSetsWon
	dst.CurrentSetNumber = src.CurrentSetNumber
	dst.Status = storage.GameStatus(src.Status)
	dst.SideSwitchedInSet5 = src.SideSwitchedInSet5
}

func (r *Router) renderOverlay(game *storage.Game, home, guest *storage.Team, activeSet *storage.GameSet) error {
	if r.renderer == nil {
		return nil
	}
	if game.Status == storage.GameStatusPlanned {
		return r.renderer.RenderPlanned(overlay.PlannedViewModel{
			HomeTeamName:       home.Name,
			HomeTeamShortName:  home.ShortName,
			HomeTeamLogoPath:   home.LogoPath,
			GuestTeamName:      guest.Name,
			GuestTeamShortName: guest.ShortName,
			GuestTeamLogoPath:  guest.LogoPath,
		})
	}

	homeScore := 0
	guestScore := 0
	if activeSet != nil {
		homeScore = activeSet.HomeScore
		guestScore = activeSet.GuestScore
	}
	leftName := home.Name
	rightName := guest.Name
	leftScore := homeScore
	rightScore := guestScore
	leftSets := game.HomeSetsWon
	rightSets := game.GuestSetsWon
	if game.HomeTeamSide == "right" {
		leftName = guest.Name
		rightName = home.Name
		leftScore = guestScore
		rightScore = homeScore
		leftSets = game.GuestSetsWon
		rightSets = game.HomeSetsWon
	}

	return r.renderer.RenderLive(overlay.LiveViewModel{
		HomeTeamName:       home.Name,
		HomeTeamShortName:  home.ShortName,
		HomeTeamLogoPath:   home.LogoPath,
		GuestTeamName:      guest.Name,
		GuestTeamShortName: guest.ShortName,
		GuestTeamLogoPath:  guest.LogoPath,
		HomeScore:          homeScore,
		GuestScore:         guestScore,
		HomeSetsWon:        game.HomeSetsWon,
		GuestSetsWon:       game.GuestSetsWon,
		CurrentSetNumber:   game.CurrentSetNumber,
		LeftTeamName:       leftName,
		RightTeamName:      rightName,
		LeftScore:          leftScore,
		RightScore:         rightScore,
		LeftSetsWon:        leftSets,
		RightSetsWon:       rightSets,
	})
}

func formatBroadcastScore(game *storage.Game, home, guest *storage.Team, set *storage.GameSet) string {
	return fmt.Sprintf(
		"Set %d: %s %d-%d %s | Sets %d-%d",
		game.CurrentSetNumber,
		home.Name,
		set.HomeScore,
		set.GuestScore,
		guest.Name,
		game.HomeSetsWon,
		game.GuestSetsWon,
	)
}
