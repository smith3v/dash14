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
	return seedTeam(t, db, "home-team", "Home FC", "HOM"),
		seedTeam(t, db, "guest-team", "Guest FC", "GST")
}

func seedTeam(t *testing.T, db *gorm.DB, key, name, shortName string) uint {
	t.Helper()
	team := &storage.Team{Key: key, Name: name, ShortName: shortName}
	if err := storage.NewTeamRepository(db).UpsertTeam(team); err != nil {
		t.Fatalf("seedTeam %q: %v", key, err)
	}
	return team.ID
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

func TestReplacePlannedGameUpdatesExpectedRow(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	newHomeID := seedTeam(t, db, "new-home", "New Home", "NH")
	newGuestID := seedTeam(t, db, "new-guest", "New Guest", "NG")
	repo := storage.NewGameRepository(db)

	game := &storage.Game{
		HomeTeamID:         homeID,
		GuestTeamID:        guestID,
		HomeTeamSide:       "right",
		GuestTeamSide:      "left",
		HomeSetsWon:        2,
		GuestSetsWon:       1,
		CurrentSetNumber:   5,
		Status:             storage.GameStatusPlanned,
		Phase:              storage.GamePhasePlanned,
		CurrentAdminUserID: 11,
		ControlMessageID:   99,
		SideSwitchedInSet5: true,
	}
	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	if err := repo.ReplacePlannedGame(
		game.ID,
		homeID,
		guestID,
		newHomeID,
		newGuestID,
		42,
	); err != nil {
		t.Fatalf("ReplacePlannedGame: %v", err)
	}

	got, err := repo.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.ID != game.ID {
		t.Fatalf("ID: got %d, want %d", got.ID, game.ID)
	}
	if got.HomeTeamID != newHomeID || got.GuestTeamID != newGuestID {
		t.Fatalf("teams: got %d vs %d, want %d vs %d", got.HomeTeamID, got.GuestTeamID, newHomeID, newGuestID)
	}
	if got.HomeTeamSide != "left" || got.GuestTeamSide != "right" {
		t.Fatalf("sides: got %q/%q, want left/right", got.HomeTeamSide, got.GuestTeamSide)
	}
	if got.HomeSetsWon != 0 || got.GuestSetsWon != 0 {
		t.Fatalf("sets won: got %d-%d, want 0-0", got.HomeSetsWon, got.GuestSetsWon)
	}
	if got.CurrentSetNumber != 1 {
		t.Fatalf("CurrentSetNumber: got %d, want 1", got.CurrentSetNumber)
	}
	if got.Status != storage.GameStatusPlanned || got.Phase != storage.GamePhasePlanned {
		t.Fatalf("lifecycle: got %q/%q, want planned/planned", got.Status, got.Phase)
	}
	if got.CurrentAdminUserID != 42 {
		t.Fatalf("CurrentAdminUserID: got %d, want 42", got.CurrentAdminUserID)
	}
	if got.ControlMessageID != 0 {
		t.Fatalf("ControlMessageID: got %d, want 0", got.ControlMessageID)
	}
	if got.SideSwitchedInSet5 {
		t.Fatal("SideSwitchedInSet5: got true, want false")
	}

	var count int64
	if err := db.Model(&storage.Game{}).Count(&count).Error; err != nil {
		t.Fatalf("count games: %v", err)
	}
	if count != 1 {
		t.Fatalf("game count: got %d, want 1", count)
	}
}

func TestReplacePlannedGameRejectsChangedGame(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *testing.T, repo *storage.GameRepository, game *storage.Game)
		callID  func(gameID uint) uint
		homeID  func(homeID, guestID uint) uint
		guestID func(homeID, guestID uint) uint
	}{
		{
			name: "started game",
			mutate: func(t *testing.T, repo *storage.GameRepository, game *storage.Game) {
				t.Helper()
				game.Status = storage.GameStatusInProgress
				game.Phase = storage.GamePhaseBetweenSets
				if err := repo.SaveGame(game); err != nil {
					t.Fatalf("SaveGame: %v", err)
				}
			},
		},
		{
			name:   "wrong expected home",
			homeID: func(_, guestID uint) uint { return guestID },
		},
		{
			name:    "wrong expected guest",
			guestID: func(homeID, _ uint) uint { return homeID },
		},
		{
			name:   "wrong game id",
			callID: func(gameID uint) uint { return gameID + 100 },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)
			homeID, guestID := seedTeams(t, db)
			newHomeID := seedTeam(t, db, "new-home", "New Home", "NH")
			newGuestID := seedTeam(t, db, "new-guest", "New Guest", "NG")
			repo := storage.NewGameRepository(db)

			game := &storage.Game{
				HomeTeamID:         homeID,
				GuestTeamID:        guestID,
				HomeTeamSide:       "left",
				GuestTeamSide:      "right",
				CurrentSetNumber:   1,
				Status:             storage.GameStatusPlanned,
				Phase:              storage.GamePhasePlanned,
				CurrentAdminUserID: 7,
				ControlMessageID:   55,
			}
			if err := repo.CreateGame(game); err != nil {
				t.Fatalf("CreateGame: %v", err)
			}
			if tc.mutate != nil {
				tc.mutate(t, repo, game)
			}

			callID := game.ID
			if tc.callID != nil {
				callID = tc.callID(game.ID)
			}
			expectedHomeID := homeID
			if tc.homeID != nil {
				expectedHomeID = tc.homeID(homeID, guestID)
			}
			expectedGuestID := guestID
			if tc.guestID != nil {
				expectedGuestID = tc.guestID(homeID, guestID)
			}

			err := repo.ReplacePlannedGame(
				callID,
				expectedHomeID,
				expectedGuestID,
				newHomeID,
				newGuestID,
				42,
			)
			if !errors.Is(err, storage.ErrPlannedGameChanged) {
				t.Fatalf("ReplacePlannedGame error = %v, want ErrPlannedGameChanged", err)
			}

			got, getErr := repo.GetGameByID(game.ID)
			if getErr != nil {
				t.Fatalf("GetGameByID: %v", getErr)
			}
			if got.HomeTeamID != homeID || got.GuestTeamID != guestID {
				t.Fatalf("teams changed after conflict: got %d vs %d, want %d vs %d", got.HomeTeamID, got.GuestTeamID, homeID, guestID)
			}
			if got.CurrentAdminUserID != 7 || got.ControlMessageID != 55 {
				t.Fatalf("control metadata changed after conflict: admin=%d message=%d", got.CurrentAdminUserID, got.ControlMessageID)
			}
		})
	}
}

func TestReplacePlannedGameRollsBackWithTransaction(t *testing.T) {
	db := openTestDB(t)
	homeID, guestID := seedTeams(t, db)
	newHomeID := seedTeam(t, db, "new-home", "New Home", "NH")
	newGuestID := seedTeam(t, db, "new-guest", "New Guest", "NG")
	repo := storage.NewGameRepository(db)
	game := &storage.Game{
		HomeTeamID:         homeID,
		GuestTeamID:        guestID,
		HomeTeamSide:       "left",
		GuestTeamSide:      "right",
		CurrentSetNumber:   1,
		Status:             storage.GameStatusPlanned,
		Phase:              storage.GamePhasePlanned,
		CurrentAdminUserID: 7,
		ControlMessageID:   55,
	}
	if err := repo.CreateGame(game); err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	errRollback := errors.New("force replacement rollback")
	err := repo.WithinTx(func(txRepo *storage.GameRepository) error {
		if err := txRepo.ReplacePlannedGame(
			game.ID,
			homeID,
			guestID,
			newHomeID,
			newGuestID,
			42,
		); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("WithinTx error = %v, want %v", err, errRollback)
	}

	got, err := repo.GetGameByID(game.ID)
	if err != nil {
		t.Fatalf("GetGameByID: %v", err)
	}
	if got.HomeTeamID != homeID || got.GuestTeamID != guestID {
		t.Fatalf("teams after rollback: got %d vs %d, want %d vs %d", got.HomeTeamID, got.GuestTeamID, homeID, guestID)
	}
	if got.CurrentAdminUserID != 7 || got.ControlMessageID != 55 {
		t.Fatalf("control metadata after rollback: admin=%d message=%d", got.CurrentAdminUserID, got.ControlMessageID)
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

func TestGameEffectivePhaseAlwaysDerivesFromStatusAndActiveSet(t *testing.T) {
	tests := []struct {
		name         string
		game         storage.Game
		hasActiveSet bool
		want         storage.GamePhase
	}{
		{
			name: "ignores stale stored phase",
			game: storage.Game{
				Status: storage.GameStatusInProgress,
				Phase:  storage.GamePhaseBetweenSets,
			},
			hasActiveSet: true,
			want:         storage.GamePhaseSetInProgress,
		},
		{
			name: "planned derives to planned",
			game: storage.Game{
				Status: storage.GameStatusPlanned,
			},
			want: storage.GamePhasePlanned,
		},
		{
			name: "in progress with active set derives to set in progress",
			game: storage.Game{
				Status: storage.GameStatusInProgress,
			},
			hasActiveSet: true,
			want:         storage.GamePhaseSetInProgress,
		},
		{
			name: "in progress without active set derives to between sets",
			game: storage.Game{
				Status: storage.GameStatusInProgress,
			},
			want: storage.GamePhaseBetweenSets,
		},
		{
			name: "finished derives to finished",
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
