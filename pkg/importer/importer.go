package importer

import (
	"errors"
	"fmt"

	"github.com/smith3v/dash14/pkg/storage"
)

// Importer orchestrates a full team import: it parses a YAML file, copies any
// referenced logo files into the managed logo directory, and upserts each team
// into the database.
type Importer struct {
	repo      *storage.TeamRepository
	logoStore *LogoStore
}

// NewImporter constructs an Importer backed by the given repository and logo
// store.
func NewImporter(repo *storage.TeamRepository, logoStore *LogoStore) *Importer {
	return &Importer{
		repo:      repo,
		logoStore: logoStore,
	}
}

// ImportTeams parses the YAML file at yamlPath, copies each team's logo (when
// present) into the managed logo directory, and upserts each team record in the
// database.
//
// Per-team errors (logo copy failures or database errors) are collected and
// returned together via errors.Join at the end so that a single bad record does
// not abort the import of the remaining teams. The caller can inspect whether
// errors.Join returned a non-nil value and log or surface individual failures.
func (imp *Importer) ImportTeams(yamlPath string) error {
	records, err := ParseTeamsYAML(yamlPath)
	if err != nil {
		return err
	}

	var errs []error

	for _, rec := range records {
		logoPath, err := imp.logoStore.CopyLogo(rec.Key, rec.Logo)
		if err != nil {
			errs = append(errs, fmt.Errorf("importer: team %q: copy logo: %w", rec.Key, err))
			// Continue so that other teams are still processed.
			logoPath = ""
		}

		team := &storage.Team{
			Key:       rec.Key,
			Name:      rec.Name,
			ShortName: rec.ShortName,
			LogoPath:  logoPath,
			Aliases:   rec.Aliases,
		}

		if err := imp.repo.UpsertTeam(team); err != nil {
			errs = append(errs, fmt.Errorf("importer: team %q: upsert: %w", rec.Key, err))
		}
	}

	return errors.Join(errs...)
}
