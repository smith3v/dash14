package storage_test

import (
	"errors"
	"testing"

	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

// seedTeams inserts two minimal teams into the database and returns their IDs.
// These are used as the HomeTeamID and GuestTeamID for test games.
func seedTeams(t *testing.T, db *gorm.DB) (homeID, guestID uint) {
	t.Helper()
	repo := storage.NewTeamRepository(db)

	home := &storage.Team{Key: "home-team", Name: "Home FC", ShortName: "HOM"}
	guest := &storage.Team{Key: "guest-team", Name: "Guest FC", ShortName: "GST"}

	if err := repo.UpsertTeam(home); err != nil {
		t.Fatalf("seedTeams: upsert home team: %v", err)
	}
	if err := repo.UpsertTeam(guest); err != nil {
		t.Fatalf("seedTeams: upsert guest team: %v", err)
	}
	return home.ID, guest.ID
}

// TestGameCreateAndGetByID verifies that CreateGame persists a new planned game
// and that GetGameByID retrieves it with all fields intact.
func TestGameCreateAndGetByID(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	game := &storage.Game{
		HomeTeamID:         homeID,
		GuestTeamID:        guestID,
		HomeTeamSide:       "left",
		GuestTeamSide:      "right",
		Status:             storage.GameStatusPlanned,
		CurrentSetNumber:   1,
		CurrentAdminUserID: 42,
	}

	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if game.ID == 0 {
		t.Fatal("expected game ID to be populated after CreateGame, got 0")
	}

	got, err := repo.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}

	if got.HomeTeamID != homeID {
		t.Errorf("HomeTeamID: got %d, want %d", got.HomeTeamID, homeID)
	}
	if got.GuestTeamID != guestID {
		t.Errorf("GuestTeamID: got %d, want %d", got.GuestTeamID, guestID)
	}
	if got.Status != storage.GameStatusPlanned {
		t.Errorf("Status: got %q, want %q", got.Status, storage.GameStatusPlanned)
	}
	if got.CurrentSetNumber != 1 {
		t.Errorf("CurrentSetNumber: got %d, want 1", got.CurrentSetNumber)
	}
	if got.HomeTeamSide != "left" {
		t.Errorf("HomeTeamSide: got %q, want %q", got.HomeTeamSide, "left")
	}
	if got.GuestTeamSide != "right" {
		t.Errorf("GuestTeamSide: got %q, want %q", got.GuestTeamSide, "right")
	}
	if got.CurrentAdminUserID != 42 {
		t.Errorf("CurrentAdminUserID: got %d, want 42", got.CurrentAdminUserID)
	}
}

// TestGameGetByIDNotFound verifies that GetGameByID returns a wrapped
// gorm.ErrRecordNotFound for a non-existent ID.
func TestGameGetByIDNotFound(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewGameRepository(db)

	_, err := repo.GetGameByID(99999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound in error chain, got: %v", err)
	}
}

// TestGameSaveUpdatesFields verifies that SaveGame persists mutations to an
// existing game row.
func TestGameSaveUpdatesFields(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	game := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		CurrentSetNumber: 1,
	}
	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	// Transition to in_progress and update set number.
	game.Status = storage.GameStatusInProgress
	game.CurrentSetNumber = 2
	game.HomeSetsWon = 1
	if err := repo.SaveGame(game); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	got, err := repo.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID after SaveGame: %v", err)
	}
	if got.Status != storage.GameStatusInProgress {
		t.Errorf("Status: got %q, want %q", got.Status, storage.GameStatusInProgress)
	}
	if got.CurrentSetNumber != 2 {
		t.Errorf("CurrentSetNumber: got %d, want 2", got.CurrentSetNumber)
	}
	if got.HomeSetsWon != 1 {
		t.Errorf("HomeSetsWon: got %d, want 1", got.HomeSetsWon)
	}
}

// TestGetActiveSet verifies that GetActiveSet returns the set where
// IsFinished=false and correctly returns gorm.ErrRecordNotFound when all sets
// for a game are finished.
func TestGetActiveSet(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	game := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusInProgress,
		CurrentSetNumber: 1,
	}
	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	// Create set 1 as finished and set 2 as active.
	set1 := &storage.GameSet{
		GameID:     game.ID,
		SetNumber:  1,
		HomeScore:  25,
		GuestScore: 20,
		IsFinished: true,
	}
	set2 := &storage.GameSet{
		GameID:     game.ID,
		SetNumber:  2,
		HomeScore:  10,
		GuestScore: 8,
		IsFinished: false,
	}
	if err := repo.CreateSet(set1); err != nil {
		t.Fatalf("CreateSet (set1): %v", err)
	}
	if err := repo.CreateSet(set2); err != nil {
		t.Fatalf("CreateSet (set2): %v", err)
	}

	t.Run("returns_unfinished_set", func(t *testing.T) {
		active, err := repo.GetActiveSet(game.ID)
		if err != nil {
			t.Fatalf("GetActiveSet: %v", err)
		}
		if active.ID != set2.ID {
			t.Errorf("ID: got %d, want %d (set2)", active.ID, set2.ID)
		}
		if active.SetNumber != 2 {
			t.Errorf("SetNumber: got %d, want 2", active.SetNumber)
		}
		if active.IsFinished {
			t.Error("IsFinished: expected false, got true")
		}
	})

	t.Run("not_found_when_all_finished", func(t *testing.T) {
		// Finish set2.
		set2.IsFinished = true
		if err := repo.SaveSet(set2); err != nil {
			t.Fatalf("SaveSet: %v", err)
		}

		_, err := repo.GetActiveSet(game.ID)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("expected gorm.ErrRecordNotFound in error chain, got: %v", err)
		}
	})
}

// TestAppStateSingleton covers the three AppState scenarios: SetCurrentGameID,
// GetCurrentGame, and ClearCurrentGameID.
func TestAppStateSingleton(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	t.Run("get_current_game_returns_nil_when_no_state", func(t *testing.T) {
		game, err := repo.GetCurrentGame()
		if err != nil {
			t.Fatalf("GetCurrentGame with no AppState row: %v", err)
		}
		if game != nil {
			t.Errorf("expected nil game, got game with ID %d", game.ID)
		}
	})

	// Create a game to point at.
	game := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		CurrentSetNumber: 1,
	}
	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	t.Run("set_and_get_current_game", func(t *testing.T) {
		if err := repo.SetCurrentGameID(game.ID); err != nil {
			t.Fatalf("SetCurrentGameID: %v", err)
		}

		got, err := repo.GetCurrentGame()
		if err != nil {
			t.Fatalf("GetCurrentGame: %v", err)
		}
		if got == nil {
			t.Fatal("GetCurrentGame: got nil, want non-nil game")
		}
		if got.ID != game.ID {
			t.Errorf("GetCurrentGame ID: got %d, want %d", got.ID, game.ID)
		}
	})

	t.Run("set_is_idempotent_upsert", func(t *testing.T) {
		// Calling SetCurrentGameID twice must not create a second row.
		if err := repo.SetCurrentGameID(game.ID); err != nil {
			t.Fatalf("second SetCurrentGameID: %v", err)
		}

		var count int64
		db.Model(&storage.AppState{}).Count(&count)
		if count != 1 {
			t.Errorf("expected exactly 1 AppState row, got %d", count)
		}
	})

	t.Run("clear_current_game_id", func(t *testing.T) {
		if err := repo.ClearCurrentGameID(); err != nil {
			t.Fatalf("ClearCurrentGameID: %v", err)
		}

		got, err := repo.GetCurrentGame()
		if err != nil {
			t.Fatalf("GetCurrentGame after clear: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil after ClearCurrentGameID, got game ID %d", got.ID)
		}

		// Confirm the singleton row still exists (just with nil CurrentGameID).
		var count int64
		db.Model(&storage.AppState{}).Count(&count)
		if count != 1 {
			t.Errorf("expected exactly 1 AppState row after clear, got %d", count)
		}
	})

	t.Run("set_after_clear_works", func(t *testing.T) {
		// Re-setting after a clear must work correctly.
		if err := repo.SetCurrentGameID(game.ID); err != nil {
			t.Fatalf("SetCurrentGameID after clear: %v", err)
		}

		got, err := repo.GetCurrentGame()
		if err != nil {
			t.Fatalf("GetCurrentGame after re-set: %v", err)
		}
		if got == nil || got.ID != game.ID {
			t.Errorf("expected game ID %d, got %v", game.ID, got)
		}
	})
}

// TestGetNonFinishedGame verifies that GetNonFinishedGame returns nil when
// only finished games exist and returns a planned/in-progress game otherwise.
func TestGetNonFinishedGame(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	t.Run("returns_nil_when_no_rows", func(t *testing.T) {
		got, err := repo.GetNonFinishedGame()
		if err != nil {
			t.Fatalf("GetNonFinishedGame: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil game, got id=%d", got.ID)
		}
	})

	finished := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusFinished,
		CurrentSetNumber: 3,
	}
	if err := repo.CreateGame(finished); err != nil {
		t.Fatalf("CreateGame finished: %v", err)
	}

	t.Run("returns_nil_when_only_finished_exists", func(t *testing.T) {
		got, err := repo.GetNonFinishedGame()
		if err != nil {
			t.Fatalf("GetNonFinishedGame: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil game, got id=%d", got.ID)
		}
	})

	planned := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		CurrentSetNumber: 1,
	}
	if err := repo.CreateGame(planned); err != nil {
		t.Fatalf("CreateGame planned: %v", err)
	}

	t.Run("returns_non_finished_game", func(t *testing.T) {
		got, err := repo.GetNonFinishedGame()
		if err != nil {
			t.Fatalf("GetNonFinishedGame: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil game")
		}
		if got.ID != planned.ID {
			t.Fatalf("expected planned game id=%d, got %d", planned.ID, got.ID)
		}
	})
}
