package telegram

import (
	"context"
	"strings"
	"testing"
)

func TestUnknownCommandNonAdminGetsSubscriberHelp(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7401
	const chatID int64 = 8401

	if err := store.users.UpsertTelegramUser(userID, "viewer"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	createCurrentPlannedGame(t, store, 0)

	r.handlePlanText(ctx, nil, makeTextUpdate(userID, chatID, "/unknown"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected help message for unknown command")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "The bot will show the score updates during the game.") {
		t.Fatalf("expected subscriber help text, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "The next game is planned between Control Home and Control Guest.") {
		t.Fatalf("expected planned game details, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "/stop") || !strings.Contains(last.Text, "/start") {
		t.Fatalf("expected subscription commands in help message, got %q", last.Text)
	}
}

func TestUnknownCommandAdminGetsAdminHelpWithPlannedGame(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7402
	const chatID int64 = 8402

	store.createAdminUser(t, userID, "admin-help")
	createCurrentPlannedGame(t, store, userID)

	r.handlePlanText(ctx, nil, makeTextUpdate(userID, chatID, "/mystery"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected admin help message for unknown command")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "You are an admin.") {
		t.Fatalf("expected admin intro, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "The next game is planned between Control Home and Control Guest.") {
		t.Fatalf("expected planned game details, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "/plan to plan the next game") {
		t.Fatalf("expected /plan help entry, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "/game to manage the game status and the score") {
		t.Fatalf("expected /game help entry, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "/takeover to takeover the current game") {
		t.Fatalf("expected /takeover help entry, got %q", last.Text)
	}
}

func TestUnknownCommandAdminWithoutGameGetsNoPlannedGameMessage(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7403
	const chatID int64 = 8403

	store.createAdminUser(t, userID, "admin-no-game")

	r.handlePlanText(ctx, nil, makeTextUpdate(userID, chatID, "/what"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected admin help message for unknown command")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "There is no game planned yet.") {
		t.Fatalf("expected no-game message, got %q", last.Text)
	}
}

func TestPlainTextWithoutPlanStateIsIgnored(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7404
	const chatID int64 = 8404

	r.handlePlanText(ctx, nil, makePlainTextUpdate(userID, chatID, "hello there"))

	if len(fb.SentMessages()) != 0 {
		t.Fatalf("expected plain text without plan state to be ignored, got %d messages", len(fb.SentMessages()))
	}
}
