package storage

import "time"

// User represents a Telegram user who has interacted with the bot. The
// TelegramUserID is used as the primary key because Telegram IDs are stable
// unique identifiers and eliminate the need for a surrogate key.
type User struct {
	TelegramUserID int64  `gorm:"primaryKey"`
	Username       string `gorm:"not null;default:''"`
	Subscribed     bool   `gorm:"not null;default:false"`
	IsAdmin        bool   `gorm:"not null;default:false"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
