package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

func makeGameMessageUpdate(userID, chatID int64, text string) *models.Update {
	return makeTextUpdate(userID, chatID, text)
}

func makeGameCallbackUpdate(userID, chatID int64, messageID int, callbackID, data string) *models.Update {
	return &models.Update{
		ID: 1,
		CallbackQuery: &models.CallbackQuery{
			ID:   callbackID,
			From: models.User{ID: userID, FirstName: "TestUser"},
			Message: models.MaybeInaccessibleMessage{
				Type: models.MaybeInaccessibleMessageTypeMessage,
				Message: &models.Message{
					ID:   messageID,
					Chat: models.Chat{ID: chatID},
				},
			},
			Data: data,
		},
	}
}

func createCurrentPlannedGame(t *testing.T, store *planTestStore, adminID int64) *storage.Game {
	t.Helper()
	home := insertTeam(t, store.teams, "ctrl-home", "Control Home", "CH")
	guest := insertTeam(t, store.teams, "ctrl-guest", "Control Guest", "CG")
	game := &storage.Game{
		HomeTeamID:         home.ID,
		GuestTeamID:        guest.ID,
		HomeTeamSide:       "left",
		GuestTeamSide:      "right",
		Status:             storage.GameStatusPlanned,
		CurrentSetNumber:   1,
		CurrentAdminUserID: adminID,
	}
	if err := store.games.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	return game
}

func keyboardCallbackData(t *testing.T, markup any) []string {
	t.Helper()
	kb, ok := markup.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard markup, got %T", markup)
	}
	var data []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			action, _ := decodeLifecycleCallback(btn.CallbackData)
			data = append(data, action)
		}
	}
	return data
}

func requireHasCallback(t *testing.T, callbacks []string, want string) {
	t.Helper()
	for _, cb := range callbacks {
		if cb == want {
			return
		}
	}
	t.Fatalf("expected callback %q in %v", want, callbacks)
}

func requireNoCallbackPrefix(t *testing.T, callbacks []string, prefix string) {
	t.Helper()
	for _, cb := range callbacks {
		if strings.HasPrefix(cb, prefix) {
			t.Fatalf("unexpected callback with prefix %q in %v", prefix, callbacks)
		}
	}
}

func requireNoCallback(t *testing.T, callbacks []string, want string) {
	t.Helper()
	for _, cb := range callbacks {
		if cb == want {
			t.Fatalf("unexpected callback %q in %v", want, callbacks)
		}
	}
}

func TestGameControlCurrentAdminAccess(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7201
	const chatID int64 = 8201
	store.createAdminUser(t, userID, "owner")
	game := createCurrentPlannedGame(t, store, userID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected control message")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "Game controls") {
		t.Fatalf("expected control message text, got %q", last.Text)
	}
	if last.ParseMode != models.ParseModeHTML || !strings.Contains(last.Text, "\n<b>Set 1 · 0–0</b>\n") {
		t.Fatalf("expected a bold set score in the control message, got %q (%q)", last.Text, last.ParseMode)
	}
	if last.ReplyMarkup == nil {
		t.Fatal("expected inline keyboard on control message")
	}
	callbacks := keyboardCallbackData(t, last.ReplyMarkup)
	requireNoCallbackPrefix(t, callbacks, "game:home:")
	requireNoCallbackPrefix(t, callbacks, "game:guest:")
	requireHasCallback(t, callbacks, "game:start")
	requireHasCallback(t, callbacks, "game:reverse")
	requireNoCallback(t, callbacks, "game:set:start_next")
	requireNoCallback(t, callbacks, "game:finish")

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.ControlMessageID == 0 {
		t.Fatal("expected control message id to be persisted")
	}
	if got.CurrentAdminUserID != userID {
		t.Fatalf("expected current admin %d, got %d", userID, got.CurrentAdminUserID)
	}
}

func TestGameControlSetInProgressShowsScoreButtons(t *testing.T) {
	game := &storage.Game{
		Status:           storage.GameStatusInProgress,
		CurrentSetNumber: 2,
		HomeSetsWon:      1,
		GuestSetsWon:     0,
	}
	activeSet := &storage.GameSet{
		SetNumber:  2,
		HomeScore:  20,
		GuestScore: 18,
		IsFinished: false,
	}

	view := buildGameControlMessage(game, "Home", "Guest", activeSet)
	callbacks := keyboardCallbackData(t, view.Keyboard)

	requireHasCallback(t, callbacks, "game:home:-1")
	requireHasCallback(t, callbacks, "game:home:+1")
	requireHasCallback(t, callbacks, "game:guest:-1")
	requireHasCallback(t, callbacks, "game:guest:+1")
	requireHasCallback(t, callbacks, "game:reverse")
	requireNoCallback(t, callbacks, "game:start")
	requireNoCallback(t, callbacks, "game:set:start_next")
	requireNoCallback(t, callbacks, "game:finish")
}

func TestGameControlPlannedShowsStartGame(t *testing.T) {
	game := &storage.Game{
		Status:           storage.GameStatusPlanned,
		CurrentSetNumber: 1,
	}

	view := buildGameControlMessage(game, "Home", "Guest", nil)
	callbacks := keyboardCallbackData(t, view.Keyboard)

	requireHasCallback(t, callbacks, "game:start")
	requireHasCallback(t, callbacks, "game:reverse")
	requireNoCallback(t, callbacks, "game:set:start_next")
	requireNoCallback(t, callbacks, "game:finish")
	requireNoCallbackPrefix(t, callbacks, "game:home:")
	requireNoCallbackPrefix(t, callbacks, "game:guest:")
}

func TestGameControlBetweenSetsShowsStartNextSet(t *testing.T) {
	game := &storage.Game{
		Status:           storage.GameStatusInProgress,
		CurrentSetNumber: 2,
		HomeSetsWon:      1,
		GuestSetsWon:     0,
	}

	view := buildGameControlMessage(game, "Home", "Guest", nil)
	callbacks := keyboardCallbackData(t, view.Keyboard)

	requireHasCallback(t, callbacks, "game:set:start_next")
	requireHasCallback(t, callbacks, "game:reverse")
	requireNoCallbackPrefix(t, callbacks, "game:home:")
	requireNoCallbackPrefix(t, callbacks, "game:guest:")
	requireNoCallback(t, callbacks, "game:start")
	requireNoCallback(t, callbacks, "game:finish")
}

func TestGameControlBetweenSetsShowsFinishGameWhenEligible(t *testing.T) {
	game := &storage.Game{
		Status:           storage.GameStatusInProgress,
		CurrentSetNumber: 4,
		HomeSetsWon:      3,
		GuestSetsWon:     1,
	}

	view := buildGameControlMessage(game, "Home", "Guest", nil)
	callbacks := keyboardCallbackData(t, view.Keyboard)

	requireHasCallback(t, callbacks, "game:finish")
	requireHasCallback(t, callbacks, "game:reverse")
	requireNoCallback(t, callbacks, "game:set:start_next")
	requireNoCallbackPrefix(t, callbacks, "game:home:")
	requireNoCallbackPrefix(t, callbacks, "game:guest:")
}

func TestGameControlReplacesOldControlThread(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7202
	const chatID int64 = 8202
	store.createAdminUser(t, userID, "owner2")
	game := createCurrentPlannedGame(t, store, userID)
	game.ControlMessageID = 77
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.ControlMessageID == 77 {
		t.Fatal("expected old control message id to be replaced")
	}
}

func TestGameControlRejectsCallbackWhenControlMessageWasInvalidated(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, renderer := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7208
	const chatID int64 = 8208
	store.createAdminUser(t, userID, "invalidated-control")
	game := createCurrentPlannedGame(t, store, userID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after /game: %v", err)
	}
	oldControlMessageID := current.ControlMessageID
	if oldControlMessageID == 0 {
		t.Fatal("expected /game to persist a control message ID")
	}

	newHome := insertTeam(t, store.teams, "invalidated-home", "Invalidated Home", "IH")
	newGuest := insertTeam(t, store.teams, "invalidated-guest", "Invalidated Guest", "IG")
	if err := store.games.ReplacePlannedGame(
		current.ID,
		current.HomeTeamID,
		current.GuestTeamID,
		newHome.ID,
		newGuest.ID,
		userID,
	); err != nil {
		t.Fatalf("ReplacePlannedGame: %v", err)
	}

	r.handleGameCallback(
		ctx,
		nil,
		makeGameCallbackUpdate(userID, chatID, oldControlMessageID, "old-start", "game:start"),
	)

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after stale callback: %v", err)
	}
	if got.Status != storage.GameStatusPlanned {
		t.Fatalf("invalidated callback changed status to %q", got.Status)
	}
	if _, err := store.games.GetActiveSet(game.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetActiveSet error = %v, want gorm.ErrRecordNotFound", err)
	}
	if renderer.liveCount() != 0 {
		t.Fatalf("invalidated callback rendered live overlay %d times", renderer.liveCount())
	}
}

func TestGameControlRejectsNonOwner(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const ownerID int64 = 7203
	const otherAdminID int64 = 7204
	const chatID int64 = 8203
	store.createAdminUser(t, ownerID, "captain")
	store.createAdminUser(t, otherAdminID, "challenger")
	game := createCurrentPlannedGame(t, store, ownerID)
	game.ControlMessageID = 55
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	r.handleGame(ctx, nil, makeGameMessageUpdate(otherAdminID, chatID, "/game"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected non-owner response message")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "@captain currently manages the game") {
		t.Fatalf("unexpected non-owner message: %q", last.Text)
	}
	if !strings.Contains(last.Text, "/takeover") {
		t.Fatalf("expected takeover guidance, got %q", last.Text)
	}

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.CurrentAdminUserID != ownerID {
		t.Fatalf("expected owner to remain %d, got %d", ownerID, got.CurrentAdminUserID)
	}
	if got.ControlMessageID != 55 {
		t.Fatalf("expected control message id to remain 55, got %d", got.ControlMessageID)
	}
}

func TestGameControlScoreButtonRouting(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7301
	const chatID int64 = 8301
	store.createAdminUser(t, userID, "owner3")
	game := createCurrentPlannedGame(t, store, userID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	ctrlID := current.ControlMessageID

	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-start", "game:start"))
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-home", "game:home:+1"))
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-guest", "game:guest:+1"))

	active, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	if active.HomeScore != 1 || active.GuestScore != 1 {
		t.Fatalf("unexpected scores after callbacks: home=%d guest=%d", active.HomeScore, active.GuestScore)
	}

	edits := fb.EditedMessages()
	if len(edits) == 0 {
		t.Fatal("expected the control message to be edited")
	}
	last := edits[len(edits)-1]
	if last.ChatID != chatID || last.MessageID != ctrlID || last.ParseMode != models.ParseModeHTML ||
		!strings.Contains(last.Text, "\n<b>Set 1 · 1–1</b>\n") {
		t.Fatalf("expected a bold set score on the edited control message, got %+v", last)
	}
}

func TestGameControlSet3FinishTransitionsToBetweenSets(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, renderer := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7302
	const chatID int64 = 8302
	store.createAdminUser(t, userID, "owner4")
	game := createCurrentPlannedGame(t, store, userID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))
	current, _ := store.games.GetGameByID(game.ID)
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-start2", "game:start"))

	g, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	g.HomeSetsWon = 2
	g.GuestSetsWon = 0
	g.CurrentSetNumber = 3
	if err := store.games.SaveGame(g); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	set, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	set.SetNumber = 3
	set.HomeScore = 25
	set.GuestScore = 10
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet: %v", err)
	}

	beforeIntermission := renderer.intermissionCount()
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-set-finish", "game:set:finish"))

	g, err = store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after set finish: %v", err)
	}
	if g.Status != storage.GameStatusInProgress {
		t.Fatalf("expected game to remain in_progress, got %q", g.Status)
	}
	if g.HomeSetsWon != 3 {
		t.Fatalf("expected home sets won to become 3, got %d", g.HomeSetsWon)
	}
	if g.CurrentSetNumber != 3 {
		t.Fatalf("expected current set number to remain 3 until next set starts, got %d", g.CurrentSetNumber)
	}
	_, err = store.games.GetActiveSet(game.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected no active set after set finish, got err=%v", err)
	}
	waitForCondition(t, "intermission overlay render after set finish", func() bool {
		return renderer.intermissionCount() > beforeIntermission
	})
	if renderer.intermissionCount() == beforeIntermission {
		t.Fatal("expected intermission overlay rendering after set finish")
	}
	last := renderer.lastIntermission()
	if len(last.SetScores) != 1 {
		t.Fatalf("expected only finished sets in intermission, got %d entries", len(last.SetScores))
	}
	if last.SetScores[0].SetNumber != 3 || last.SetScores[0].HomeScore != 25 || last.SetScores[0].GuestScore != 10 {
		t.Fatalf("unexpected intermission set history: %+v", last.SetScores)
	}
}

func TestGameControlSet4FinishPromptsGameFinishWithoutAutoFinish(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, renderer := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7305
	const chatID int64 = 8305
	store.createAdminUser(t, userID, "owner6")
	game := createCurrentPlannedGame(t, store, userID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))
	current, _ := store.games.GetGameByID(game.ID)
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-start4", "game:start"))

	g, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	g.HomeSetsWon = 3
	g.GuestSetsWon = 0
	g.CurrentSetNumber = 4
	if err := store.games.SaveGame(g); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	set, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	set.SetNumber = 4
	set.HomeScore = 25
	set.GuestScore = 10
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet: %v", err)
	}

	beforeIntermission := renderer.intermissionCount()
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-set-finish4", "game:set:finish"))

	g, err = store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after set finish: %v", err)
	}
	if g.Status != storage.GameStatusInProgress {
		t.Fatalf("expected game to remain in_progress, got %q", g.Status)
	}
	if g.HomeSetsWon != 4 {
		t.Fatalf("expected home sets won to become 4, got %d", g.HomeSetsWon)
	}
	if g.CurrentSetNumber != 4 {
		t.Fatalf("expected current set number to stay on set 4 for finish prompt, got %d", g.CurrentSetNumber)
	}
	_, err = store.games.GetActiveSet(game.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected no active set after set 4 finish prompt, got err=%v", err)
	}
	waitForCondition(t, "intermission overlay render after set 4 finish", func() bool {
		return renderer.intermissionCount() > beforeIntermission
	})
}

func waitForScoreboardEdit(t *testing.T, fb *FakeBot, message sentMessage, text string) {
	t.Helper()
	waitForCondition(t, "scoreboard edit containing "+text, func() bool {
		for _, edit := range fb.EditedMessages() {
			if edit.ChatID == message.ChatID && edit.MessageID == message.MessageID && strings.Contains(edit.Text, text) {
				if edit.ParseMode != models.ParseModeHTML {
					t.Fatalf("scoreboard edit parse mode = %q, want HTML", edit.ParseMode)
				}
				return true
			}
		}
		return false
	})
}

func TestGameControlScoreboardEditsSameMessageForPointsAndCorrections(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const adminID int64 = 7303
	const subID int64 = 7304
	const chatID int64 = 8303
	store.createAdminUser(t, adminID, "owner5")
	if err := store.users.UpsertTelegramUser(subID, "subscriber", "Subscriber"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	game := createCurrentPlannedGame(t, store, adminID)
	home, err := store.teams.GetTeamByID(game.HomeTeamID)
	if err != nil {
		t.Fatal(err)
	}
	home.Name = "A&B <Home>"
	if err := store.teams.UpsertTeam(home); err != nil {
		t.Fatal(err)
	}

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start3", "game:start"))
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)
	messages := fb.SentMessages()
	if !strings.Contains(messages[0].Text, "Home: A&amp;B &lt;Home&gt;") {
		t.Fatalf("expected escaped team name in the control message, got %q", messages[0].Text)
	}
	scoreboard := messages[1]
	const matchup = "🏠 A&amp;B &lt;Home&gt; vs ✈️ Control Guest"
	if scoreboard.ChatID != subID || scoreboard.ParseMode != models.ParseModeHTML ||
		scoreboard.Text != matchup+"\n<b>Set 1 · 0–0</b>\n<i>Sets won: 0–0</i>" {
		t.Fatalf("unexpected initial scoreboard: %+v", scoreboard)
	}

	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-score", "game:home:+1"))
	waitForScoreboardEdit(t, fb, scoreboard, matchup+"\n<b>Set 1 · 1–0</b>")
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-score-correction", "game:home:-1"))
	waitForScoreboardEdit(t, fb, scoreboard, matchup+"\n<b>Set 1 · 0–0</b>")

	for _, edit := range fb.EditedMessages() {
		if edit.ChatID == adminID {
			t.Fatalf("current admin received scoreboard edit: %+v", edit)
		}
	}
	if got := len(fb.SentMessages()); got != 2 {
		t.Fatalf("sent %d messages, want one control and one scoreboard", got)
	}
}

func TestGameControlSetFinishEditsScoreboard(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const adminID int64 = 7307
	const subID int64 = 7308
	const chatID int64 = 8307
	store.createAdminUser(t, adminID, "owner-set-finish")
	if err := store.users.UpsertTelegramUser(subID, "subscriber2", "Subscriber Two"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	game := createCurrentPlannedGame(t, store, adminID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, _ := store.games.GetGameByID(game.ID)
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start-set-finish", "game:start"))
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)

	g, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	g.HomeSetsWon = 2
	g.GuestSetsWon = 0
	g.CurrentSetNumber = 3
	if err := store.games.SaveGame(g); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	set, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	set.SetNumber = 3
	set.HomeScore = 25
	set.GuestScore = 10
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet: %v", err)
	}

	scoreboard := fb.SentMessages()[1]
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-set-finish", "game:set:finish"))
	waitForScoreboardEdit(t, fb, scoreboard, "<b>Set 3 finished · 25–10</b>\n<i>Sets won: 3–0</i>")
	if got := len(fb.SentMessages()); got != 2 {
		t.Fatalf("sent %d messages, want one control and one scoreboard", got)
	}
}

func TestGameControlStartNextSetCreatesActiveSetAndEditsScoreboard(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)
	ctx := context.Background()

	const adminID int64 = 7309
	const subID int64 = 7314
	const chatID int64 = 8309
	store.createAdminUser(t, adminID, "owner-next-set")
	if err := store.users.UpsertTelegramUser(subID, "subscriber3", "Subscriber Three"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	game := createCurrentPlannedGame(t, store, adminID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, _ := store.games.GetGameByID(game.ID)
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start-next-flow", "game:start"))
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)

	g, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	g.HomeSetsWon = 2
	g.GuestSetsWon = 1
	g.CurrentSetNumber = 3
	if err := store.games.SaveGame(g); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	set, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	set.IsFinished = true
	set.SetNumber = 3
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet: %v", err)
	}

	scoreboard := fb.SentMessages()[1]
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start-next", "game:set:start_next"))

	g, err = store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after next set start: %v", err)
	}
	if g.CurrentSetNumber != 4 {
		t.Fatalf("expected current set number 4, got %d", g.CurrentSetNumber)
	}
	active, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet after next set start: %v", err)
	}
	if active.SetNumber != 4 || active.HomeScore != 0 || active.GuestScore != 0 || active.IsFinished {
		t.Fatalf("unexpected active set after start next set: %+v", active)
	}
	waitForCondition(t, "live overlay render after next set start", func() bool { return renderer.liveCount() > 0 })
	waitForScoreboardEdit(t, fb, scoreboard, "<b>Set 4 · 0–0</b>\n<i>Sets won: 2–1</i>")
	if got := len(fb.SentMessages()); got != 2 {
		t.Fatalf("sent %d messages, want one control and one scoreboard", got)
	}
}

func TestGameControlFinishEditsMessageWithoutControls(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7306
	const chatID int64 = 8306
	store.createAdminUser(t, userID, "owner7")
	game := createCurrentPlannedGame(t, store, userID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(userID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-start5", "game:start"))

	g, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after start: %v", err)
	}
	g.HomeSetsWon = 4
	g.GuestSetsWon = 0
	g.CurrentSetNumber = 4
	if err := store.games.SaveGame(g); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	set, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	set.IsFinished = true
	set.SetNumber = 4
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet: %v", err)
	}
	set.IsFinished = true
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet finished: %v", err)
	}

	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-game-finish", "game:finish"))

	g, err = store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after finish: %v", err)
	}
	if g.Status != storage.GameStatusFinished {
		t.Fatalf("expected finished status, got %q", g.Status)
	}

	edited := fb.EditedMessages()
	if len(edited) == 0 {
		t.Fatal("expected edited control message after game finish")
	}
	last := edited[len(edited)-1]
	if !strings.Contains(last.Text, "Game finished") {
		t.Fatalf("expected finish confirmation text, got %q", last.Text)
	}
	kb, ok := last.ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard markup on edited message, got %T", last.ReplyMarkup)
	}
	if len(kb.InlineKeyboard) != 0 {
		t.Fatalf("expected no controls after finish, got %d keyboard rows", len(kb.InlineKeyboard))
	}
}

func TestGameControlGameFinishEditsScoreboard(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const adminID int64 = 7315
	const subID int64 = 7316
	const chatID int64 = 8315
	store.createAdminUser(t, adminID, "owner-finish-broadcast")
	if err := store.users.UpsertTelegramUser(subID, "subscriber4", "Subscriber Four"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	game := createCurrentPlannedGame(t, store, adminID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start-finish-broadcast", "game:start"))
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)

	g, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after start: %v", err)
	}
	g.HomeSetsWon = 4
	g.GuestSetsWon = 0
	g.CurrentSetNumber = 4
	if err := store.games.SaveGame(g); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	set, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet: %v", err)
	}
	set.IsFinished = true
	set.SetNumber = 4
	if err := store.games.SaveSet(set); err != nil {
		t.Fatalf("SaveSet: %v", err)
	}

	scoreboard := fb.SentMessages()[1]
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-game-finish-summary", "game:finish"))
	waitForScoreboardEdit(t, fb, scoreboard, "🏠 Control Home vs ✈️ Control Guest\n<b>Final · 4–0</b>")
	if got := len(fb.SentMessages()); got != 2 {
		t.Fatalf("sent %d messages, want one control and one scoreboard", got)
	}
}

type saveFailGames struct {
	inner        *storage.GameRepository
	failWithinTx bool
}

func (g *saveFailGames) WithinTx(fn func(repo *storage.GameRepository) error) error {
	if g.failWithinTx {
		return errors.New("simulated transactional failure")
	}
	return g.inner.WithinTx(fn)
}
func (g *saveFailGames) CreateGame(game *storage.Game) error        { return g.inner.CreateGame(game) }
func (g *saveFailGames) CreateSet(set *storage.GameSet) error       { return g.inner.CreateSet(set) }
func (g *saveFailGames) GetCurrentGame() (*storage.Game, error)     { return g.inner.GetCurrentGame() }
func (g *saveFailGames) GetGameByID(id uint) (*storage.Game, error) { return g.inner.GetGameByID(id) }
func (g *saveFailGames) GetActiveSet(gameID uint) (*storage.GameSet, error) {
	return g.inner.GetActiveSet(gameID)
}
func (g *saveFailGames) SaveGame(game *storage.Game) error  { return g.inner.SaveGame(game) }
func (g *saveFailGames) SaveSet(set *storage.GameSet) error { return g.inner.SaveSet(set) }

// blockingSubscriberBot lets control messages through while holding subscriber
// broadcasts independently of overlay rendering. started records the number of
// control edits visible when the broadcast begins.
type blockingSubscriberBot struct {
	FakeBot
	subscriberID int64
	started      chan int
	release      chan struct{}
}

func (b *blockingSubscriberBot) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	if chatID, ok := params.ChatID.(int64); ok && chatID == b.subscriberID {
		b.started <- len(b.EditedMessages())
		<-b.release
	}
	return b.FakeBot.SendMessage(ctx, params)
}

func TestGameControlSignalsOverlayAndEditsControlBeforeBroadcast(t *testing.T) {
	const adminID int64 = 7310
	const subID int64 = 7311
	const chatID int64 = 8310

	store := openPlanTestStore(t)
	fb := &blockingSubscriberBot{
		subscriberID: subID,
		started:      make(chan int, 1),
		release:      make(chan struct{}),
	}
	overlayChanged := make(chan struct{}, 1)
	releaseBroadcast := sync.OnceFunc(func() { close(fb.release) })
	t.Cleanup(releaseBroadcast)
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), fb, store.users, store.teams)
	r.broadcastQueueSize = 1
	r.SetGameServices(store.games, func() { overlayChanged <- struct{}{} })
	ctx := context.Background()

	store.createAdminUser(t, adminID, "owner-order")
	if err := store.users.UpsertTelegramUser(subID, "subscriber", "Subscriber"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	game := createCurrentPlannedGame(t, store, adminID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	ctrlID := current.ControlMessageID

	callbackDone := make(chan struct{})
	go func() {
		r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start-order", "game:start"))
		close(callbackDone)
	}()
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("control callback did not return while async side effects were blocked")
	}

	select {
	case <-overlayChanged:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed overlay signal")
	}
	select {
	case edits := <-fb.started:
		if edits != 1 {
			t.Fatalf("expected control edit before broadcast started, got %d edits", edits)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async broadcast worker to start")
	}

	if len(fb.SentMessages()) != 1 {
		t.Fatalf("expected only initial control message before broadcast completed, got %d", len(fb.SentMessages()))
	}
	releaseBroadcast()
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)
}

func TestGameControlSaveFailureSkipsEditAndAsyncSideEffects(t *testing.T) {
	store := openPlanTestStore(t)
	fb := &FakeBot{}
	renderer := &fakeOverlayRenderer{}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), fb, store.users, store.teams)
	r.SetGameServices(store.games, recordOverlayChange(t, store, renderer))
	ctx := context.Background()

	const adminID int64 = 7312
	const subID int64 = 7313
	const chatID int64 = 8312
	store.createAdminUser(t, adminID, "owner-fail")
	if err := store.users.UpsertTelegramUser(subID, "subscriber", "Subscriber"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	game := createCurrentPlannedGame(t, store, adminID)

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start-save", "game:start"))
	waitForCondition(t, "initial overlay renders after start", func() bool {
		return renderer.liveCount() == 1 && renderer.intermissionCount() == 1
	})
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)

	beforeMsgs := len(fb.SentMessages())
	r.games = &saveFailGames{inner: store.games, failWithinTx: true}
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-score-save-fail", "game:home:+1"))

	if len(fb.EditedMessages()) != 1 {
		t.Fatalf("expected no extra edited control message after save failure, got %d edits", len(fb.EditedMessages()))
	}
	time.Sleep(100 * time.Millisecond)
	if renderer.liveCount() != 1 || renderer.intermissionCount() != 1 {
		t.Fatalf("expected no extra overlay renders after save failure, got live=%d intermission=%d", renderer.liveCount(), renderer.intermissionCount())
	}
	if len(fb.SentMessages()) != beforeMsgs+1 {
		t.Fatalf("expected exactly one retry-safe failure message and no broadcast after transactional failure, got %d sent messages", len(fb.SentMessages()))
	}
	last := fb.SentMessages()[len(fb.SentMessages())-1]
	if last.ChatID != chatID || !strings.Contains(last.Text, "state was not changed") {
		t.Fatalf("expected retry-safe admin failure message, got chat=%d text=%q", last.ChatID, last.Text)
	}
}
