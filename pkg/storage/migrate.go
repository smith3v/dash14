package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// Migrate creates or updates the storage schema. It is safe to call on an
// already-migrated database.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&Team{}, &User{}, &Game{}, &GameSet{}); err != nil {
		return fmt.Errorf("storage: automigrate: %w", err)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_games_single_non_finished
			ON games ((1))
			WHERE status <> 'finished'
		`).Error; err != nil {
			return fmt.Errorf("storage: create single non-finished game index: %w", err)
		}

		var phaseColumns int
		if err := tx.Raw(`SELECT count(*) FROM pragma_table_info('games') WHERE name = 'phase'`).
			Row().Scan(&phaseColumns); err != nil {
			return fmt.Errorf("storage: inspect games schema: %w", err)
		}
		if phaseColumns != 0 {
			// SQLite's native DROP COLUMN preserves the partial index on status.
			if err := tx.Exec(`ALTER TABLE games DROP COLUMN phase`).Error; err != nil {
				return fmt.Errorf("storage: drop obsolete games.phase: %w", err)
			}
		}

		if err := tx.Exec(`DROP TABLE IF EXISTS app_state`).Error; err != nil {
			return fmt.Errorf("storage: drop obsolete app_state table: %w", err)
		}
		return nil
	})
}
