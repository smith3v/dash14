package storage

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameRepository provides persistence operations for Game, GameSet, and
// AppState records.
type GameRepository struct {
	db *gorm.DB
}

// NewGameRepository constructs a GameRepository backed by the given database.
func NewGameRepository(db *gorm.DB) *GameRepository {
	return &GameRepository{db: db}
}

// CreateGame inserts a new Game record and populates its ID on success.
func (r *GameRepository) CreateGame(game *Game) error {
	if err := r.db.Create(game).Error; err != nil {
		return fmt.Errorf("storage: create game: %w", err)
	}
	return nil
}

// CreateSet inserts a new GameSet record and populates its ID on success.
func (r *GameRepository) CreateSet(set *GameSet) error {
	if err := r.db.Create(set).Error; err != nil {
		return fmt.Errorf("storage: create set for game %d: %w", set.GameID, err)
	}
	return nil
}

// GetCurrentGame returns the game pointed to by AppState.CurrentGameID. It
// returns nil, nil when no current game is set (AppState row absent or
// CurrentGameID is nil).
func (r *GameRepository) GetCurrentGame() (*Game, error) {
	var state AppState
	err := r.db.First(&state, 1).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get app state: %w", err)
	}
	if state.CurrentGameID == nil {
		return nil, nil
	}
	return r.GetGameByID(*state.CurrentGameID)
}

// GetNonFinishedGame returns one game whose status is not finished.
// It returns nil, nil when no planned/in-progress game exists.
func (r *GameRepository) GetNonFinishedGame() (*Game, error) {
	var game Game
	err := r.db.
		Where("status <> ?", GameStatusFinished).
		Order("id DESC").
		First(&game).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get non-finished game: %w", err)
	}
	return &game, nil
}

// GetGameByID returns the game with the given ID. It returns a wrapped
// gorm.ErrRecordNotFound when no game exists with that ID.
func (r *GameRepository) GetGameByID(id uint) (*Game, error) {
	var game Game
	if err := r.db.First(&game, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("storage: game with id %d not found: %w", id, gorm.ErrRecordNotFound)
		}
		return nil, fmt.Errorf("storage: get game by id %d: %w", id, err)
	}
	return &game, nil
}

// GetActiveSet returns the GameSet for the given game where IsFinished is
// false. At any time there should be at most one such set. It returns a
// wrapped gorm.ErrRecordNotFound when no active set exists.
func (r *GameRepository) GetActiveSet(gameID uint) (*GameSet, error) {
	var set GameSet
	err := r.db.
		Where("game_id = ? AND is_finished = ?", gameID, false).
		First(&set).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("storage: no active set for game %d: %w", gameID, gorm.ErrRecordNotFound)
		}
		return nil, fmt.Errorf("storage: get active set for game %d: %w", gameID, err)
	}
	return &set, nil
}

// SaveGame persists all fields of the given game record. The game must already
// exist (have a non-zero ID).
func (r *GameRepository) SaveGame(game *Game) error {
	if err := r.db.Save(game).Error; err != nil {
		return fmt.Errorf("storage: save game %d: %w", game.ID, err)
	}
	return nil
}

// SaveSet persists all fields of the given set record. The set must already
// exist (have a non-zero ID).
func (r *GameRepository) SaveSet(set *GameSet) error {
	if err := r.db.Save(set).Error; err != nil {
		return fmt.Errorf("storage: save set %d: %w", set.ID, err)
	}
	return nil
}

// SetCurrentGameID upserts the singleton AppState row (ID=1) with the given
// game ID as the current game.
func (r *GameRepository) SetCurrentGameID(id uint) error {
	state := AppState{
		ID:            1,
		CurrentGameID: &id,
	}
	result := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"current_game_id", "updated_at"}),
	}).Create(&state)
	if result.Error != nil {
		return fmt.Errorf("storage: set current game id %d: %w", id, result.Error)
	}
	return nil
}

// ClearCurrentGameID upserts the singleton AppState row (ID=1) with
// CurrentGameID set to nil, indicating no active game.
func (r *GameRepository) ClearCurrentGameID() error {
	result := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"current_game_id", "updated_at"}),
	}).Create(&AppState{ID: 1, CurrentGameID: nil})
	if result.Error != nil {
		return fmt.Errorf("storage: clear current game id: %w", result.Error)
	}
	return nil
}
