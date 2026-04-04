package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	game2 "github.com/smith3v/dash14/pkg/game"
	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
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
	var overlayJob *overlayJob
	phase := game.EffectivePhase(activeSet != nil && !activeSet.IsFinished)

	switch data {
	case "game:start":
		if phase != storage.GamePhasePlanned {
			return
		}
		gState, setState, err := game2.StartPlannedGame(toGameState(game))
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
		broadcastText = fmt.Sprintf("Game started: 🏠 %s vs ✈️ %s", home.Name, guest.Name)

	case "game:set:start_next":
		if phase != storage.GamePhaseBetweenSets {
			return
		}
		gState, setState, err := game2.StartNextSet(toGameState(game))
		if err != nil {
			return
		}
		applyGameState(game, gState)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		nextSet := &storage.GameSet{
			GameID:     game.ID,
			SetNumber:  setState.SetNumber,
			HomeScore:  setState.HomeScore,
			GuestScore: setState.GuestScore,
			IsFinished: false,
		}
		if err := r.games.CreateSet(nextSet); err != nil {
			return
		}
		activeSet = nextSet
		broadcastText = fmt.Sprintf(
			"Set %d started\n🏠 %s vs ✈️ %s\n<i>Game score: %d-%d</i>",
			nextSet.SetNumber,
			home.Name,
			guest.Name,
			game.HomeSetsWon,
			game.GuestSetsWon,
		)

	case "game:home:+1", "game:home:-1", "game:guest:+1", "game:guest:-1":
		if phase != storage.GamePhaseSetInProgress || activeSet == nil {
			return
		}
		score := game2.SetScore{
			HomeScore:          activeSet.HomeScore,
			GuestScore:         activeSet.GuestScore,
			SetNumber:          activeSet.SetNumber,
			SideSwitchedInSet5: game.SideSwitchedInSet5,
		}
		var result game2.ScoreResult
		switch data {
		case "game:home:+1":
			result = game2.IncrementHome(score)
		case "game:home:-1":
			result = game2.DecrementHome(score)
		case "game:guest:+1":
			result = game2.IncrementGuest(score)
		case "game:guest:-1":
			result = game2.DecrementGuest(score)
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
		if phase != storage.GamePhaseSetInProgress || activeSet == nil {
			return
		}
		setState := game2.SetState{
			SetScore: game2.SetScore{
				HomeScore:          activeSet.HomeScore,
				GuestScore:         activeSet.GuestScore,
				SetNumber:          activeSet.SetNumber,
				SideSwitchedInSet5: game.SideSwitchedInSet5,
			},
			IsFinished: activeSet.IsFinished,
		}
		res, err := game2.ConfirmSetFinished(toGameState(game), setState)
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
		activeSet = nil

	case "game:finish":
		if phase != storage.GamePhaseBetweenSets {
			return
		}
		res, err := game2.ConfirmGameFinished(toGameState(game))
		if err != nil {
			return
		}
		applyGameState(game, res.Game)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		broadcastText = fmt.Sprintf(
			"Game finished\n🏠 %s <b>%d-%d</b> ✈️ %s",
			home.Name,
			game.HomeSetsWon,
			game.GuestSetsWon,
			guest.Name,
		)

	case "game:reverse":
		res := game2.ReverseOverlaySides(toGameState(game))
		applyGameState(game, res.Game)
		if err := r.games.SaveGame(game); err != nil {
			return
		}
		broadcastText = fmt.Sprintf("Overlay sides reversed for %s vs %s", home.Name, guest.Name)

	default:
		return
	}
	if r.renderer != nil {
		job, err := r.buildOverlayJob(game, home, guest, activeSet)
		if err != nil {
			r.logger.ErrorContext(ctx, "handleGameCallback: build overlay job failed", "game_id", game.ID, "err", err)
		} else {
			overlayJob = job
		}
	}

	view := buildGameControlMessage(game, home.Name, guest.Name, activeSet)
	_, editErr := r.client.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   game.ControlMessageID,
		Text:        view.Text,
		ReplyMarkup: view.Keyboard,
	})
	if editErr != nil {
		r.logger.ErrorContext(ctx, "handleGameCallback: edit control message failed",
			"game_id", game.ID, "message_id", game.ControlMessageID, "err", editErr)
	}

	if overlayJob != nil {
		r.enqueueOverlayRender(ctx, *overlayJob)
	}

	if strings.TrimSpace(broadcastText) != "" {
		r.BroadcastExcept(ctx, broadcastText, game.CurrentAdminUserID)
	}
}

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
		finishable = game2.IsSetFinishable(game2.SetScore{
			HomeScore:          activeSet.HomeScore,
			GuestScore:         activeSet.GuestScore,
			SetNumber:          activeSet.SetNumber,
			SideSwitchedInSet5: game.SideSwitchedInSet5,
		})
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
		rows = append(rows,
			[]models.InlineKeyboardButton{
				{Text: fmt.Sprintf("%s -1", homeName), CallbackData: "game:home:-1"},
				{Text: fmt.Sprintf("%s +1", homeName), CallbackData: "game:home:+1"},
			},
			[]models.InlineKeyboardButton{
				{Text: fmt.Sprintf("%s -1", guestName), CallbackData: "game:guest:-1"},
				{Text: fmt.Sprintf("%s +1", guestName), CallbackData: "game:guest:+1"},
			},
		)
	}
	if phase == storage.GamePhasePlanned {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Start the game", CallbackData: "game:start"},
		})
	}
	if phase == storage.GamePhaseSetInProgress && finishable {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Finish the set", CallbackData: "game:set:finish"},
		})
	}
	if phase == storage.GamePhaseBetweenSets && !game2.IsGameFinishEligible(toGameState(game)) {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Start next set", CallbackData: "game:set:start_next"},
		})
	}
	if phase == storage.GamePhaseBetweenSets && game2.IsGameFinishEligible(toGameState(game)) {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Finish the game", CallbackData: "game:finish"},
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

func toGameState(g *storage.Game) game2.GameState {
	return game2.GameState{
		HomeTeamID:         g.HomeTeamID,
		GuestTeamID:        g.GuestTeamID,
		HomeSide:           g.HomeTeamSide,
		GuestSide:          g.GuestTeamSide,
		HomeSetsWon:        g.HomeSetsWon,
		GuestSetsWon:       g.GuestSetsWon,
		CurrentSetNumber:   g.CurrentSetNumber,
		Status:             string(g.Status),
		Phase:              string(g.Phase),
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
	dst.Phase = storage.GamePhase(src.Phase)
	dst.SideSwitchedInSet5 = src.SideSwitchedInSet5
}

type overlayJob struct {
	game      storage.Game
	home      storage.Team
	guest     storage.Team
	activeSet *storage.GameSet
	sets      []storage.GameSet
}

func (r *Router) buildOverlayJob(game *storage.Game, home, guest *storage.Team, activeSet *storage.GameSet) (*overlayJob, error) {
	sets, err := r.games.ListSetsByGameID(game.ID)
	if err != nil {
		return nil, err
	}

	job := &overlayJob{
		game:  *game,
		home:  *home,
		guest: *guest,
		sets:  append([]storage.GameSet(nil), sets...),
	}
	if activeSet != nil {
		setCopy := *activeSet
		job.activeSet = &setCopy
	}
	return job, nil
}

func (r *Router) ensureOverlayWorker() {
	r.overlayWorkerOnce.Do(func() {
		if r.overlayQueueSize <= 0 {
			r.overlayQueueSize = defaultOverlayQueueSize
		}
		r.overlayJobs = make(chan overlayJob, r.overlayQueueSize)
		go r.overlayLoop()
	})
}

func (r *Router) overlayLoop() {
	for job := range r.overlayJobs {
		if err := r.renderOverlayJob(job); err != nil {
			r.logger.Error("overlay render failed", "game_id", job.game.ID, "err", err)
		}
	}
}

func (r *Router) enqueueOverlayRender(ctx context.Context, job overlayJob) {
	if r.renderer == nil {
		return
	}
	r.ensureOverlayWorker()
	select {
	case r.overlayJobs <- job:
	default:
		r.logger.WarnContext(ctx, "Overlay: queue full, dropping render job", "game_id", job.game.ID)
	}
}

func (r *Router) renderOverlayJob(job overlayJob) error {
	if r.renderer == nil {
		return nil
	}
	homeTeam := overlay.TeamIdentity{
		Name:      job.home.Name,
		ShortName: job.home.ShortName,
		Hometown:  job.home.Hometown,
		LogoPath:  job.home.LogoPath,
	}
	guestTeam := overlay.TeamIdentity{
		Name:      job.guest.Name,
		ShortName: job.guest.ShortName,
		Hometown:  job.guest.Hometown,
		LogoPath:  job.guest.LogoPath,
	}
	setScores := overlay.BuildSetScoreHistory(job.sets)
	phase := job.game.EffectivePhase(job.activeSet != nil && !job.activeSet.IsFinished)

	if phase == storage.GamePhasePlanned {
		if err := r.renderer.RenderPlanned(overlay.PlannedViewModel{
			HomeTeamName:       job.home.Name,
			HomeTeamShortName:  job.home.ShortName,
			HomeTeamHometown:   job.home.Hometown,
			HomeTeamLogoPath:   job.home.LogoPath,
			GuestTeamName:      job.guest.Name,
			GuestTeamShortName: job.guest.ShortName,
			GuestTeamHometown:  job.guest.Hometown,
			GuestTeamLogoPath:  job.guest.LogoPath,
		}); err != nil {
			return err
		}
		return r.renderer.RenderIntermission(overlay.BuildIntermissionViewModel(
			homeTeam,
			guestTeam,
			job.game.HomeSetsWon,
			job.game.GuestSetsWon,
			setScores,
		))
	}
	if phase == storage.GamePhaseBetweenSets {
		vm := overlay.BuildIntermissionViewModel(
			homeTeam,
			guestTeam,
			job.game.HomeSetsWon,
			job.game.GuestSetsWon,
			setScores,
		)
		if err := r.renderer.RenderIntermissionMain(vm); err != nil {
			return err
		}
		return r.renderer.RenderIntermission(vm)
	}
	if phase == storage.GamePhaseFinished {
		if err := r.renderer.RenderFinished(overlay.BuildFinishedViewModel(
			homeTeam,
			guestTeam,
			job.game.HomeSetsWon,
			job.game.GuestSetsWon,
			setScores,
		)); err != nil {
			return err
		}
		return r.renderer.RenderIntermission(overlay.BuildIntermissionViewModel(
			homeTeam,
			guestTeam,
			job.game.HomeSetsWon,
			job.game.GuestSetsWon,
			setScores,
		))
	}

	homeScore := 0
	guestScore := 0
	if job.activeSet != nil {
		homeScore = job.activeSet.HomeScore
		guestScore = job.activeSet.GuestScore
	}
	if err := r.renderer.RenderLive(overlay.BuildLiveViewModel(
		homeTeam,
		guestTeam,
		job.game.HomeTeamSide,
		homeScore,
		guestScore,
		job.game.HomeSetsWon,
		job.game.GuestSetsWon,
		job.game.CurrentSetNumber,
	)); err != nil {
		return err
	}
	return r.renderer.RenderIntermission(overlay.BuildIntermissionViewModel(
		homeTeam,
		guestTeam,
		job.game.HomeSetsWon,
		job.game.GuestSetsWon,
		setScores,
	))
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
