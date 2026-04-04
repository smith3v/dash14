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

// TestPlanRejectedWhenNonFinishedGameExists verifies /plan is blocked when
// there is already a planned or in-progress game.
func TestPlanRejectedWhenNonFinishedGameExists(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	ctx := context.Background()

	const userID int64 = 7102
	const chatID int64 = 8102
	store.createAdminUser(t, userID, "planner2")

	home := insertTeam(t, store.teams, "home2", "Home Two", "H2")
	guest := insertTeam(t, store.teams, "guest2", "Guest Two", "G2")
	existing := &storage.Game{
		HomeTeamID:       home.ID,
		GuestTeamID:      guest.ID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		Phase:            storage.GamePhasePlanned,
		CurrentSetNumber: 1,
	}
	if err := store.games.CreateGame(existing); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	r.handlePlan(ctx, nil, makeTextUpdate(userID, chatID, "/plan"))

	msgs := fb.SentMessages()
	if len(msgs) == 0 {
		t.Fatal("expected rejection message")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Text, "another game is still planned or in progress") {
		t.Fatalf("unexpected rejection message: %q", last.Text)
	}
}
