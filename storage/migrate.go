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
//   - Task 7: Game, GameSet
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Team{},
		&User{},
	); err != nil {
		return fmt.Errorf("storage: automigrate: %w", err)
	}
	return nil
}
