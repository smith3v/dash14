package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/smith3v/dash14/pkg/storage"
)

// TestOpen verifies that Open succeeds for a fresh database placed inside a
// subdirectory that does not yet exist, and that the returned *gorm.DB can
// execute a basic query.
func TestOpen(t *testing.T) {
	t.Run("creates_parent_dirs_and_opens", func(t *testing.T) {
		// Nest the database a couple of levels deep so we exercise MkdirAll.
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "sub", "dir", "dash14_test.db")

		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("Open(%q): unexpected error: %v", dbPath, err)
		}

		// Verify the connection is live with a trivial query.
		var result int
		if err := db.Raw("SELECT 1").Scan(&result).Error; err != nil {
			t.Fatalf("ping query failed: %v", err)
		}
		if result != 1 {
			t.Fatalf("ping query: got %d, want 1", result)
		}
	})

	t.Run("foreign_keys_enabled", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "dash14_fk.db")

		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("Open(%q): unexpected error: %v", dbPath, err)
		}

		// PRAGMA foreign_keys returns 1 when enabled.
		var fkOn int
		if err := db.Raw("PRAGMA foreign_keys").Scan(&fkOn).Error; err != nil {
			t.Fatalf("PRAGMA foreign_keys query failed: %v", err)
		}
		if fkOn != 1 {
			t.Fatalf("foreign_keys pragma: got %d, want 1", fkOn)
		}
	})

	t.Run("sqlite_runtime_pragmas_applied", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "dash14_pragmas.db")

		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("Open(%q): unexpected error: %v", dbPath, err)
		}

		var journalMode string
		if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
			t.Fatalf("PRAGMA journal_mode query failed: %v", err)
		}
		if journalMode != "wal" {
			t.Fatalf("journal_mode pragma: got %q, want %q", journalMode, "wal")
		}

		var synchronous int
		if err := db.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
			t.Fatalf("PRAGMA synchronous query failed: %v", err)
		}
		if synchronous != 1 {
			t.Fatalf("synchronous pragma: got %d, want %d", synchronous, 1)
		}

		var busyTimeout int
		if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
			t.Fatalf("PRAGMA busy_timeout query failed: %v", err)
		}
		if busyTimeout != 5000 {
			t.Fatalf("busy_timeout pragma: got %d, want %d", busyTimeout, 5000)
		}
	})
}

// TestMigrate verifies that Migrate returns nil on a fresh database.
func TestMigrate(t *testing.T) {
	t.Run("returns_nil_on_fresh_db", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "dash14_migrate.db")

		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("Open(%q): unexpected error: %v", dbPath, err)
		}

		if err := storage.Migrate(db); err != nil {
			t.Fatalf("Migrate: unexpected error: %v", err)
		}
	})

	t.Run("idempotent_on_second_call", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "dash14_migrate2.db")

		db, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("Open(%q): unexpected error: %v", dbPath, err)
		}

		if err := storage.Migrate(db); err != nil {
			t.Fatalf("first Migrate: unexpected error: %v", err)
		}
		if err := storage.Migrate(db); err != nil {
			t.Fatalf("second Migrate: unexpected error: %v", err)
		}
	})
}

func TestMigrateRemovesLegacyLifecycleState(t *testing.T) {
	db := openTestDB(t)
	for _, query := range []string{
		`ALTER TABLE games ADD COLUMN phase TEXT NOT NULL DEFAULT 'planned'`,
		`CREATE TABLE app_state (id INTEGER PRIMARY KEY, current_game_id INTEGER, updated_at DATETIME)`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}

	homeID, guestID := seedTeams(t, db)
	repo := storage.NewGameRepository(db)
	finished := &storage.Game{
		HomeTeamID: homeID, GuestTeamID: guestID, Status: storage.GameStatusFinished,
		HomeSetsWon: 3, GuestSetsWon: 1, CurrentSetNumber: 4,
	}
	current := &storage.Game{
		HomeTeamID: homeID, GuestTeamID: guestID, Status: storage.GameStatusInProgress,
		HomeSetsWon: 1, CurrentSetNumber: 2, CurrentAdminUserID: 42, ControlMessageID: 99,
	}
	for _, game := range []*storage.Game{finished, current} {
		if err := repo.CreateGame(game); err != nil {
			t.Fatalf("create legacy game: %v", err)
		}
	}
	if err := db.Exec(`INSERT INTO app_state (id, current_game_id) VALUES (1, ?)`, finished.ID).Error; err != nil {
		t.Fatalf("seed stale current game pointer: %v", err)
	}

	completedSet := &storage.GameSet{GameID: current.ID, SetNumber: 1, HomeScore: 25, GuestScore: 19, IsFinished: true}
	activeSet := &storage.GameSet{GameID: current.ID, SetNumber: 2, HomeScore: 12, GuestScore: 9}
	for _, set := range []*storage.GameSet{completedSet, activeSet} {
		if err := repo.CreateSet(set); err != nil {
			t.Fatalf("create legacy set: %v", err)
		}
	}

	for range 2 {
		if err := storage.Migrate(db); err != nil {
			t.Fatalf("migrate legacy database: %v", err)
		}
	}

	var phaseColumns int
	if err := db.Raw(`SELECT count(*) FROM pragma_table_info('games') WHERE name = 'phase'`).
		Row().Scan(&phaseColumns); err != nil || phaseColumns != 0 {
		t.Fatalf("games.phase column count = %d, %v, want 0, nil", phaseColumns, err)
	}
	if db.Migrator().HasTable("app_state") {
		t.Fatal("legacy app_state table still exists")
	}

	got, err := repo.GetCurrentGame()
	if err != nil || got == nil || got.ID != current.ID || got.CurrentSetNumber != current.CurrentSetNumber ||
		got.HomeSetsWon != current.HomeSetsWon || got.CurrentAdminUserID != current.CurrentAdminUserID ||
		got.ControlMessageID != current.ControlMessageID {
		t.Fatalf("current game after migration = %+v, %v, want %+v", got, err, current)
	}
	history, err := repo.GetGameByID(finished.ID)
	if err != nil || history.Status != finished.Status || history.HomeSetsWon != finished.HomeSetsWon ||
		history.GuestSetsWon != finished.GuestSetsWon {
		t.Fatalf("finished game after migration = %+v, %v, want %+v", history, err, finished)
	}
	set, err := repo.GetActiveSet(current.ID)
	if err != nil || set.ID != activeSet.ID || set.HomeScore != activeSet.HomeScore || set.GuestScore != activeSet.GuestScore {
		t.Fatalf("active set after migration = %+v, %v, want %+v", set, err, activeSet)
	}
	sets, err := repo.ListSetsByGameID(current.ID)
	if err != nil || len(sets) != 2 || sets[0].ID != completedSet.ID || !sets[0].IsFinished {
		t.Fatalf("set history after migration = %+v, %v, want completed and active sets", sets, err)
	}
	if phase := got.EffectivePhase(true); phase != storage.GamePhaseSetInProgress {
		t.Fatalf("phase after migration = %q, want set_in_progress", phase)
	}

	if err := repo.CreateGame(&storage.Game{HomeTeamID: homeID, GuestTeamID: guestID}); err == nil {
		t.Fatal("migration removed the single non-finished game constraint")
	}
}
