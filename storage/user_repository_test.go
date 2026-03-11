package storage_test

import (
	"errors"
	"testing"

	"github.com/smith3v/dash14/storage"
	"gorm.io/gorm"
)

// TestUserInitialInsert verifies that UpsertTelegramUser creates a new user
// record on the first call and that the record can be retrieved by TelegramUserID.
func TestUserInitialInsert(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewUserRepository(db)

	const telegramID int64 = 100001

	if err := repo.UpsertTelegramUser(telegramID, "alice"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}

	got, err := repo.GetUserByTelegramID(telegramID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}

	if got.TelegramUserID != telegramID {
		t.Errorf("TelegramUserID: got %d, want %d", got.TelegramUserID, telegramID)
	}
	if got.Username != "alice" {
		t.Errorf("Username: got %q, want %q", got.Username, "alice")
	}
	if !got.Subscribed {
		t.Error("Subscribed: expected true after initial UpsertTelegramUser, got false")
	}

	// Confirm exactly one row exists.
	var count int64
	db.Model(&storage.User{}).Where("telegram_user_id = ?", telegramID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 row for telegram_user_id %d, got %d", telegramID, count)
	}
}

// TestUserUsernameUpdateOnRepeatUpsert verifies that calling UpsertTelegramUser
// twice with the same TelegramUserID but a different username updates the
// username without creating a duplicate row and without overwriting the
// Subscribed flag.
func TestUserUsernameUpdateOnRepeatUpsert(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewUserRepository(db)

	const telegramID int64 = 100002

	// First call — creates the user.
	if err := repo.UpsertTelegramUser(telegramID, "bob_old"); err != nil {
		t.Fatalf("first UpsertTelegramUser: %v", err)
	}

	// Manually unsubscribe to confirm upsert won't reset Subscribed.
	if err := repo.SetSubscription(telegramID, false); err != nil {
		t.Fatalf("SetSubscription: %v", err)
	}

	// Second call — same ID, new username.
	if err := repo.UpsertTelegramUser(telegramID, "bob_new"); err != nil {
		t.Fatalf("second UpsertTelegramUser: %v", err)
	}

	got, err := repo.GetUserByTelegramID(telegramID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}
	if got.Username != "bob_new" {
		t.Errorf("Username: got %q, want %q", got.Username, "bob_new")
	}
	// Subscribed must remain false — upsert must not touch it.
	if got.Subscribed {
		t.Error("Subscribed: expected false (not reset by upsert), got true")
	}

	// Confirm no duplicate row was created.
	var count int64
	db.Model(&storage.User{}).Where("telegram_user_id = ?", telegramID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 row for telegram_user_id %d, got %d", telegramID, count)
	}
}

// TestUserUnsubscribeOnStop verifies that SetSubscription(id, false) marks the
// user as unsubscribed and that ListSubscribedUsers no longer includes them.
func TestUserUnsubscribeOnStop(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewUserRepository(db)

	const telegramID int64 = 100003

	if err := repo.UpsertTelegramUser(telegramID, "carol"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}

	// User should be subscribed after initial upsert.
	subscribed, err := repo.ListSubscribedUsers()
	if err != nil {
		t.Fatalf("ListSubscribedUsers before unsubscribe: %v", err)
	}
	found := containsUserID(subscribed, telegramID)
	if !found {
		t.Fatal("expected user to be in subscribed list before /stop")
	}

	// Now unsubscribe.
	if err := repo.SetSubscription(telegramID, false); err != nil {
		t.Fatalf("SetSubscription(false): %v", err)
	}

	// Verify via GetUserByTelegramID.
	got, err := repo.GetUserByTelegramID(telegramID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}
	if got.Subscribed {
		t.Error("Subscribed: expected false after SetSubscription(false), got true")
	}

	// Verify ListSubscribedUsers no longer includes the user.
	subscribed, err = repo.ListSubscribedUsers()
	if err != nil {
		t.Fatalf("ListSubscribedUsers after unsubscribe: %v", err)
	}
	if containsUserID(subscribed, telegramID) {
		t.Error("expected user to be absent from subscribed list after /stop")
	}
}

// TestUserListOnlySubscribed creates three users, subscribes two of them, and
// verifies that ListSubscribedUsers returns exactly those two.
func TestUserListOnlySubscribed(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewUserRepository(db)

	users := []struct {
		id       int64
		username string
	}{
		{200001, "dave"},
		{200002, "eve"},
		{200003, "frank"},
	}

	for _, u := range users {
		if err := repo.UpsertTelegramUser(u.id, u.username); err != nil {
			t.Fatalf("UpsertTelegramUser(%d): %v", u.id, err)
		}
	}

	// Unsubscribe the third user so only dave and eve remain subscribed.
	if err := repo.SetSubscription(users[2].id, false); err != nil {
		t.Fatalf("SetSubscription(frank, false): %v", err)
	}

	subscribed, err := repo.ListSubscribedUsers()
	if err != nil {
		t.Fatalf("ListSubscribedUsers: %v", err)
	}

	if len(subscribed) != 2 {
		t.Fatalf("expected 2 subscribed users, got %d: %v", len(subscribed), userIDs(subscribed))
	}

	if !containsUserID(subscribed, users[0].id) {
		t.Errorf("expected dave (%d) in subscribed list", users[0].id)
	}
	if !containsUserID(subscribed, users[1].id) {
		t.Errorf("expected eve (%d) in subscribed list", users[1].id)
	}
	if containsUserID(subscribed, users[2].id) {
		t.Errorf("expected frank (%d) to be absent from subscribed list", users[2].id)
	}
}

// TestUserGetByTelegramIDNotFound verifies that GetUserByTelegramID returns a
// wrapped gorm.ErrRecordNotFound when no user exists with that ID.
func TestUserGetByTelegramIDNotFound(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewUserRepository(db)

	_, err := repo.GetUserByTelegramID(999999999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound in error chain, got: %v", err)
	}
}

// --- helpers ---------------------------------------------------------------

// containsUserID reports whether any user in the slice has the given TelegramUserID.
func containsUserID(users []storage.User, id int64) bool {
	for _, u := range users {
		if u.TelegramUserID == id {
			return true
		}
	}
	return false
}

// userIDs returns the TelegramUserID of each user in the slice, for diagnostic output.
func userIDs(users []storage.User) []int64 {
	ids := make([]int64, len(users))
	for i, u := range users {
		ids[i] = u.TelegramUserID
	}
	return ids
}
