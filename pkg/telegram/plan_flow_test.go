package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/overlay"
	"github.com/smith3v/dash14/pkg/storage"
)

// planTestStore bundles a raw DB, a UserRepository, and a TeamRepository for
// use by plan wizard tests.
type planTestStore struct {
	*testStore
	teams *storage.TeamRepository
	games *storage.GameRepository
}

type txFailGames struct {
	inner        *storage.GameRepository
	failCreate   bool
	failWithinTx bool
}

func (g *txFailGames) WithinTx(fn func(repo *storage.GameRepository) error) error {
	if g.failWithinTx {
		return fmt.Errorf("simulated transaction setup failure")
	}
	return g.inner.WithinTx(func(repo *storage.GameRepository) error {
		if g.failCreate {
			return fmt.Errorf("simulated create failure")
		}
		return fn(repo)
	})
}

func (g *txFailGames) CreateGame(game *storage.Game) error    { return g.inner.CreateGame(game) }
func (g *txFailGames) CreateSet(set *storage.GameSet) error   { return g.inner.CreateSet(set) }
func (g *txFailGames) GetCurrentGame() (*storage.Game, error) { return g.inner.GetCurrentGame() }
func (g *txFailGames) GetNonFinishedGame() (*storage.Game, error) {
	return g.inner.GetNonFinishedGame()
}
func (g *txFailGames) GetGameByID(id uint) (*storage.Game, error) { return g.inner.GetGameByID(id) }
func (g *txFailGames) GetActiveSet(gameID uint) (*storage.GameSet, error) {
	return g.inner.GetActiveSet(gameID)
}
func (g *txFailGames) ListSetsByGameID(gameID uint) ([]storage.GameSet, error) {
	return g.inner.ListSetsByGameID(gameID)
}
func (g *txFailGames) SaveGame(game *storage.Game) error  { return g.inner.SaveGame(game) }
func (g *txFailGames) SaveSet(set *storage.GameSet) error { return g.inner.SaveSet(set) }

type fakeOverlayRenderer struct {
	mu           sync.Mutex
	planned      []overlay.PlannedViewModel
	live         []overlay.LiveViewModel
	intermission []overlay.IntermissionViewModel
	finished     []overlay.FinishedViewModel
}

func (f *fakeOverlayRenderer) RenderPlanned(vm overlay.PlannedViewModel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planned = append(f.planned, vm)
	return nil
}

func (f *fakeOverlayRenderer) RenderLive(vm overlay.LiveViewModel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live = append(f.live, vm)
	return nil
}

func (f *fakeOverlayRenderer) RenderIntermission(vm overlay.IntermissionViewModel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intermission = append(f.intermission, vm)
	return nil
}

func (f *fakeOverlayRenderer) RenderIntermissionMain(vm overlay.IntermissionViewModel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intermission = append(f.intermission, vm)
	return nil
}

func (f *fakeOverlayRenderer) RenderFinished(vm overlay.FinishedViewModel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, vm)
	return nil
}

func (f *fakeOverlayRenderer) plannedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.planned)
}

func (f *fakeOverlayRenderer) lastPlanned() overlay.PlannedViewModel {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.planned[len(f.planned)-1]
}

func (f *fakeOverlayRenderer) liveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.live)
}

func (f *fakeOverlayRenderer) intermissionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.intermission)
}

func (f *fakeOverlayRenderer) finishedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.finished)
}

func (f *fakeOverlayRenderer) lastIntermission() overlay.IntermissionViewModel {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.intermission[len(f.intermission)-1]
}

func waitForCondition(t *testing.T, desc string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// openPlanTestStore creates an isolated SQLite database with all migrations
// applied and returns a planTestStore.
func openPlanTestStore(t *testing.T) *planTestStore {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("openPlanTestStore: Open: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("openPlanTestStore: Migrate: %v", err)
	}
	return &planTestStore{
		testStore: &testStore{
			db:    db,
			users: storage.NewUserRepository(db),
		},
		teams: storage.NewTeamRepository(db),
		games: storage.NewGameRepository(db),
	}
}

// insertTeam is a test helper that inserts a team directly into the repository
// and returns its record.
func insertTeam(t *testing.T, repo *storage.TeamRepository, key, name, shortName string) *storage.Team {
	t.Helper()
	team := &storage.Team{
		Key:       key,
		Name:      name,
		ShortName: shortName,
	}
	if err := repo.UpsertTeam(team); err != nil {
		t.Fatalf("insertTeam %q: %v", key, err)
	}
	got, err := repo.GetTeamByKey(key)
	if err != nil {
		t.Fatalf("insertTeam GetTeamByKey %q: %v", key, err)
	}
	return got
}

// newPlanRouter creates a Router wired to a FakeBot and the given store, with
// both the users and teams repositories populated.
func newPlanRouter(t *testing.T, store *planTestStore) (*Router, *FakeBot, *fakeOverlayRenderer) {
	t.Helper()
	fb := &FakeBot{}
	renderer := &fakeOverlayRenderer{}
	b := newTestBot(t)
	r := NewRouter(b, discardLogger(), fb, store.users, store.teams)
	r.SetGameServices(store.games, renderer)
	return r, fb, renderer
}

// makePlainTextUpdate builds a *models.Update that looks like a plain text
// message (not a command) sent by userID. It does not add a bot command entity,
// which allows handlePlanText to process it as a search query.
func makePlainTextUpdate(userID int64, chatID int64, text string) *models.Update {
	return &models.Update{
		ID: 1,
		Message: &models.Message{
			ID: 43,
			From: &models.User{
				ID:        userID,
				FirstName: "TestUser",
			},
			Chat: models.Chat{
				ID: chatID,
			},
			Text: text,
		},
	}
}

// makeCallbackUpdate builds a minimal *models.Update that looks like an inline
// keyboard callback sent by a user.
func makeCallbackUpdate(userID int64, chatID int64, callbackID, data string) *models.Update {
	return &models.Update{
		ID: 1,
		CallbackQuery: &models.CallbackQuery{
			ID:   callbackID,
			From: models.User{ID: userID, FirstName: "TestUser"},
			Message: models.MaybeInaccessibleMessage{
				Type: models.MaybeInaccessibleMessageTypeMessage,
				Message: &models.Message{
					ID:   99,
					Chat: models.Chat{ID: chatID},
				},
			},
			Data: data,
		},
	}
}

// ---- TestPlanNonAdminRejected -----------------------------------------------

// TestPlanNonAdminRejected verifies that a user who is not an admin receives
// an authorisation rejection when they issue /plan.
func TestPlanNonAdminRejected(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7001
	const chatID int64 = 8001

	// Insert user as a non-admin.
	if err := store.users.UpsertTelegramUser(userID, "notadmin", "Not Admin"); err != nil {
		t.Fatalf("UpsertTelegramUser: %v", err)
	}

	upd := makeTextUpdate(userID, chatID, "/plan")
	r.handlePlan(ctx, nil, upd)

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected a rejection message to be sent, got none")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "not authorised") {
		t.Errorf("expected rejection text to contain 'not authorised', got %q", last.Text)
	}

	// No plan state should have been created.
	if _, ok := r.plans.Load(userID); ok {
		t.Error("expected no plan state to be stored for non-admin user")
	}
}

// ---- TestPlan1Match ---------------------------------------------------------

// TestPlan1Match verifies that when exactly one team matches the query, the
// wizard auto-selects it and moves on to ask for the guest team.
func TestPlan1Match(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7002
	const chatID int64 = 8002

	store.createAdminUser(t, userID, "admin1")
	team := insertTeam(t, store.teams, "alpha", "Alpha Volleyball Club", "Alpha")

	// Step 1: invoke /plan → expect "enter home team" prompt.
	upd := makeTextUpdate(userID, chatID, "/plan")
	r.handlePlan(ctx, nil, upd)

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected a message after /plan")
	}
	if !strings.Contains(msgs[len(msgs)-1].Text, "home team") {
		t.Errorf("expected home team prompt, got %q", msgs[len(msgs)-1].Text)
	}

	// Step 2: send team query → single result auto-selects.
	textUpd := makePlainTextUpdate(userID, chatID, "Alpha")
	r.handlePlanText(ctx, nil, textUpd)

	msgs = fb.SentMessages()
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, team.Name) {
		t.Errorf("expected confirmation to contain team name %q, got %q", team.Name, last.Text)
	}
	if !strings.Contains(last.Text, "guest team") {
		t.Errorf("expected prompt for guest team after auto-select, got %q", last.Text)
	}

	// The plan state should have HomeTeam set.
	raw, ok := r.plans.Load(userID)
	if !ok {
		t.Fatal("expected plan state to exist after auto-select")
	}
	state := raw.(*planState)
	if state.HomeTeam == nil {
		t.Fatal("expected HomeTeam to be set in plan state")
	}
	if state.HomeTeam.ID != team.ID {
		t.Errorf("expected HomeTeam.ID=%d, got %d", team.ID, state.HomeTeam.ID)
	}
}

// ---- TestPlan2To8Matches ----------------------------------------------------

// TestPlan2To8Matches verifies that when 2-8 teams match the query, the wizard
// presents an inline keyboard so the admin can pick one.
func TestPlan2To8Matches(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7003
	const chatID int64 = 8003

	store.createAdminUser(t, userID, "admin2")

	// Insert 3 teams that all share the prefix "Beta".
	var inserted []*storage.Team
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("beta%d", i)
		name := fmt.Sprintf("Beta Club %d", i)
		inserted = append(inserted, insertTeam(t, store.teams, key, name, fmt.Sprintf("B%d", i)))
	}

	// Invoke /plan.
	upd := makeTextUpdate(userID, chatID, "/plan")
	r.handlePlan(ctx, nil, upd)

	// Send the search query that matches all three teams.
	textUpd := makePlainTextUpdate(userID, chatID, "Beta")
	r.handlePlanText(ctx, nil, textUpd)

	msgs := fb.SentMessages()
	last := msgs[len(msgs)-1]

	// The reply markup must be an InlineKeyboardMarkup.
	if last.ReplyMarkup == nil {
		t.Fatal("expected inline keyboard to be sent, got nil ReplyMarkup")
	}
	kb, ok := last.ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected *models.InlineKeyboardMarkup, got %T", last.ReplyMarkup)
	}
	if len(kb.InlineKeyboard) != len(inserted) {
		t.Errorf("expected %d buttons, got %d", len(inserted), len(kb.InlineKeyboard))
	}
	for _, row := range kb.InlineKeyboard {
		if len(row) != 1 {
			t.Errorf("expected 1 button per row, got %d", len(row))
		}
		btn := row[0]
		if !strings.HasPrefix(btn.CallbackData, "plan:home:") {
			t.Errorf("expected callback data to start with 'plan:home:', got %q", btn.CallbackData)
		}
	}

	// HomeTeam must still be nil — no selection made yet.
	raw, ok := r.plans.Load(userID)
	if !ok {
		t.Fatal("expected plan state to exist")
	}
	if raw.(*planState).HomeTeam != nil {
		t.Error("expected HomeTeam to remain nil before user selects from keyboard")
	}

	// Simulate the admin clicking the first button.
	firstTeamID := inserted[0].ID
	cbUpd := makeCallbackUpdate(userID, chatID, "cbq1",
		fmt.Sprintf("plan:home:%d", firstTeamID))
	r.handlePlanCallback(ctx, nil, cbUpd)

	// Now HomeTeam should be set.
	raw, ok = r.plans.Load(userID)
	if !ok {
		t.Fatal("expected plan state to exist after callback")
	}
	state := raw.(*planState)
	if state.HomeTeam == nil {
		t.Fatal("expected HomeTeam to be set after callback selection")
	}
	if state.HomeTeam.ID != firstTeamID {
		t.Errorf("expected HomeTeam.ID=%d after selection, got %d", firstTeamID, state.HomeTeam.ID)
	}

	// A confirmation message should have been sent.
	msgs = fb.SentMessages()
	last = msgs[len(msgs)-1]
	if !strings.Contains(last.Text, inserted[0].Name) {
		t.Errorf("expected confirmation to contain team name %q, got %q", inserted[0].Name, last.Text)
	}
}

// ---- TestPlanMoreThan8 ------------------------------------------------------

// TestPlanMoreThan8 verifies that when more than 8 teams match the query, the
// wizard asks the admin to refine the search rather than displaying a huge list.
func TestPlanMoreThan8(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7004
	const chatID int64 = 8004

	store.createAdminUser(t, userID, "admin3")

	// Insert 9 teams that all share the prefix "Gamma".
	for i := 1; i <= 9; i++ {
		key := fmt.Sprintf("gamma%d", i)
		name := fmt.Sprintf("Gamma Club %d", i)
		insertTeam(t, store.teams, key, name, fmt.Sprintf("G%d", i))
	}

	// Invoke /plan.
	upd := makeTextUpdate(userID, chatID, "/plan")
	r.handlePlan(ctx, nil, upd)

	// Send a query that matches all 9 teams.
	textUpd := makePlainTextUpdate(userID, chatID, "Gamma")
	r.handlePlanText(ctx, nil, textUpd)

	msgs := fb.SentMessages()
	last := msgs[len(msgs)-1]

	if !strings.Contains(last.Text, "Too many results") {
		t.Errorf("expected 'Too many results' message, got %q", last.Text)
	}
	if last.ReplyMarkup != nil {
		t.Error("expected no inline keyboard for >8 results, got one")
	}
}

// TestPlanCreateGameFromGuestSelection verifies that after selecting home and
// guest teams, /plan creates a planned game, assigns the current admin, and
// renders the planned overlay.
func TestPlanCreateGameFromGuestSelection(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7101
	const chatID int64 = 8101
	store.createAdminUser(t, userID, "planner")

	home := insertTeam(t, store.teams, "home", "Home Club", "HOM")
	guest := insertTeam(t, store.teams, "guest", "Guest Club", "GST")

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(userID, chatID, "Home"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(userID, chatID, "Guest"))

	current, err := store.games.GetCurrentGame()
	if err != nil {
		t.Fatalf("GetCurrentGame: %v", err)
	}
	if current == nil {
		t.Fatal("expected current game after /plan completion")
	}
	if current.Status != storage.GameStatusPlanned {
		t.Fatalf("expected planned status, got %q", current.Status)
	}
	if current.Phase != storage.GamePhasePlanned {
		t.Fatalf("expected planned phase, got %q", current.Phase)
	}
	if current.HomeTeamID != home.ID || current.GuestTeamID != guest.ID {
		t.Fatalf("unexpected teams on planned game: home=%d guest=%d", current.HomeTeamID, current.GuestTeamID)
	}
	if current.CurrentAdminUserID != userID {
		t.Fatalf("expected current_admin_user_id=%d, got %d", userID, current.CurrentAdminUserID)
	}

	if len(renderer.planned) != 1 {
		t.Fatalf("expected exactly one planned render call, got %d", len(renderer.planned))
	}
	vm := renderer.planned[0]
	if vm.HomeTeamName != home.Name || vm.GuestTeamName != guest.Name {
		t.Fatalf("unexpected planned view model teams: %#v", vm)
	}

	msgs := fb.SentMessages()
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "Planned game created") {
		t.Fatalf("expected success confirmation message, got %q", last.Text)
	}
}

func TestPlanWithoutCurrentGameStartsTeamSelection(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7102
	const chatID int64 = 8102
	store.createAdminUser(t, userID, "planner2")

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))

	msgs := fb.SentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected one prompt, got %d", len(msgs))
	}
	if msgs[0].Text != "Please enter the home team name:" {
		t.Fatalf("unexpected prompt: %q", msgs[0].Text)
	}
	if _, ok := r.plans.Load(userID); !ok {
		t.Fatal("expected plan state to be created")
	}
}

func TestPlanWithInProgressGameStopsBeforeTeamSelection(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7104
	const chatID int64 = 8104
	store.createAdminUser(t, userID, "busy-planner")
	game := createCurrentPlannedGame(t, store, userID)
	game.Status = storage.GameStatusInProgress
	game.Phase = storage.GamePhaseBetweenSets
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))

	msgs := fb.SentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected one blocking message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "Control Home") || !strings.Contains(msgs[0].Text, "Control Guest") {
		t.Fatalf("expected current teams in message, got %q", msgs[0].Text)
	}
	if !strings.Contains(msgs[0].Text, "Finish this game") {
		t.Fatalf("expected finish guidance, got %q", msgs[0].Text)
	}
	if _, ok := r.plans.Load(userID); ok {
		t.Fatal("expected no plan state while a game is in progress")
	}
}

func TestPlanWithPlannedGameRequestsReplacementConfirmation(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7105
	const chatID int64 = 8105
	store.createAdminUser(t, userID, "replacement-planner")
	game := createCurrentPlannedGame(t, store, userID)
	game.ControlMessageID = 77
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))

	msgs := fb.SentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected one confirmation message, got %d", len(msgs))
	}
	last := msgs[0]
	if !strings.Contains(last.Text, "Control Home") || !strings.Contains(last.Text, "Control Guest") {
		t.Fatalf("expected planned teams in confirmation, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "Would you like to plan another game instead?") {
		t.Fatalf("expected replacement question, got %q", last.Text)
	}
	kb, ok := last.ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard, got %T", last.ReplyMarkup)
	}
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected one row with two buttons, got %#v", kb.InlineKeyboard)
	}
	if got := kb.InlineKeyboard[0][0]; got.Text != "Yes, plan another" || got.CallbackData != "plan:replace:start" {
		t.Fatalf("unexpected positive button: %#v", got)
	}
	if got := kb.InlineKeyboard[0][1]; got.Text != "No, keep current" || got.CallbackData != "plan:replace:keep" {
		t.Fatalf("unexpected negative button: %#v", got)
	}

	raw, ok := r.plans.Load(userID)
	if !ok {
		t.Fatal("expected pending replacement state")
	}
	state := raw.(*planState)
	if state.Replacement == nil || !state.Replacement.AwaitingConfirmation {
		t.Fatalf("expected awaiting replacement state, got %#v", state.Replacement)
	}
	if state.Replacement.GameID != game.ID ||
		state.Replacement.ExpectedHomeTeamID != game.HomeTeamID ||
		state.Replacement.ExpectedGuestTeamID != game.GuestTeamID ||
		state.Replacement.PreviousControlMessageID != 77 {
		t.Fatalf("unexpected replacement snapshot: %#v", state.Replacement)
	}
}

func TestPlanReplacementKeepClearsState(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7106
	const chatID int64 = 8106
	store.createAdminUser(t, userID, "keep-planner")
	game := createCurrentPlannedGame(t, store, userID)

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))
	r.handlePlanCallback(ctx, nil, makeCallbackUpdate(userID, chatID, "keep", "plan:replace:keep"))

	if _, ok := r.plans.Load(userID); ok {
		t.Fatal("expected replacement state to be cleared")
	}
	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.HomeTeamID != game.HomeTeamID || got.GuestTeamID != game.GuestTeamID {
		t.Fatalf("planned game changed after keep: got %d vs %d", got.HomeTeamID, got.GuestTeamID)
	}
	if renderer.plannedCount() != 0 || renderer.intermissionCount() != 0 {
		t.Fatal("expected no overlay render after keeping current game")
	}
	msgs := fb.SentMessages()
	if !strings.Contains(msgs[len(msgs)-1].Text, "kept") {
		t.Fatalf("expected keep confirmation, got %q", msgs[len(msgs)-1].Text)
	}
}

func TestPlanReplacementStartContinuesTeamSelection(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7107
	const chatID int64 = 8107
	store.createAdminUser(t, userID, "continue-planner")
	createCurrentPlannedGame(t, store, userID)

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))
	r.handlePlanCallback(ctx, nil, makeCallbackUpdate(userID, chatID, "start", "plan:replace:start"))

	msgs := fb.SentMessages()
	if got := msgs[len(msgs)-1].Text; got != "Please enter the home team name:" {
		t.Fatalf("unexpected prompt after confirmation: %q", got)
	}
	raw, ok := r.plans.Load(userID)
	if !ok {
		t.Fatal("expected replacement state to remain during team selection")
	}
	state := raw.(*planState)
	if state.Replacement == nil || state.Replacement.AwaitingConfirmation {
		t.Fatalf("expected confirmed replacement state, got %#v", state.Replacement)
	}
}

func TestPlanReplacementIgnoresTextBeforeConfirmation(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7108
	const chatID int64 = 8108
	store.createAdminUser(t, userID, "waiting-planner")
	createCurrentPlannedGame(t, store, userID)
	insertTeam(t, store.teams, "ignored-team", "Ignored Team", "IGN")

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))
	before := len(fb.SentMessages())
	r.handlePlanText(ctx, nil, makePlainTextUpdate(userID, chatID, "Ignored Team"))

	if got := len(fb.SentMessages()); got != before {
		t.Fatalf("expected text to be ignored before confirmation, messages %d -> %d", before, got)
	}
	raw, ok := r.plans.Load(userID)
	if !ok || raw.(*planState).HomeTeam != nil {
		t.Fatal("expected no home team selection before confirmation")
	}
}

func TestPlanReplacementStartRejectsGameStartedBeforeConfirmation(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7109
	const chatID int64 = 8109
	store.createAdminUser(t, userID, "stale-planner")
	game := createCurrentPlannedGame(t, store, userID)

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))
	game.Status = storage.GameStatusInProgress
	game.Phase = storage.GamePhaseBetweenSets
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	r.handlePlanCallback(ctx, nil, makeCallbackUpdate(userID, chatID, "stale", "plan:replace:start"))

	if _, ok := r.plans.Load(userID); ok {
		t.Fatal("expected stale replacement state to be cleared")
	}
	msgs := fb.SentMessages()
	if !strings.Contains(msgs[len(msgs)-1].Text, "changed") {
		t.Fatalf("expected changed-game message, got %q", msgs[len(msgs)-1].Text)
	}
}

func confirmPlannedGameReplacement(r *Router, userID, chatID int64) {
	r.handlePlan(context.Background(), nil, makeTextUpdate(userID, chatID, "/plan"))
	r.handlePlanCallback(
		context.Background(),
		nil,
		makeCallbackUpdate(userID, chatID, "replace-start", "plan:replace:start"),
	)
}

func TestPlanReplacementDoesNotPersistBeforeBothTeams(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, renderer := newPlanRouter(t, store)

	const userID int64 = 7110
	const chatID int64 = 8110
	store.createAdminUser(t, userID, "partial-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	newHome := insertTeam(t, store.teams, "partial-new-home", "Partial New Home", "PNH")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.HomeTeamID != game.HomeTeamID || got.GuestTeamID != game.GuestTeamID {
		t.Fatalf("game changed before guest selection: got %d vs %d, want %d vs %d", got.HomeTeamID, got.GuestTeamID, game.HomeTeamID, game.GuestTeamID)
	}
	if renderer.plannedCount() != 0 || renderer.intermissionCount() != 0 {
		t.Fatal("expected no overlay render before both replacement teams are selected")
	}
	raw, ok := r.plans.Load(userID)
	if !ok || raw.(*planState).HomeTeam == nil || raw.(*planState).HomeTeam.ID != newHome.ID {
		t.Fatal("expected replacement home team to remain in wizard state")
	}
}

func TestPlanReplacementUpdatesSameGameAndRenders(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)

	const userID int64 = 7111
	const chatID int64 = 8111
	store.createAdminUser(t, userID, "complete-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	game.ControlMessageID = 88
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	newHome := insertTeam(t, store.teams, "complete-new-home", "Complete New Home", "CNH")
	newGuest := insertTeam(t, store.teams, "complete-new-guest", "Complete New Guest", "CNG")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newGuest.Name))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.ID != game.ID {
		t.Fatalf("replacement ID: got %d, want %d", got.ID, game.ID)
	}
	if got.HomeTeamID != newHome.ID || got.GuestTeamID != newGuest.ID {
		t.Fatalf("replacement teams: got %d vs %d, want %d vs %d", got.HomeTeamID, got.GuestTeamID, newHome.ID, newGuest.ID)
	}
	if got.ControlMessageID != 0 || got.CurrentAdminUserID != userID {
		t.Fatalf("replacement control metadata: admin=%d message=%d", got.CurrentAdminUserID, got.ControlMessageID)
	}
	if renderer.plannedCount() != 1 || renderer.intermissionCount() != 1 {
		t.Fatalf("render counts: planned=%d intermission=%d, want 1/1", renderer.plannedCount(), renderer.intermissionCount())
	}
	planned := renderer.lastPlanned()
	if planned.HomeTeamName != newHome.Name || planned.GuestTeamName != newGuest.Name {
		t.Fatalf("planned overlay teams: %#v", planned)
	}
	intermission := renderer.lastIntermission()
	if intermission.HomeTeamName != newHome.Name || intermission.GuestTeamName != newGuest.Name {
		t.Fatalf("intermission overlay teams: %#v", intermission)
	}
	if _, ok := r.plans.Load(userID); ok {
		t.Fatal("expected completed replacement state to be cleared")
	}
	msgs := fb.SentMessages()
	if !strings.Contains(msgs[len(msgs)-1].Text, "Planned game updated") {
		t.Fatalf("expected update confirmation, got %q", msgs[len(msgs)-1].Text)
	}
}

func TestPlanReplacementRejectsGameStartedDuringWizard(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)

	const userID int64 = 7112
	const chatID int64 = 8112
	store.createAdminUser(t, userID, "started-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	newHome := insertTeam(t, store.teams, "started-new-home", "Started New Home", "SNH")
	newGuest := insertTeam(t, store.teams, "started-new-guest", "Started New Guest", "SNG")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))
	game.Status = storage.GameStatusInProgress
	game.Phase = storage.GamePhaseBetweenSets
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newGuest.Name))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.Status != storage.GameStatusInProgress || got.HomeTeamID != game.HomeTeamID || got.GuestTeamID != game.GuestTeamID {
		t.Fatalf("started game was replaced: status=%q teams=%d/%d", got.Status, got.HomeTeamID, got.GuestTeamID)
	}
	if renderer.plannedCount() != 0 || renderer.intermissionCount() != 0 {
		t.Fatal("expected no replacement overlay after game started")
	}
	if _, ok := r.plans.Load(userID); ok {
		t.Fatal("expected conflicting replacement state to be cleared")
	}
	msgs := fb.SentMessages()
	if !strings.Contains(msgs[len(msgs)-1].Text, "changed") {
		t.Fatalf("expected changed-game message, got %q", msgs[len(msgs)-1].Text)
	}
}

func TestPlanReplacementRejectsConcurrentReplacement(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, renderer := newPlanRouter(t, store)

	const userID int64 = 7113
	const chatID int64 = 8113
	store.createAdminUser(t, userID, "concurrent-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	newHome := insertTeam(t, store.teams, "concurrent-new-home", "Concurrent New Home", "CNH")
	newGuest := insertTeam(t, store.teams, "concurrent-new-guest", "Concurrent New Guest", "CNG")
	winnerHome := insertTeam(t, store.teams, "winner-home", "Winner Home", "WH")
	winnerGuest := insertTeam(t, store.teams, "winner-guest", "Winner Guest", "WG")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))
	if err := store.games.ReplacePlannedGame(
		game.ID,
		game.HomeTeamID,
		game.GuestTeamID,
		winnerHome.ID,
		winnerGuest.ID,
		999,
	); err != nil {
		t.Fatalf("concurrent ReplacePlannedGame: %v", err)
	}
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newGuest.Name))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.HomeTeamID != winnerHome.ID || got.GuestTeamID != winnerGuest.ID || got.CurrentAdminUserID != 999 {
		t.Fatalf("stale wizard overwrote concurrent replacement: teams=%d/%d admin=%d", got.HomeTeamID, got.GuestTeamID, got.CurrentAdminUserID)
	}
	if renderer.plannedCount() != 0 || renderer.intermissionCount() != 0 {
		t.Fatal("expected no overlay render for stale replacement")
	}
}

func TestPlanReplacementCallbackCannotApplyTwice(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, renderer := newPlanRouter(t, store)

	const userID int64 = 7114
	const chatID int64 = 8114
	store.createAdminUser(t, userID, "repeated-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	newHome := insertTeam(t, store.teams, "repeated-new-home", "Repeated New Home", "RNH")
	newGuest := insertTeam(t, store.teams, "repeated-new-guest", "Repeated New Guest", "RNG")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))
	guestCallback := makeCallbackUpdate(userID, chatID, "guest-repeat", fmt.Sprintf("plan:guest:%d", newGuest.ID))
	r.handlePlanCallback(context.Background(), nil, guestCallback)
	r.handlePlanCallback(context.Background(), nil, guestCallback)

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.HomeTeamID != newHome.ID || got.GuestTeamID != newGuest.ID {
		t.Fatalf("replacement teams: got %d/%d, want %d/%d", got.HomeTeamID, got.GuestTeamID, newHome.ID, newGuest.ID)
	}
	if renderer.plannedCount() != 1 || renderer.intermissionCount() != 1 {
		t.Fatalf("repeated callback rendered more than once: planned=%d intermission=%d", renderer.plannedCount(), renderer.intermissionCount())
	}
}

func TestPlanReplacementDisablesPreviousControlMessage(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)

	const userID int64 = 7115
	const chatID int64 = 8115
	store.createAdminUser(t, userID, "control-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	game.ControlMessageID = 91
	if err := store.games.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}
	newHome := insertTeam(t, store.teams, "control-new-home", "Control New Home", "CNH")
	newGuest := insertTeam(t, store.teams, "control-new-guest", "Control New Guest", "CNG")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newGuest.Name))

	edited := fb.EditedMessages()
	if len(edited) != 1 {
		t.Fatalf("expected one old-control edit, got %d", len(edited))
	}
	if edited[0].MessageID != 91 || !strings.Contains(edited[0].Text, "replaced") {
		t.Fatalf("unexpected old-control edit: %#v", edited[0])
	}
	kb, ok := edited[0].ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok || len(kb.InlineKeyboard) != 0 {
		t.Fatalf("expected empty old-control keyboard, got %#v", edited[0].ReplyMarkup)
	}
}

func TestPlanReplacementTransactionalFailurePreservesCurrentGame(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)
	r.games = &txFailGames{inner: store.games, failWithinTx: true}

	const userID int64 = 7116
	const chatID int64 = 8116
	store.createAdminUser(t, userID, "failed-replacement")
	game := createCurrentPlannedGame(t, store, userID)
	newHome := insertTeam(t, store.teams, "failed-new-home", "Failed New Home", "FNH")
	newGuest := insertTeam(t, store.teams, "failed-new-guest", "Failed New Guest", "FNG")

	confirmPlannedGameReplacement(r, userID, chatID)
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newHome.Name))
	r.handlePlanText(context.Background(), nil, makePlainTextUpdate(userID, chatID, newGuest.Name))

	got, err := store.games.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.HomeTeamID != game.HomeTeamID || got.GuestTeamID != game.GuestTeamID {
		t.Fatalf("game changed after transaction failure: got %d/%d, want %d/%d", got.HomeTeamID, got.GuestTeamID, game.HomeTeamID, game.GuestTeamID)
	}
	if renderer.plannedCount() != 0 || renderer.intermissionCount() != 0 {
		t.Fatal("expected no overlay render after transaction failure")
	}
	msgs := fb.SentMessages()
	if !strings.Contains(msgs[len(msgs)-1].Text, "state was not changed") {
		t.Fatalf("expected retry-safe failure message, got %q", msgs[len(msgs)-1].Text)
	}
}

func TestPlanTransactionalFailureSendsRetrySafeMessageAndSkipsOverlay(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, renderer := newPlanRouter(t, store)
	r.games = &txFailGames{inner: store.games, failCreate: true}
	ctx := context.Background()

	const userID int64 = 7103
	const chatID int64 = 8103
	store.createAdminUser(t, userID, "planner3")

	insertTeam(t, store.teams, "home3", "Home Three", "H3")
	insertTeam(t, store.teams, "guest3", "Guest Three", "G3")

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(userID, chatID, "Home Three"))
	r.handlePlanText(ctx, nil, makePlainTextUpdate(userID, chatID, "Guest Three"))

	current, err := store.games.GetCurrentGame()
	if err != nil {
		t.Fatalf("GetCurrentGame: %v", err)
	}
	if current != nil {
		t.Fatalf("expected no current game after transactional failure, got %d", current.ID)
	}
	if renderer.plannedCount() != 0 {
		t.Fatalf("expected no planned render after transactional failure, got %d", renderer.plannedCount())
	}
	if renderer.intermissionCount() != 0 {
		t.Fatalf("expected no intermission render after transactional failure, got %d", renderer.intermissionCount())
	}

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected failure message")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "state was not changed") {
		t.Fatalf("expected retry-safe failure message, got %q", last.Text)
	}
}
