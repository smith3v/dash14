package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/smith3v/dash14/pkg/storage"
)

func TestResetCancelsCurrentGameInEachPhase(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status storage.GameStatus
		sets   []storage.GameSet
	}{
		{"planned", storage.GameStatusPlanned, nil},
		{"during a set", storage.GameStatusInProgress, []storage.GameSet{
			{SetNumber: 1, HomeScore: 13, GuestScore: 18},
		}},
		{"between sets", storage.GameStatusInProgress, []storage.GameSet{
			{SetNumber: 1, HomeScore: 25, GuestScore: 20, IsFinished: true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			const adminID, chatID int64 = 30, 40
			store := openPlanTestStore(t)
			r, fb, _ := newPlanRouter(t, store)
			store.createAdminUser(t, adminID, "owner")
			match := createCurrentPlannedGame(t, store, adminID)
			match.Status = tc.status
			if err := store.games.SaveGame(match); err != nil {
				t.Fatal(err)
			}
			for _, set := range tc.sets {
				set.GameID = match.ID
				if err := store.games.CreateSet(&set); err != nil {
					t.Fatal(err)
				}
			}
			r.handleGame(ctx, nil, makeTextUpdate(adminID, chatID, "/game"))

			notifications := 0
			r.SetGameServices(store.games, func() {
				notifications++
				if notifications == 1 {
					current, err := store.games.GetCurrentGame()
					if err != nil || current != nil {
						t.Fatalf("overlay notification preceded cancellation commit: %+v, %v", current, err)
					}
				}
			})
			r.handleReset(ctx, nil, makeTextUpdate(adminID, chatID, "/reset"))

			messages := fb.SentMessages()
			if len(messages) != 2 || !strings.Contains(messages[1].Text, "Game canceled") ||
				!strings.Contains(messages[1].Text, "/plan") || notifications != 1 {
				t.Fatalf("messages = %+v, overlay notifications = %d", messages, notifications)
			}
			canceled, err := store.games.GetGameByID(match.ID)
			if err != nil || canceled.Status != storage.GameStatusCanceled ||
				canceled.CurrentAdminUserID != 0 || canceled.ControlMessageID != 0 || canceled.ControlChatID != 0 {
				t.Fatalf("canceled game = %+v, %v", canceled, err)
			}
			sets, err := store.games.ListSetsByGameID(match.ID)
			if err != nil || len(sets) != len(tc.sets) {
				t.Fatalf("canceled game's sets = %+v, %v", sets, err)
			}
			for i, want := range tc.sets {
				if got := sets[i]; got.HomeScore != want.HomeScore || got.GuestScore != want.GuestScore ||
					got.IsFinished != want.IsFinished {
					t.Fatalf("set after cancellation = %+v, want scores and status from %+v", got, want)
				}
			}

			r.handlePlan(ctx, nil, makeTextUpdate(adminID, chatID, "/plan"))
			r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, chatID, "Control Home"))
			r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, chatID, "Control Guest"))
			next, err := store.games.GetCurrentGame()
			if err != nil || next == nil || next.ID == match.ID || next.Status != storage.GameStatusPlanned {
				t.Fatalf("new game after /reset and /plan = %+v, %v", next, err)
			}
		})
	}
}

func TestResetRequiresMatchControl(t *testing.T) {
	const userID, chatID int64 = 30, 40
	for _, tc := range []struct {
		name  string
		admin bool
		owner int64
		reply string
	}{
		{"non-admin recorded as owner", false, userID, "not authorised"},
		{"different admin", true, 31, "/takeover"},
		{"unassigned match", true, 0, "Run /game"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openPlanTestStore(t)
			r, fb, _ := newPlanRouter(t, store)
			if tc.admin {
				store.createAdminUser(t, userID, "admin")
			} else if err := store.users.UpsertTelegramUser(userID, "subscriber", "Subscriber"); err != nil {
				t.Fatal(err)
			}
			match := createCurrentPlannedGame(t, store, tc.owner)
			r.SetGameServices(store.games, func() { t.Fatal("rejected reset signaled the overlay") })
			r.handleReset(context.Background(), nil, makeTextUpdate(userID, chatID, "/reset"))

			messages := fb.SentMessages()
			if len(messages) != 1 || !strings.Contains(messages[0].Text, tc.reply) {
				t.Fatalf("reset reply = %+v, want %q", messages, tc.reply)
			}
			current, err := store.games.GetCurrentGame()
			if err != nil || current == nil || current.ID != match.ID ||
				current.Status != storage.GameStatusPlanned || current.CurrentAdminUserID != tc.owner {
				t.Fatalf("game after rejected reset = %+v, %v", current, err)
			}
		})
	}
}

func TestResetWithoutCurrentGamePreservesFinishedResult(t *testing.T) {
	const userID, chatID int64 = 30, 40
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	store.createAdminUser(t, userID, "admin")
	match := createCurrentPlannedGame(t, store, userID)
	match.Status, match.HomeSetsWon, match.GuestSetsWon = storage.GameStatusFinished, 3, 1
	if err := store.games.SaveGame(match); err != nil {
		t.Fatal(err)
	}
	r.SetGameServices(store.games, func() { t.Fatal("reset without a current game signaled the overlay") })
	r.handleReset(context.Background(), nil, makeTextUpdate(userID, chatID, "/reset"))

	messages := fb.SentMessages()
	if len(messages) != 1 || messages[0].Text != "No planned or active game to cancel." {
		t.Fatalf("unexpected reset reply: %+v", messages)
	}
	got, err := store.games.GetGameByID(match.ID)
	if err != nil || got.Status != match.Status || got.HomeSetsWon != 3 || got.GuestSetsWon != 1 ||
		!got.UpdatedAt.Equal(match.UpdatedAt) {
		t.Fatalf("finished game changed: %+v, %v", got, err)
	}
}

func TestResetMakesOldControlStaleAcrossChats(t *testing.T) {
	ctx := context.Background()
	const adminID, subscriberID, oldChatID, nextChatID int64 = 30, 31, 40, 41
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	store.createAdminUser(t, adminID, "owner")
	if err := store.users.UpsertTelegramUser(subscriberID, "subscriber", "Subscriber"); err != nil {
		t.Fatal(err)
	}
	first := createCurrentPlannedGame(t, store, adminID)
	r.handleGame(ctx, nil, makeTextUpdate(adminID, oldChatID, "/game"))
	oldMessageID := fb.SentMessages()[0].MessageID
	oldCallback := makeCurrentGameCallbackUpdate(t, store.games, adminID, oldChatID, oldMessageID, "old", startMatch)
	r.handleReset(ctx, nil, makeTextUpdate(adminID, oldChatID, "/reset"))

	r.handlePlan(ctx, nil, makeTextUpdate(adminID, nextChatID, "/plan"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, nextChatID, "Control Home"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, nextChatID, "Control Guest"))
	// Telegram message IDs are scoped to one chat, so the new chat can use
	// exactly the same message ID as the old one.
	fb.nextMessageID = oldMessageID
	r.handleGame(ctx, nil, makeTextUpdate(adminID, nextChatID, "/game"))
	r.handleGameCallback(ctx, nil, oldCallback)

	next, err := store.games.GetCurrentGame()
	if err != nil || next == nil || next.ID == first.ID || next.Status != storage.GameStatusPlanned ||
		next.ControlChatID != nextChatID || next.ControlMessageID != oldMessageID {
		t.Fatalf("new game after old callback = %+v, %v", next, err)
	}
	beforeBroadcast := len(fb.SentMessages())
	freshCallback := makeCurrentGameCallbackUpdate(t, store.games, adminID, nextChatID, oldMessageID, "new", startMatch)
	r.handleGameCallback(ctx, nil, freshCallback)
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, beforeBroadcast+1)
	next, err = store.games.GetCurrentGame()
	if err != nil || next == nil || next.Status != storage.GameStatusInProgress {
		t.Fatalf("fresh control did not start new game: %+v, %v", next, err)
	}
}

type gameStoreHooks struct {
	*storage.GameRepository
	beforeTransaction    func()
	afterCurrentGameRead func()
}

func (s *gameStoreHooks) WithinTx(fn func(*storage.GameRepository) error) error {
	if hook := s.beforeTransaction; hook != nil {
		s.beforeTransaction = nil
		hook()
	}
	return s.GameRepository.WithinTx(fn)
}

func (s *gameStoreHooks) GetCurrentGame() (*storage.Game, error) {
	match, err := s.GameRepository.GetCurrentGame()
	if hook := s.afterCurrentGameRead; hook != nil {
		s.afterCurrentGameRead = nil
		hook()
	}
	return match, err
}

func TestResetRevokesInFlightGameControls(t *testing.T) {
	for _, action := range []string{"/game", "/takeover", "game:reverse"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			const adminID, chatID int64 = 30, 40
			store := openPlanTestStore(t)
			r, fb, _ := newPlanRouter(t, store)
			store.createAdminUser(t, adminID, "owner")
			match := createCurrentPlannedGame(t, store, adminID)
			r.handleGame(ctx, nil, makeTextUpdate(adminID, chatID, "/game"))
			messageID := fb.SentMessages()[0].MessageID

			games := &gameStoreHooks{GameRepository: store.games}
			r.games = games
			cancel := func() { r.handleReset(ctx, nil, makeTextUpdate(adminID, chatID, "/reset")) }
			switch action {
			case "/game":
				games.beforeTransaction = cancel
				r.handleGame(ctx, nil, makeTextUpdate(adminID, chatID, action))
			case "/takeover":
				games.beforeTransaction = cancel
				r.handleTakeover(ctx, nil, makeTextUpdate(adminID, chatID, action))
			default:
				callback := makeCurrentGameCallbackUpdate(t, store.games, adminID, chatID, messageID, "old", action)
				games.afterCurrentGameRead = cancel
				r.handleGameCallback(ctx, nil, callback)
			}

			got, err := store.games.GetGameByID(match.ID)
			if err != nil || got.Status != storage.GameStatusCanceled || got.HomeTeamSide != "left" ||
				got.CurrentAdminUserID != 0 || got.ControlMessageID != 0 || len(fb.EditedMessages()) != 0 {
				t.Fatalf("in-flight action changed canceled game: %+v, %v", got, err)
			}
		})
	}
}

func TestResetRejectsOldPlanTeamSelection(t *testing.T) {
	ctx := context.Background()
	const adminID, chatID int64 = 30, 40
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	store.createAdminUser(t, adminID, "owner")
	match := createCurrentPlannedGame(t, store, adminID)
	insertTeam(t, store.teams, "other-guest", "Other Guest", "OG")

	chooseGuest := func() sentMessage {
		t.Helper()
		r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, chatID, "Control Home"))
		r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, chatID, "Guest"))
		messages := fb.SentMessages()
		return messages[len(messages)-1]
	}
	confirmPlannedGameReplacement(t, r, adminID, chatID)
	oldKeyboard := chooseGuest()
	oldChoices := keyboardCallbackData(t, oldKeyboard.ReplyMarkup)
	r.handleReset(ctx, nil, makeTextUpdate(adminID, chatID, "/reset"))

	r.handlePlan(ctx, nil, makeTextUpdate(adminID, chatID, "/plan"))
	newKeyboard := chooseGuest()
	newChoices := keyboardCallbackData(t, newKeyboard.ReplyMarkup)
	r.handlePlanCallback(ctx, nil, makeCallbackUpdateForMessage(adminID, chatID, oldKeyboard.MessageID, "old", oldChoices[0]))
	if current, err := store.games.GetCurrentGame(); err != nil || current != nil {
		t.Fatalf("stale team choice created a match: %+v, %v", current, err)
	}

	r.handlePlanCallback(ctx, nil, makeCallbackUpdateForMessage(adminID, chatID, newKeyboard.MessageID, "new", newChoices[0]))
	next, err := store.games.GetCurrentGame()
	if err != nil || next == nil || next.ID == match.ID || next.Status != storage.GameStatusPlanned {
		t.Fatalf("fresh team choice did not create the next planned match: %+v, %v", next, err)
	}
}

func TestResetDoesNotLetDuplicatePlanCallbackCreateAnotherGame(t *testing.T) {
	ctx := context.Background()
	const adminID, chatID int64 = 30, 40
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	store.createAdminUser(t, adminID, "owner")
	insertTeam(t, store.teams, "home", "Home Club", "HC")
	insertTeam(t, store.teams, "guest-one", "Guest One", "G1")
	insertTeam(t, store.teams, "guest-two", "Guest Two", "G2")

	r.handlePlan(ctx, nil, makeTextUpdate(adminID, chatID, "/plan"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, chatID, "Home Club"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(adminID, chatID, "Guest"))
	messages := fb.SentMessages()
	prompt := messages[len(messages)-1]
	choice := keyboardCallbackData(t, prompt.ReplyMarkup)[0]
	r.games = &gameStoreHooks{GameRepository: store.games, beforeTransaction: func() {
		r.handlePlanCallback(ctx, nil, makeCallbackUpdateForMessage(adminID, chatID, prompt.MessageID, "retry", choice))
		r.handleReset(ctx, nil, makeTextUpdate(adminID, chatID, "/reset"))
	}}
	r.handlePlanCallback(ctx, nil, makeCallbackUpdateForMessage(adminID, chatID, prompt.MessageID, "first", choice))

	current, err := store.games.GetCurrentGame()
	if err != nil || current == nil || current.Status != storage.GameStatusPlanned {
		t.Fatalf("the first callback should create the planned game: %+v, %v", current, err)
	}
	var count int64
	if err := store.db.Model(&storage.Game{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate callback and reset produced %d games: %v", count, err)
	}
}

func TestResetInvalidatesAnotherOperatorsUnfinishedPlan(t *testing.T) {
	ctx := context.Background()
	const ownerID, plannerID int64 = 30, 31
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	store.createAdminUser(t, ownerID, "owner")
	store.createAdminUser(t, plannerID, "planner")
	insertTeam(t, store.teams, "home", "Home Club", "HC")
	insertTeam(t, store.teams, "guest-one", "Guest One", "G1")
	insertTeam(t, store.teams, "guest-two", "Guest Two", "G2")

	r.handlePlan(ctx, nil, makeTextUpdate(plannerID, plannerID, "/plan"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(plannerID, plannerID, "Home Club"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(plannerID, plannerID, "Guest"))
	messages := fb.SentMessages()
	prompt := messages[len(messages)-1]
	choice := keyboardCallbackData(t, prompt.ReplyMarkup)[0]

	r.handlePlan(ctx, nil, makeTextUpdate(ownerID, ownerID, "/plan"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(ownerID, ownerID, "Home Club"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(ownerID, ownerID, "Guest One"))
	r.handleReset(ctx, nil, makeTextUpdate(ownerID, ownerID, "/reset"))
	r.handlePlanCallback(ctx, nil, makeCallbackUpdateForMessage(plannerID, plannerID, prompt.MessageID, "old", choice))

	if current, err := store.games.GetCurrentGame(); err != nil || current != nil {
		t.Fatalf("an older wizard created a match after another operator reset: %+v, %v", current, err)
	}
	messages = fb.SentMessages()
	if got := messages[len(messages)-1].Text; !strings.Contains(got, "Please run /plan again") {
		t.Fatalf("expected restart guidance for the old wizard, got %q", got)
	}
}
