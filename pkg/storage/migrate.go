package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// Migrate runs GORM AutoMigrate for all storage models, creating or updating
// tables as needed. It is safe to call on an already-migrated database.
//
// Models are registered here as they are introduced in subsequent tasks:
//   - Task 5: Team, AppState
//   - Task 6: User
//   - Task 7: Game, GameSet, AppState
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Team{},
		&User{},
		&Game{},
		&GameSet{},
		&AppState{},
	); err != nil {
		return fmt.Errorf("storage: automigrate: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_games_single_non_finished
		ON games ((1))
		WHERE status <> 'finished'
	`).Error; err != nil {
		return fmt.Errorf("storage: create single non-finished game index: %w", err)
	}
	return nil
}
