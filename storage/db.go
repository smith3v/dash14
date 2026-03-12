// Package storage handles database access, schema migrations, and persistence
// for dash14. It uses GORM with the pure-Go glebarez/sqlite driver so that no
// CGO toolchain is required at build time.
package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Open opens (or creates) the SQLite database at path, enables the
// foreign-key pragma, and returns a configured *gorm.DB ready for use.
// Parent directories are created automatically when they do not exist.
func Open(path string) (*gorm.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create database directory %q: %w", dir, err)
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("storage: open database %q: %w", path, err)
	}

	// Enable foreign-key enforcement. SQLite disables it by default.
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return nil, fmt.Errorf("storage: enable foreign_keys pragma: %w", err)
	}

	return db, nil
}
