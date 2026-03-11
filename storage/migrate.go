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
//   - Task 6: Game, GameSet
//   - Task 7: User
func Migrate(db *gorm.DB) error {
	// No models yet — placeholder list will be extended in Tasks 5-7.
	if err := db.AutoMigrate(); err != nil {
		return fmt.Errorf("storage: automigrate: %w", err)
	}
	return nil
}
