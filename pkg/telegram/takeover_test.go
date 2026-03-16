package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/smith3v/dash14/pkg/storage"
)

func TestTakeoverSuccess(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const ownerID int64 = 7401
	const newAdminID int64 = 7402
	const chatID int64 = 8401
	store.createAdminUser(t, ownerID, "owner")
	store.createAdminUser(t, newAdminID, "newowner")
	game := createCurrentPlannedGame(t, store, ownerID)
	game.ControlMessageID = 123
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	r.handleTakeover(ctx, nil, makeTextUpdate(newAdminID, chatID, "/takeover"))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.CurrentAdminUserID != newAdminID {
		t.Fatalf("expected new admin %d, got %d", newAdminID, got.CurrentAdminUserID)
	}
	if got.ControlMessageID != 0 {
		t.Fatalf("expected control thread invalidation, got control_message_id=%d", got.ControlMessageID)
	}

	msgs := fb.SentMessages()
	if len(msgs) < 2 {
		t.Fatalf("expected notifications for both admins, got %d messages", len(msgs))
	}
	var ownerNotified bool
	var newAdminNotified bool
	for _, m := range msgs {
		if m.ChatID == ownerID && m.Text == "Game control was transferred to @newowner." {
			ownerNotified = true
		}
		if m.ChatID == chatID && strings.Contains(m.Text, "Run /game") {
			newAdminNotified = true
		}
	}
	if !ownerNotified {
		t.Fatal("expected previous admin notification")
	}
	if !newAdminNotified {
		t.Fatal("expected new admin notification")
	}
}

func TestTakeoverFallsBackToDisplayName(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const ownerID int64 = 7411
	const newAdminID int64 = 7412
	const chatID int64 = 8411
	store.createAdminUser(t, ownerID, "owner")
	if err := store.users.UpsertTelegramUser(newAdminID, "", "Alex Admin"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	if err := store.db.Model(&storage.User{}).
		Where("telegram_user_id = ?", newAdminID).
		Update("is_admin", true).Error; err != nil {
		t.Fatalf("set is_admin: %v", err)
	}
	game := createCurrentPlannedGame(t, store, ownerID)
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	r.handleTakeover(ctx, nil, makeTextUpdate(newAdminID, chatID, "/takeover"))

	for _, m := range fb.SentMessages() {
		if m.ChatID == ownerID && m.Text == "Game control was transferred to Alex Admin." {
			return
		}
	}
	t.Fatal("expected previous admin notification with display-name fallback")
}

func TestTakeoverRejectedWhenNoCurrentGame(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const adminID int64 = 7403
	const chatID int64 = 8403
	store.createAdminUser(t, adminID, "admin")

	r.handleTakeover(ctx, nil, makeTextUpdate(adminID, chatID, "/takeover"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected rejection message")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "No planned or active game is available") {
		t.Fatalf("unexpected response: %q", last.Text)
	}
}

func TestTakeoverRejectedForNonAdmin(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7404
	const chatID int64 = 8404
	if err := store.users.UpsertTelegramUser(userID, "viewer", "Viewer"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}

	r.handleTakeover(ctx, nil, makeTextUpdate(userID, chatID, "/takeover"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected rejection message")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "not authorised") {
		t.Fatalf("expected auth rejection message, got %q", last.Text)
	}
}
