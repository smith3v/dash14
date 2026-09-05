package storage

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrPlannedGameChanged indicates that a guarded planned-game replacement no
// longer matches the persisted game.
var ErrPlannedGameChanged = errors.New("storage: planned game changed")

// GameRepository provides persistence operations for Game, GameSet, and
// AppState records.
type GameRepository struct {
	db *gorm.DB
}

// NewGameRepository constructs a GameRepository backed by the given database.
func NewGameRepository(db *gorm.DB) *GameRepository {
	return &GameRepository{db: db}
}

// WithinTx runs fn inside a single database transaction using a repository
// backed by the transactional DB handle.
func (r *GameRepository) WithinTx(fn func(repo *GameRepository) error) error {
	if err := r.db.Transaction(func(tx *gorm.DB) error {
		return fn(NewGameRepository(tx))
	}); err != nil {
		return fmt.Errorf("storage: transaction failed: %w", err)
	}
	return nil
}

// CreateGame inserts a new Game record and populates its ID on success.
func (r *GameRepository) CreateGame(game *Game) error {
	if game.Phase == "" {
		game.Phase = DeriveGamePhase(game.Status, false)
	}
	if err := r.db.Create(game).Error; err != nil {
		return fmt.Errorf("storage: create game: %w", err)
	}
	return nil
}

// ReplacePlannedGame atomically replaces both teams on an existing planned
// game when its identity and original teams still match the caller's snapshot.
func (r *GameRepository) ReplacePlannedGame(
	gameID uint,
	expectedHomeTeamID uint,
	expectedGuestTeamID uint,
	newHomeTeamID uint,
	newGuestTeamID uint,
	adminUserID int64,
) error {
	result := r.db.
		Model(&Game{}).
		Where(
			"id = ? AND status = ? AND home_team_id = ? AND guest_team_id = ?",
			gameID,
			GameStatusPlanned,
			expectedHomeTeamID,
			expectedGuestTeamID,
		).
		Updates(map[string]any{
			"home_team_id":          newHomeTeamID,
			"guest_team_id":         newGuestTeamID,
			"home_team_side":        "left",
			"guest_team_side":       "right",
			"home_sets_won":         0,
			"guest_sets_won":        0,
			"current_set_number":    1,
			"status":                GameStatusPlanned,
			"phase":                 GamePhasePlanned,
			"current_admin_user_id": adminUserID,
			"control_message_id":    0,
			"side_switched_in_set5": false,
		})
	if result.Error != nil {
		return fmt.Errorf("storage: replace planned game %d: %w", gameID, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf(
			"%w: game %d no longer matches the expected teams",
			ErrPlannedGameChanged,
			gameID,
		)
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

// GetCurrentGame returns the single non-finished game. It returns nil, nil
// when no planned/in-progress game exists.
func (r *GameRepository) GetCurrentGame() (*Game, error) {
	var game Game
	err := r.db.
		Where("status <> ?", GameStatusFinished).
		Order("id DESC").
		First(&game).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get current game: %w", err)
	}
	return &game, nil
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

// ListSetsByGameID returns all sets for the given game ordered by set number.
func (r *GameRepository) ListSetsByGameID(gameID uint) ([]GameSet, error) {
	var sets []GameSet
	if err := r.db.
		Where("game_id = ?", gameID).
		Order("set_number ASC").
		Find(&sets).Error; err != nil {
		return nil, fmt.Errorf("storage: list sets for game %d: %w", gameID, err)
	}
	return sets, nil
}

// SaveGame persists all fields of the given game record. The game must already
// exist (have a non-zero ID).
func (r *GameRepository) SaveGame(game *Game) error {
	if game.Phase == "" {
		hasActiveSet, err := r.hasActiveSet(game.ID)
		if err != nil {
			return err
		}
		game.Phase = DeriveGamePhase(game.Status, hasActiveSet)
	}
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

func (r *GameRepository) hasActiveSet(gameID uint) (bool, error) {
	if gameID == 0 {
		return false, nil
	}

	var count int64
	if err := r.db.Model(&GameSet{}).
		Where("game_id = ? AND is_finished = ?", gameID, false).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("storage: count active sets for game %d: %w", gameID, err)
	}
	return count > 0, nil
}
