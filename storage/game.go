package storage

import "time"

// GameStatus represents the lifecycle state of a match.
type GameStatus string

const (
	// GameStatusPlanned indicates the game has been created but not yet started.
	GameStatusPlanned GameStatus = "planned"
	// GameStatusInProgress indicates the game is currently being played.
	GameStatusInProgress GameStatus = "in_progress"
	// GameStatusFinished indicates the game has concluded.
	GameStatusFinished GameStatus = "finished"
)

// Game represents a volleyball match between two teams. It records aggregate
// match state: which teams are playing, how many sets each has won, which set
// is current, and administrative metadata for the Telegram control panel.
type Game struct {
	ID uint `gorm:"primarykey"`

	// HomeTeamID and GuestTeamID reference the teams table.
	HomeTeamID  uint `gorm:"not null"`
	GuestTeamID uint `gorm:"not null"`

	// HomeTeamSide and GuestTeamSide track which side of the overlay each team
	// occupies. Valid values are "left" and "right".
	HomeTeamSide  string `gorm:"not null;default:'left'"`
	GuestTeamSide string `gorm:"not null;default:'right'"`

	// HomeSetsWon and GuestSetsWon are the number of sets won by each team.
	HomeSetsWon  int `gorm:"not null;default:0"`
	GuestSetsWon int `gorm:"not null;default:0"`

	// CurrentSetNumber is the 1-based index of the active set (1–5).
	CurrentSetNumber int `gorm:"not null;default:1"`

	// Status is the lifecycle state of the game.
	Status GameStatus `gorm:"not null;default:'planned'"`

	// CurrentAdminUserID is the Telegram user ID of the admin currently
	// managing this game. Zero means no admin is assigned.
	CurrentAdminUserID int64 `gorm:"not null;default:0"`

	// ControlMessageID is the Telegram message ID of the current inline control
	// panel message. Zero means no control message is active.
	ControlMessageID int `gorm:"not null;default:0"`

	// SideSwitchedInSet5 tracks whether the side switch in the fifth set has
	// already been applied (it may happen at most once per match).
	SideSwitchedInSet5 bool `gorm:"not null;default:false"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// GameSet records the score state of a single set within a match.
type GameSet struct {
	ID uint `gorm:"primarykey"`

	// GameID references the parent game.
	GameID uint `gorm:"not null;index"`

	// SetNumber is the 1-based index of this set within the match (1–5).
	SetNumber int `gorm:"not null"`

	// HomeScore and GuestScore are the current point tallies for each team.
	HomeScore  int `gorm:"not null;default:0"`
	GuestScore int `gorm:"not null;default:0"`

	// IsFinished is true once an admin has confirmed the set is over.
	IsFinished bool `gorm:"not null;default:false"`

	CreatedAt time.Time
	UpdatedAt time.Time
}
