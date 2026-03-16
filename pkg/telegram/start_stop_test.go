package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

// testStore holds the raw *gorm.DB alongside a UserRepository so tests can
// insert rows that require fields not exposed by the repository API (e.g.
// IsAdmin).
type testStore struct {
	db    *gorm.DB
	users *storage.UserRepository
}

// openTestStore opens a fresh SQLite database in a temp dir, runs all
// migrations, and returns a testStore. Each call produces an isolated store.
func openTestStore(t *testing.T) *testStore {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("openTestStore: Open: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("openTestStore: Migrate: %v", err)
	}
	return &testStore{
		db:    db,
		users: storage.NewUserRepository(db),
	}
}

// createAdminUser directly inserts a user record with IsAdmin=true.
func (s *testStore) createAdminUser(t *testing.T, telegramUserID int64, username string) {
	t.Helper()
	displayName := username
	if displayName == "" {
		displayName = "Admin User"
	}
	if err := s.db.Create(&storage.User{
		TelegramUserID: telegramUserID,
		Username:       username,
		DisplayName:    displayName,
		Subscribed:     true,
		IsAdmin:        true,
	}).Error; err != nil {
		t.Fatalf("createAdminUser: %v", err)
	}
}

// newRouterWithStore creates a Router wired to a FakeBot and the given store.
func newRouterWithStore(t *testing.T, store *testStore) (*Router, *FakeBot) {
	t.Helper()
	fb := &FakeBot{}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), fb, store.users, nil)
	return r, fb
}

// makeUpdate is a shorthand for makeTextUpdate.
func makeUpdate(userID, chatID int64, text string) *models.Update {
	return makeTextUpdate(userID, chatID, text)
}

// --- /start tests -----------------------------------------------------------

// TestStartSubscribesNewUser verifies that /start creates the user in the DB
// and marks them as subscribed.
func TestStartSubscribesNewUser(t *testing.T) {
	store := openTestStore(t)
	r, fb := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 1001
	const chatID int64 = 2001

	upd := makeUpdate(userID, chatID, "/start")
	r.handleStart(ctx, nil, upd)

	// User must exist and be subscribed.
	got, err := store.users.GetUserByTelegramID(userID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}
	if !got.Subscribed {
		t.Error("expected Subscribed=true after /start, got false")
	}
	if got.DisplayName != "TestUser" {
		t.Errorf("expected DisplayName=TestUser after /start, got %q", got.DisplayName)
	}

	// A confirmation message must have been sent.
	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected at least one message to be sent")
	}
	last := msgs[len(msgs)-1]
	if last.ChatID != chatID {
		t.Errorf("expected message to chatID %d, got %d", chatID, last.ChatID)
	}
}

// TestStartResubscribesExistingUser verifies that /start re-subscribes a user
// who had previously unsubscribed via /stop.
func TestStartResubscribesExistingUser(t *testing.T) {
	store := openTestStore(t)
	r, _ := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 1002
	const chatID int64 = 2002

	// Create user and immediately unsubscribe.
	if err := store.users.UpsertTelegramUser(userID, "dave", "Dave"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}
	if err := store.users.SetSubscription(userID, false); err != nil {
		t.Fatalf("SetSubscription(false): %v", err)
	}

	upd := makeUpdate(userID, chatID, "/start")
	r.handleStart(ctx, nil, upd)

	got, err := store.users.GetUserByTelegramID(userID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}
	if !got.Subscribed {
		t.Error("expected Subscribed=true after /start, got false")
	}
}

// --- /stop tests ------------------------------------------------------------

// TestStopUnsubscribesUser verifies that /stop marks the user as unsubscribed.
func TestStopUnsubscribesUser(t *testing.T) {
	store := openTestStore(t)
	r, fb := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 1003
	const chatID int64 = 2003

	// Subscribe the user first.
	upd := makeUpdate(userID, chatID, "/start")
	r.handleStart(ctx, nil, upd)

	// Now unsubscribe.
	upd = makeUpdate(userID, chatID, "/stop")
	r.handleStop(ctx, nil, upd)

	got, err := store.users.GetUserByTelegramID(userID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}
	if got.Subscribed {
		t.Error("expected Subscribed=false after /stop, got true")
	}

	// A confirmation message must have been sent.
	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected at least one message to be sent")
	}
}

// TestStopWithoutPriorStartCreatesAndUnsubscribes verifies that /stop without
// a prior /start still creates the user record and marks them unsubscribed.
func TestStopWithoutPriorStartCreatesAndUnsubscribes(t *testing.T) {
	store := openTestStore(t)
	r, _ := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 1004
	const chatID int64 = 2004

	upd := makeUpdate(userID, chatID, "/stop")
	r.handleStop(ctx, nil, upd)

	got, err := store.users.GetUserByTelegramID(userID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID: %v", err)
	}
	if got.Subscribed {
		t.Error("expected Subscribed=false after /stop for new user, got true")
	}
	if got.DisplayName != "TestUser" {
		t.Errorf("expected DisplayName=TestUser after /stop, got %q", got.DisplayName)
	}
}

// --- requireAdmin tests -----------------------------------------------------

// TestAuthRequireAdminRejectsUnknownUser verifies that requireAdmin sends a
// rejection and returns false for a user not present in the database.
func TestAuthRequireAdminRejectsUnknownUser(t *testing.T) {
	store := openTestStore(t)
	r, fb := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 9001
	const chatID int64 = 8001

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil {
		t.Fatalf("requireAdmin: unexpected error: %v", err)
	}
	if ok {
		t.Error("expected requireAdmin to return false for unknown user, got true")
	}

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected rejection message to be sent, got none")
	}
}

// TestAuthRequireAdminRejectsNonAdmin verifies that requireAdmin sends a
// rejection and returns false for a known but non-admin user.
func TestAuthRequireAdminRejectsNonAdmin(t *testing.T) {
	store := openTestStore(t)
	r, fb := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 9002
	const chatID int64 = 8002

	// Create a non-admin user.
	if err := store.users.UpsertTelegramUser(userID, "nonadmin", "Non Admin"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil {
		t.Fatalf("requireAdmin: unexpected error: %v", err)
	}
	if ok {
		t.Error("expected requireAdmin to return false for non-admin user, got true")
	}

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected rejection message to be sent, got none")
	}
}

// TestAuthRequireAdminAllowsAdmin verifies that requireAdmin returns true for a
// user whose IsAdmin flag is set in the database.
func TestAuthRequireAdminAllowsAdmin(t *testing.T) {
	store := openTestStore(t)
	r, fb := newRouterWithStore(t, store)
	ctx := context.Background()

	const userID int64 = 9003
	const chatID int64 = 8003

	// Insert an admin user directly (IsAdmin is not settable via the repo API).
	store.createAdminUser(t, userID, "superadmin")

	ok, err := r.requireAdmin(ctx, userID, chatID)
	if err != nil {
		t.Fatalf("requireAdmin: unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected requireAdmin to return true for admin user, got false")
	}

	// No rejection message should have been sent.
	msgs := fb.SentMessages()
	if len(msgs) != 0 {
		t.Errorf("expected no messages sent for admin user, got %d", len(msgs))
	}
}

// --- Broadcast tests --------------------------------------------------------

// errBot is a BotClient that fails SendMessage for a specific set of chat IDs
// and succeeds for all others. It records successfully-sent messages.
type errBot struct {
	mu      sync.Mutex
	failIDs map[int64]bool
	sent    []sentMessage
}

func (e *errBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	chatID, _ := params.ChatID.(int64)
	if e.failIDs[chatID] {
		return nil, errors.New("simulated send failure")
	}
	e.sent = append(e.sent, sentMessage{ChatID: chatID, Text: params.Text})
	return &models.Message{ID: 1}, nil
}

func (e *errBot) EditMessageText(_ context.Context, _ *bot.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: 1}, nil
}

func (e *errBot) AnswerCallbackQuery(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
	return true, nil
}

func (e *errBot) SentCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.sent)
}

// TestBroadcastSendsToAllSubscribed verifies that Broadcast delivers the
// message to every subscribed user.
func TestBroadcastSendsToAllSubscribed(t *testing.T) {
	store := openTestStore(t)
	eb := &errBot{failIDs: map[int64]bool{}}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), eb, store.users, nil)
	ctx := context.Background()

	for _, id := range []int64{3001, 3002, 3003} {
		if err := store.users.UpsertTelegramUser(id, "", "Viewer"); err != nil {
			t.Fatalf("UpsertTelegramUser(%d): %v", id, err)
		}
	}

	r.Broadcast(ctx, "hello everyone")

	if got := eb.SentCount(); got != 3 {
		t.Errorf("expected 3 messages sent, got %d", got)
	}
}

// TestBroadcastPartialFailureContinues verifies that a failure sending to one
// user does not prevent messages being sent to the remaining subscribed users.
func TestBroadcastPartialFailureContinues(t *testing.T) {
	store := openTestStore(t)
	// User 4002 will fail; 4001 and 4003 should still receive the message.
	eb := &errBot{failIDs: map[int64]bool{4002: true}}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), eb, store.users, nil)
	ctx := context.Background()

	for _, id := range []int64{4001, 4002, 4003} {
		if err := store.users.UpsertTelegramUser(id, "", "Viewer"); err != nil {
			t.Fatalf("UpsertTelegramUser(%d): %v", id, err)
		}
	}

	// Must not panic or abort early.
	r.Broadcast(ctx, "partial test")

	// Exactly 2 messages must have been delivered (4001 and 4003).
	if got := eb.SentCount(); got != 2 {
		t.Errorf("expected 2 successful sends (one failure skipped), got %d", got)
	}
}

// TestBroadcastSkipsUnsubscribed verifies that Broadcast does not send to
// users who have unsubscribed.
func TestBroadcastSkipsUnsubscribed(t *testing.T) {
	store := openTestStore(t)
	eb := &errBot{failIDs: map[int64]bool{}}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), eb, store.users, nil)
	ctx := context.Background()

	for _, id := range []int64{5001, 5002} {
		if err := store.users.UpsertTelegramUser(id, "", "Viewer"); err != nil {
			t.Fatalf("UpsertTelegramUser(%d): %v", id, err)
		}
	}
	if err := store.users.SetSubscription(5002, false); err != nil {
		t.Fatalf("SetSubscription(5002, false): %v", err)
	}

	r.Broadcast(ctx, "only one should get this")

	if got := eb.SentCount(); got != 1 {
		t.Errorf("expected 1 message sent (unsubscribed user skipped), got %d", got)
	}
}

// TestBroadcastExceptSkipsExcluded verifies that BroadcastExcept does not send
// to explicitly excluded users even when they are subscribed.
func TestBroadcastExceptSkipsExcluded(t *testing.T) {
	store := openTestStore(t)
	eb := &errBot{failIDs: map[int64]bool{}}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), eb, store.users, nil)
	ctx := context.Background()

	for _, id := range []int64{6001, 6002, 6003} {
		if err := store.users.UpsertTelegramUser(id, "", "Viewer"); err != nil {
			t.Fatalf("UpsertTelegramUser(%d): %v", id, err)
		}
	}

	r.BroadcastExcept(ctx, "skip one", 6002)

	if got := eb.SentCount(); got != 2 {
		t.Fatalf("expected 2 sends with one excluded user, got %d", got)
	}
	for _, m := range eb.sent {
		if m.ChatID == 6002 {
			t.Fatalf("excluded user %d unexpectedly received a message", m.ChatID)
		}
	}
}
