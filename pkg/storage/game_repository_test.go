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
		Phase:              storage.GamePhasePlanned,
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
	if got.Phase != storage.GamePhasePlanned {
		t.Errorf("Phase: got %q, want %q", got.Phase, storage.GamePhasePlanned)
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
		Phase:            storage.GamePhasePlanned,
		CurrentSetNumber: 1,
	}
	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	// Transition to in_progress and update set number.
	game.Status = storage.GameStatusInProgress
	game.Phase = storage.GamePhaseSetInProgress
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
	if got.Phase != storage.GamePhaseSetInProgress {
		t.Errorf("Phase: got %q, want %q", got.Phase, storage.GamePhaseSetInProgress)
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
		Phase:            storage.GamePhaseSetInProgress,
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

// TestGetCurrentGame verifies that current-game lookup follows the single
// non-finished game rather than the AppState pointer row.
func TestGetCurrentGame(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	t.Run("returns_nil_when_no_non_finished_game_exists", func(t *testing.T) {
		game, err := repo.GetCurrentGame()
		if err != nil {
			t.Fatalf("GetCurrentGame: %v", err)
		}
		if game != nil {
			t.Errorf("expected nil game, got game with ID %d", game.ID)
		}
	})

	t.Run("returns_non_finished_game_without_app_state_row", func(t *testing.T) {
		game := &storage.Game{
			HomeTeamID:       homeID,
			GuestTeamID:      guestID,
			HomeTeamSide:     "left",
			GuestTeamSide:    "right",
			Status:           storage.GameStatusPlanned,
			Phase:            storage.GamePhasePlanned,
			CurrentSetNumber: 1,
		}
		if err := repo.CreateGame(game); err != nil {
			t.Fatalf("CreateGame: %v", err)
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

		var count int64
		if err := db.Model(&storage.AppState{}).Count(&count).Error; err != nil {
			t.Fatalf("count AppState rows: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0 AppState rows, got %d", count)
		}
	})

	t.Run("returns_nil_when_all_games_are_finished", func(t *testing.T) {
		db := openTestDB(t)
		homeID, guestID := seedTeams(t, db)
		repo := storage.NewGameRepository(db)

		game := &storage.Game{
			HomeTeamID:       homeID,
			GuestTeamID:      guestID,
			HomeTeamSide:     "left",
			GuestTeamSide:    "right",
			Status:           storage.GameStatusFinished,
			Phase:            storage.GamePhaseFinished,
			CurrentSetNumber: 4,
		}
		if err := repo.CreateGame(game); err != nil {
			t.Fatalf("CreateGame: %v", err)
		}

		got, err := repo.GetCurrentGame()
		if err != nil {
			t.Fatalf("GetCurrentGame: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil current game, got %d", got.ID)
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
		Phase:            storage.GamePhaseFinished,
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
		Phase:            storage.GamePhasePlanned,
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

func TestCreateGameRejectsSecondNonFinishedGame(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	first := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusPlanned,
		Phase:            storage.GamePhasePlanned,
		CurrentSetNumber: 1,
	}
	if err := repo.CreateGame(first); err != nil {
		t.Fatalf("CreateGame(first): %v", err)
	}

	second := &storage.Game{
		HomeTeamID:       homeID,
		GuestTeamID:      guestID,
		HomeTeamSide:     "left",
		GuestTeamSide:    "right",
		Status:           storage.GameStatusInProgress,
		Phase:            storage.GamePhaseSetInProgress,
		CurrentSetNumber: 1,
	}
	err := repo.CreateGame(second)
	if err == nil {
		t.Fatal("expected second non-finished game creation to fail, got nil")
	}

	var count int64
	if err := db.Model(&storage.Game{}).
		Where("status <> ?", storage.GameStatusFinished).
		Count(&count).Error; err != nil {
		t.Fatalf("count non-finished games: %v", err)
	}
	if count != 1 {
		t.Fatalf("non-finished game count = %d, want 1", count)
	}
}

func TestWithinTxRollsBackOnError(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)

	errExpected := errors.New("force rollback")
	err := repo.WithinTx(func(txRepo *storage.GameRepository) error {
		game := &storage.Game{
			HomeTeamID:       homeID,
			GuestTeamID:      guestID,
			HomeTeamSide:     "left",
			GuestTeamSide:    "right",
			Status:           storage.GameStatusPlanned,
			Phase:            storage.GamePhasePlanned,
			CurrentSetNumber: 1,
		}
		if err := txRepo.CreateGame(game); err != nil {
			return err
		}
		return errExpected
	})
	if !errors.Is(err, errExpected) {
		t.Fatalf("WithinTx error = %v, want wrapped %v", err, errExpected)
	}

	var count int64
	if err := db.Model(&storage.Game{}).Count(&count).Error; err != nil {
		t.Fatalf("count games after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("game count after rollback = %d, want 0", count)
	}
}

func TestSaveGameInfersPhaseWhenMissing(t *testing.T) {
	t.Run("planned_status_defaults_to_planned_phase", func(t *testing.T) {
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
		if game.Phase != storage.GamePhasePlanned {
			t.Fatalf("CreateGame inferred phase = %q, want %q", game.Phase, storage.GamePhasePlanned)
		}
	})

	t.Run("in_progress_with_active_set_defaults_to_set_in_progress", func(t *testing.T) {
		db := openTestDB(t)
		homeID, guestID := seedTeams(t, db)
		repo := storage.NewGameRepository(db)

		game := &storage.Game{
			HomeTeamID:       homeID,
			GuestTeamID:      guestID,
			HomeTeamSide:     "left",
			GuestTeamSide:    "right",
			Status:           storage.GameStatusPlanned,
			Phase:            storage.GamePhasePlanned,
			CurrentSetNumber: 1,
		}
		if err := repo.CreateGame(game); err != nil {
			t.Fatalf("CreateGame: %v", err)
		}
		if err := repo.CreateSet(&storage.GameSet{
			GameID:     game.ID,
			SetNumber:  1,
			HomeScore:  4,
			GuestScore: 3,
			IsFinished: false,
		}); err != nil {
			t.Fatalf("CreateSet: %v", err)
		}

		game.Status = storage.GameStatusInProgress
		game.Phase = ""
		if err := repo.SaveGame(game); err != nil {
			t.Fatalf("SaveGame: %v", err)
		}

		got, err := repo.GetGameByID(game.ID)
		if err != nil {
			t.Fatalf("GetGameByID: %v", err)
		}
		if got.Phase != storage.GamePhaseSetInProgress {
			t.Fatalf("Phase: got %q, want %q", got.Phase, storage.GamePhaseSetInProgress)
		}
	})

	t.Run("in_progress_without_active_set_defaults_to_between_sets", func(t *testing.T) {
		db := openTestDB(t)
		homeID, guestID := seedTeams(t, db)
		repo := storage.NewGameRepository(db)

		game := &storage.Game{
			HomeTeamID:       homeID,
			GuestTeamID:      guestID,
			HomeTeamSide:     "left",
			GuestTeamSide:    "right",
			Status:           storage.GameStatusPlanned,
			Phase:            storage.GamePhasePlanned,
			CurrentSetNumber: 2,
		}
		if err := repo.CreateGame(game); err != nil {
			t.Fatalf("CreateGame: %v", err)
		}
		if err := repo.CreateSet(&storage.GameSet{
			GameID:     game.ID,
			SetNumber:  1,
			HomeScore:  25,
			GuestScore: 19,
			IsFinished: true,
		}); err != nil {
			t.Fatalf("CreateSet: %v", err)
		}

		game.Status = storage.GameStatusInProgress
		game.Phase = ""
		if err := repo.SaveGame(game); err != nil {
			t.Fatalf("SaveGame: %v", err)
		}

		got, err := repo.GetGameByID(game.ID)
		if err != nil {
			t.Fatalf("GetGameByID: %v", err)
		}
		if got.Phase != storage.GamePhaseBetweenSets {
			t.Fatalf("Phase: got %q, want %q", got.Phase, storage.GamePhaseBetweenSets)
		}
	})

	t.Run("finished_status_defaults_to_finished_phase", func(t *testing.T) {
		db := openTestDB(t)
		homeID, guestID := seedTeams(t, db)
		repo := storage.NewGameRepository(db)

		game := &storage.Game{
			HomeTeamID:       homeID,
			GuestTeamID:      guestID,
			HomeTeamSide:     "left",
			GuestTeamSide:    "right",
			Status:           storage.GameStatusPlanned,
			Phase:            storage.GamePhasePlanned,
			CurrentSetNumber: 4,
		}
		if err := repo.CreateGame(game); err != nil {
			t.Fatalf("CreateGame: %v", err)
		}

		game.Status = storage.GameStatusFinished
		game.Phase = ""
		if err := repo.SaveGame(game); err != nil {
			t.Fatalf("SaveGame: %v", err)
		}

		got, err := repo.GetGameByID(game.ID)
		if err != nil {
			t.Fatalf("GetGameByID: %v", err)
		}
		if got.Phase != storage.GamePhaseFinished {
			t.Fatalf("Phase: got %q, want %q", got.Phase, storage.GamePhaseFinished)
		}
	})
}

func TestGameEffectivePhaseFallsBackFromLegacyBlankPhase(t *testing.T) {
	tests := []struct {
		name         string
		game         storage.Game
		hasActiveSet bool
		want         storage.GamePhase
	}{
		{
			name: "stored phase wins",
			game: storage.Game{
				Status: storage.GameStatusInProgress,
				Phase:  storage.GamePhaseBetweenSets,
			},
			hasActiveSet: true,
			want:         storage.GamePhaseBetweenSets,
		},
		{
			name: "planned falls back to planned",
			game: storage.Game{
				Status: storage.GameStatusPlanned,
			},
			want: storage.GamePhasePlanned,
		},
		{
			name: "in progress with active set falls back to set in progress",
			game: storage.Game{
				Status: storage.GameStatusInProgress,
			},
			hasActiveSet: true,
			want:         storage.GamePhaseSetInProgress,
		},
		{
			name: "in progress without active set falls back to between sets",
			game: storage.Game{
				Status: storage.GameStatusInProgress,
			},
			want: storage.GamePhaseBetweenSets,
		},
		{
			name: "finished falls back to finished",
			game: storage.Game{
				Status: storage.GameStatusFinished,
			},
			want: storage.GamePhaseFinished,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.game.EffectivePhase(tc.hasActiveSet)
			if got != tc.want {
				t.Fatalf("EffectivePhase() = %q, want %q", got, tc.want)
			}
		})
	}
}
