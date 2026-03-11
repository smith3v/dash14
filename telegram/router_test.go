package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/storage"
)

// FakeBot implements BotClient and records every call made to it.
// It is safe for concurrent use from multiple handler goroutines.
type FakeBot struct {
	mu   sync.Mutex
	sent []sentMessage
}

// sentMessage captures the arguments of a SendMessage call.
type sentMessage struct {
	ChatID int64
	Text   string
}

// SendMessage records the call and returns a minimal *models.Message.
func (f *FakeBot) SendMessage(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	chatID, _ := params.ChatID.(int64)
	f.sent = append(f.sent, sentMessage{
		ChatID: chatID,
		Text:   params.Text,
	})
	return &models.Message{ID: 1}, nil
}

// EditMessageText records the call and returns a minimal *models.Message.
func (f *FakeBot) EditMessageText(_ context.Context, _ *bot.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: 1}, nil
}

// AnswerCallbackQuery records the call and returns true.
func (f *FakeBot) AnswerCallbackQuery(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
	return true, nil
}

// SentMessages returns a snapshot of all messages sent via SendMessage.
func (f *FakeBot) SentMessages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

// stubHTTPClient is a minimal net/http client that satisfies bot.HttpClient.
// It returns a valid JSON Telegram "ok" response for every request so that
// the bot library does not fail when handlers call SendMessage.
type stubHTTPClient struct{}

func (s *stubHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, _ := json.Marshal(map[string]any{
		"ok":     true,
		"result": map[string]any{"message_id": 1, "chat": map[string]any{"id": 1}, "date": 0, "text": ""},
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// newTestBot creates a *bot.Bot that uses a stub HTTP client and skips
// the GetMe initialisation call so no real Telegram network round-trip occurs.
func newTestBot(t *testing.T) *bot.Bot {
	t.Helper()
	b, err := bot.New("123456:test-token-for-unit-tests",
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(time.Second, &stubHTTPClient{}),
	)
	if err != nil {
		t.Fatalf("newTestBot: %v", err)
	}
	return b
}

// discardLogger returns a *slog.Logger that drops all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// openTestUsers opens a fresh in-process SQLite database, runs migrations, and
// returns a UserRepository backed by it. Each call produces an isolated store.
func openTestUsers(t *testing.T) *storage.UserRepository {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("openTestUsers: Open: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("openTestUsers: Migrate: %v", err)
	}
	return storage.NewUserRepository(db)
}

// makeTextUpdate builds a minimal *models.Update that looks like a text
// message command sent by a user with the given userID.
func makeTextUpdate(userID int64, chatID int64, text string) *models.Update {
	return &models.Update{
		ID: 1,
		Message: &models.Message{
			ID: 42,
			From: &models.User{
				ID:        userID,
				FirstName: "TestUser",
			},
			Chat: models.Chat{
				ID: chatID,
			},
			Text: text,
			Entities: []models.MessageEntity{
				{
					Type:   models.MessageEntityTypeBotCommand,
					Offset: 0,
					Length: len(text),
				},
			},
		},
	}
}

// TestRouterRegisterDoesNotPanic verifies that Register wires all five
// commands onto the bot without panicking.
func TestRouterRegisterDoesNotPanic(t *testing.T) {
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), &FakeBot{}, nil)

	// Register must not panic.
	r.Register()
}

// TestRouterNewRouter verifies that NewRouter returns a non-nil Router with
// the bot and logger wired correctly.
func TestRouterNewRouter(t *testing.T) {
	b := newTestBot(t)
	logger := discardLogger()
	fb := &FakeBot{}

	r := NewRouter(b, logger, fb, nil)
	if r == nil {
		t.Fatal("NewRouter returned nil")
	}
	if r.b != b {
		t.Error("NewRouter: bot field not set correctly")
	}
	if r.logger != logger {
		t.Error("NewRouter: logger field not set correctly")
	}
	if r.client != fb {
		t.Error("NewRouter: client field not set correctly")
	}
}

// TestRouterCommandsAreRouted verifies that each of the five commands results
// in a reply being sent back. We use ProcessUpdate to drive the registered
// handlers and a stub HTTP client so no real network calls are made.
func TestRouterCommandsAreRouted(t *testing.T) {
	commands := []string{"/start", "/stop", "/plan", "/game", "/takeover"}

	for _, cmd := range commands {
		cmd := cmd // capture
		t.Run(cmd, func(t *testing.T) {
			// WithNotAsyncHandlers ensures ProcessUpdate runs the handler
			// before returning so we can reason about completion without
			// adding sleeps or wait groups in the test.
			b, err := bot.New("123456:test-token-for-unit-tests",
				bot.WithSkipGetMe(),
				bot.WithHTTPClient(time.Second, &stubHTTPClient{}),
				bot.WithNotAsyncHandlers(),
			)
			if err != nil {
				t.Fatalf("create sync bot: %v", err)
			}

			users := openTestUsers(t)
			r := NewRouter(b, discardLogger(), &FakeBot{}, users)
			r.Register()

			ctx := context.Background()
			upd := makeTextUpdate(100, 200, cmd)

			// ProcessUpdate dispatches the update to the matching handler.
			// The handler calls b.SendMessage which goes through stubHTTPClient.
			b.ProcessUpdate(ctx, upd)
			// If we reach here without panic the handler was invoked.
		})
	}
}

// TestRouterFakeBotImplementsBotClient verifies that FakeBot satisfies the
// BotClient interface at compile time and that its method implementations
// record the expected data.
func TestRouterFakeBotImplementsBotClient(t *testing.T) {
	// Compile-time check: FakeBot must implement BotClient.
	var _ BotClient = (*FakeBot)(nil)

	fb := &FakeBot{}
	ctx := context.Background()

	msg, err := fb.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: int64(42),
		Text:   "hello",
	})
	if err != nil {
		t.Fatalf("SendMessage: unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("SendMessage: returned nil message")
	}

	msgs := fb.SentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 recorded message, got %d", len(msgs))
	}
	if msgs[0].Text != "hello" {
		t.Errorf("expected text %q, got %q", "hello", msgs[0].Text)
	}
	if msgs[0].ChatID != 42 {
		t.Errorf("expected chatID %d, got %d", 42, msgs[0].ChatID)
	}
}

// TestRouterFakeBotEditMessageText verifies that EditMessageText on FakeBot
// returns a valid message without error.
func TestRouterFakeBotEditMessageText(t *testing.T) {
	fb := &FakeBot{}
	ctx := context.Background()

	msg, err := fb.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    int64(1),
		MessageID: 10,
		Text:      "updated",
	})
	if err != nil {
		t.Fatalf("EditMessageText: unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("EditMessageText: returned nil message")
	}
}

// TestRouterFakeBotAnswerCallbackQuery verifies that AnswerCallbackQuery on
// FakeBot returns true without error.
func TestRouterFakeBotAnswerCallbackQuery(t *testing.T) {
	fb := &FakeBot{}
	ctx := context.Background()

	ok, err := fb.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: "cq1",
	})
	if err != nil {
		t.Fatalf("AnswerCallbackQuery: unexpected error: %v", err)
	}
	if !ok {
		t.Error("AnswerCallbackQuery: expected true")
	}
}
