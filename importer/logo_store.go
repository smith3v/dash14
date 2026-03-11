package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LogoStore manages the on-disk directory where team logo files are kept.
// Logos are stored under a single flat directory with stable filenames derived
// from the team key and the original file extension, so re-importing a team
// with a changed source path simply overwrites the existing file.
type LogoStore struct {
	logoDir string
}

// NewLogoStore returns a LogoStore that keeps logos in logoDir.
// The directory is created on first use (inside CopyLogo), not here.
func NewLogoStore(logoDir string) *LogoStore {
	return &LogoStore{logoDir: logoDir}
}

// CopyLogo copies the source logo at sourcePath into the managed logo
// directory. The destination filename is stable: {teamKey}{ext}, where ext is
// the file extension of sourcePath (including the leading dot). The method
// returns the relative path (filename only) that should be stored in the
// database.
//
// If sourcePath is empty, CopyLogo returns ("", nil) without touching the
// filesystem.
func (s *LogoStore) CopyLogo(teamKey, sourcePath string) (string, error) {
	if sourcePath == "" {
		return "", nil
	}

	ext := filepath.Ext(sourcePath)
	destFilename := teamKey + ext
	destPath := filepath.Join(s.logoDir, destFilename)

	if err := os.MkdirAll(s.logoDir, 0o750); err != nil {
		return "", fmt.Errorf("importer: create logo directory %q: %w", s.logoDir, err)
	}

	src, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("importer: open source logo %q: %w", sourcePath, err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("importer: create dest logo %q: %w", destPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("importer: copy logo to %q: %w", destPath, err)
	}

	return destFilename, nil
}
