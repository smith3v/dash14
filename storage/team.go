package storage

import "time"

// Team represents a volleyball team that can be selected for a match. Aliases
// are stored as a JSON array so the import and fuzzy-matching round-trip cleanly
// without requiring a separate join table.
type Team struct {
	ID        uint   `gorm:"primarykey"`
	Key       string `gorm:"uniqueIndex;not null"`
	Name      string `gorm:"not null"`
	ShortName string `gorm:"not null"`
	LogoPath  string
	Aliases   []string `gorm:"serializer:json"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
