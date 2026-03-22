package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	if err := store.games.SetCurrentGameID(game.ID); err != nil {
		t.Fatalf("SetCurrentGameID: %v", err)
	}
	return game
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
	if last.ReplyMarkup == nil {
		t.Fatal("expected inline keyboard on control message")
	}

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
	r, _, _ := newPlanRouter(t, store)
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
}

func TestGameControlSet3FinishCreatesSet4(t *testing.T) {
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
	if g.CurrentSetNumber != 4 {
		t.Fatalf("expected game to continue to set 4, got set %d", g.CurrentSetNumber)
	}
	nextSet, err := store.games.GetActiveSet(game.ID)
	if err != nil {
		t.Fatalf("GetActiveSet after set finish: %v", err)
	}
	if nextSet.SetNumber != 4 {
		t.Fatalf("expected next active set number 4, got %d", nextSet.SetNumber)
	}
	if len(renderer.live) == 0 {
		t.Fatal("expected live overlay rendering after set finish")
	}
	if len(renderer.intermission) == 0 {
		t.Fatal("expected intermission overlay rendering after set finish")
	}
	last := renderer.intermission[len(renderer.intermission)-1]
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
	if len(renderer.live) == 0 {
		t.Fatal("expected live overlay rendering after set finish")
	}
}

func TestGameControlBroadcastTextGeneration(t *testing.T) {
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

	r.handleGame(ctx, nil, makeGameMessageUpdate(adminID, chatID, "/game"))
	current, _ := store.games.GetGameByID(game.ID)
	ctrlID := current.ControlMessageID
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-start3", "game:start"))
	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(adminID, chatID, ctrlID, "cb-score", "game:home:+1"))

	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 3)
	msgs := fb.SentMessages()
	foundBroadcast := false
	for _, m := range msgs {
		if m.ChatID == subID && strings.Contains(m.Text, "Current set: 1") && strings.Contains(m.Text, "Game score") {
			foundBroadcast = true
			break
		}
	}
	if !foundBroadcast {
		t.Fatal("expected broadcast score update to subscribed user")
	}

	for _, m := range msgs {
		if m.ChatID == adminID {
			t.Fatalf("expected active admin %d to be excluded from broadcasts", adminID)
		}
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

	r.handleGameCallback(ctx, nil, makeGameCallbackUpdate(userID, chatID, ctrlID, "cb-game-finish", "game:game:finish"))

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
