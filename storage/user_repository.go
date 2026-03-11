package storage

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserRepository provides persistence operations for User records.
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository constructs a UserRepository backed by the given database.
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// UpsertTelegramUser inserts a new user or updates the Username field for an
// existing user matched by TelegramUserID. On first insert the Subscribed flag
// is set to true so that the user immediately receives broadcasts after /start.
// Subsequent calls update Username only, leaving Subscribed and IsAdmin intact.
func (r *UserRepository) UpsertTelegramUser(telegramUserID int64, username string) error {
	user := User{
		TelegramUserID: telegramUserID,
		Username:       username,
		Subscribed:     true,
	}
	result := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "telegram_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username", "updated_at"}),
	}).Create(&user)
	if result.Error != nil {
		return fmt.Errorf("storage: upsert user %d: %w", telegramUserID, result.Error)
	}
	return nil
}

// SetSubscription sets the Subscribed field for the user with the given
// TelegramUserID. It returns gorm.ErrRecordNotFound (wrapped) when no user
// with that ID exists.
func (r *UserRepository) SetSubscription(telegramUserID int64, subscribed bool) error {
	result := r.db.Model(&User{}).
		Where("telegram_user_id = ?", telegramUserID).
		Update("subscribed", subscribed)
	if result.Error != nil {
		return fmt.Errorf("storage: set subscription for user %d: %w", telegramUserID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("storage: user %d not found: %w", telegramUserID, gorm.ErrRecordNotFound)
	}
	return nil
}

// GetUserByTelegramID returns the user with the given TelegramUserID. It
// returns a wrapped gorm.ErrRecordNotFound when no such user exists.
func (r *UserRepository) GetUserByTelegramID(telegramUserID int64) (*User, error) {
	var user User
	if err := r.db.Where("telegram_user_id = ?", telegramUserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("storage: user %d not found: %w", telegramUserID, gorm.ErrRecordNotFound)
		}
		return nil, fmt.Errorf("storage: get user by telegram id %d: %w", telegramUserID, err)
	}
	return &user, nil
}

// ListSubscribedUsers returns all users whose Subscribed field is true.
func (r *UserRepository) ListSubscribedUsers() ([]User, error) {
	var users []User
	if err := r.db.Where("subscribed = ?", true).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("storage: list subscribed users: %w", err)
	}
	return users, nil
}
