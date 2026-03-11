package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/smith3v/dash14/storage"
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
