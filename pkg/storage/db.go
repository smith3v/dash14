// Package storage handles database access, schema migrations, and persistence
// for dash14. It uses GORM with the pure-Go glebarez/sqlite driver so that no
// CGO toolchain is required at build time.
package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Open opens (or creates) the SQLite database at path, enables the
// foreign-key pragma, and returns a configured *gorm.DB ready for use.
// Parent directories are created automatically when they do not exist.
func Open(path string) (*gorm.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create database directory %q: %w", dir, err)
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "", log.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: open database %q: %w", path, err)
	}

	pragmas := []struct {
		sql     string
		context string
	}{
		{
			sql:     "PRAGMA foreign_keys = ON",
			context: "enable foreign_keys pragma",
		},
		{
			sql:     "PRAGMA journal_mode = WAL",
			context: "set journal_mode pragma",
		},
		{
			sql:     "PRAGMA synchronous = NORMAL",
			context: "set synchronous pragma",
		},
		{
			sql:     "PRAGMA busy_timeout = 5000",
			context: "set busy_timeout pragma",
		},
	}
	for _, pragma := range pragmas {
		if err := db.Exec(pragma.sql).Error; err != nil {
			return nil, fmt.Errorf("storage: %s: %w", pragma.context, err)
		}
	}

	return db, nil
}
