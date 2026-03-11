package storage

import "time"

// AppState holds the singleton application state row. There is always exactly
// one row in this table, with ID=1. It tracks which game is currently active
// so that all components can locate the live match without a broadcast.
type AppState struct {
	// ID is always 1. The singleton pattern is enforced by the repository
	// methods, which upsert only with ID=1.
	ID uint `gorm:"primarykey"`

	// CurrentGameID points to the active game, or nil when no game is running.
	CurrentGameID *uint

	UpdatedAt time.Time
}
