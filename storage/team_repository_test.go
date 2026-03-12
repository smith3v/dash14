package storage_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/smith3v/dash14/storage"
	"gorm.io/gorm"
)

// openTestDB opens a fresh SQLite database in a temp directory and runs all
// migrations. Each call produces an isolated database.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// TestTeamUpsertByKey verifies that UpsertTeam creates a team on first call
// and overwrites all mutable fields on a subsequent call with the same Key.
// No duplicate rows must be created.
func TestTeamUpsertByKey(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewTeamRepository(db)

	original := &storage.Team{
		Key:       "team-alpha",
		Name:      "Alpha FC",
		ShortName: "ALF",
		LogoPath:  "logos/alpha.png",
		Aliases:   []string{"Alpha", "The Alphas"},
	}

	if err := repo.UpsertTeam(original); err != nil {
		t.Fatalf("first UpsertTeam: %v", err)
	}
	if original.ID == 0 {
		t.Fatal("expected ID to be populated after insert, got 0")
	}
	firstID := original.ID

	// Second upsert — same Key, all other fields changed.
	updated := &storage.Team{
		Key:       "team-alpha",
		Name:      "Alpha Volleyball Club",
		ShortName: "AVC",
		LogoPath:  "logos/alpha_v2.png",
		Aliases:   []string{"Alpha", "AVC Stars"},
	}
	if err := repo.UpsertTeam(updated); err != nil {
		t.Fatalf("second UpsertTeam: %v", err)
	}
	if updated.ID != firstID {
		t.Fatalf("upsert created a second row: first ID=%d, second ID=%d", firstID, updated.ID)
	}

	// Reload and verify updated values.
	got, err := repo.GetTeamByKey("team-alpha")
	if err != nil {
		t.Fatalf("GetTeamByKey after upsert: %v", err)
	}
	if got.Name != "Alpha Volleyball Club" {
		t.Errorf("Name: got %q, want %q", got.Name, "Alpha Volleyball Club")
	}
	if got.ShortName != "AVC" {
		t.Errorf("ShortName: got %q, want %q", got.ShortName, "AVC")
	}
	if got.LogoPath != "logos/alpha_v2.png" {
		t.Errorf("LogoPath: got %q, want %q", got.LogoPath, "logos/alpha_v2.png")
	}

	// Confirm exactly one row exists.
	var count int64
	db.Model(&storage.Team{}).Where("key = ?", "team-alpha").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 row with key %q, got %d", "team-alpha", count)
	}
}

// TestTeamAliasPersistence verifies that an aliases slice stored via UpsertTeam
// round-trips through the database unchanged.
func TestTeamAliasPersistence(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewTeamRepository(db)

	want := []string{"Tbilisi Ballers", "TBL", "The Tbilisi Side"}
	team := &storage.Team{
		Key:       "team-tbilisi",
		Name:      "Tbilisi Volleyballers",
		ShortName: "TBL",
		Aliases:   want,
	}

	if err := repo.UpsertTeam(team); err != nil {
		t.Fatalf("UpsertTeam: %v", err)
	}

	got, err := repo.GetTeamByID(team.ID)
	if err != nil {
		t.Fatalf("GetTeamByID: %v", err)
	}

	if len(got.Aliases) != len(want) {
		t.Fatalf("Aliases length: got %d, want %d (got=%v)", len(got.Aliases), len(want), got.Aliases)
	}
	for i := range want {
		if got.Aliases[i] != want[i] {
			t.Errorf("Aliases[%d]: got %q, want %q", i, got.Aliases[i], want[i])
		}
	}
}

// TestTeamSearchRankedResults tests ranked search across three sub-cases:
//  1. Exactly one match is returned.
//  2. Two-to-eight matches are returned in the correct order (exact > prefix > contains).
//  3. When more than limit results exist the returned slice is capped at limit.
//
// Query design:
//
//	Query = "dragon" (lowercase)
//	Exact  matches: name=="Dragon" OR short=="Dragon" OR alias=="Dragon"
//	Prefix matches: name/short/alias starts with "dragon" but is not equal to "dragon"
//	Contains matches: name/short/alias contains "dragon" but does not start with it
func TestTeamSearchRankedResults(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewTeamRepository(db)

	// Seed teams that cover all ranking tiers for query "dragon".
	seeds := []storage.Team{
		// ---- rank 1: exact matches ----
		// exact on name
		{Key: "exact-name", Name: "Dragon", ShortName: "DRG", Aliases: []string{}},
		// exact on short_name
		{Key: "exact-short", Name: "Falcons", ShortName: "Dragon", Aliases: []string{}},
		// exact on alias
		{Key: "exact-alias", Name: "Phoenix Rising", ShortName: "PHX", Aliases: []string{"Dragon"}},

		// ---- rank 2: prefix matches (start with "dragon", not equal to "dragon") ----
		// prefix on name
		{Key: "prefix-name", Name: "Dragon Warriors", ShortName: "DRW", Aliases: []string{}},
		// prefix on short_name
		{Key: "prefix-short", Name: "Ravens", ShortName: "Dragon FC", Aliases: []string{}},
		// prefix on alias
		{Key: "prefix-alias", Name: "Storm Chasers", ShortName: "STC", Aliases: []string{"Dragon Slayers"}},

		// ---- rank 3: contains matches (contain "dragon" but do not start with it) ----
		// contains on name
		{Key: "contains-name", Name: "Wild Dragon United", ShortName: "WDU", Aliases: []string{}},
		// contains on short_name
		{Key: "contains-short", Name: "Owls", ShortName: "Red Dragon", Aliases: []string{}},
		// contains on alias
		{Key: "contains-alias", Name: "Riverside Eagles", ShortName: "RVE", Aliases: []string{"The Dragon Club"}},

		// ---- no match ----
		{Key: "no-match", Name: "Wolves", ShortName: "WLV", Aliases: []string{"Howlers"}},
	}
	for i := range seeds {
		if err := repo.UpsertTeam(&seeds[i]); err != nil {
			t.Fatalf("seed UpsertTeam[%d] (%s): %v", i, seeds[i].Key, err)
		}
	}

	t.Run("one_match", func(t *testing.T) {
		// "Wolves" appears only in team no-match, which is the sole result.
		results, err := repo.SearchTeams("Wolves", 10)
		if err != nil {
			t.Fatalf("SearchTeams: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d: %v", len(results), teamKeys(results))
		}
		if results[0].Key != "no-match" {
			t.Errorf("expected no-match, got %q", results[0].Key)
		}
	})

	t.Run("two_to_eight_matches_ranked", func(t *testing.T) {
		// Query "dragon" (limit 9) should return all 9 matching teams in ranked order.
		results, err := repo.SearchTeams("dragon", 9)
		if err != nil {
			t.Fatalf("SearchTeams: %v", err)
		}
		if len(results) < 2 || len(results) > 9 {
			t.Fatalf("expected 2-9 results, got %d: %v", len(results), teamKeys(results))
		}

		// Build a position map: key -> index in results.
		posOf := make(map[string]int, len(results))
		for i, r := range results {
			posOf[r.Key] = i
		}

		exactKeys := []string{"exact-name", "exact-short", "exact-alias"}
		prefixKeys := []string{"prefix-name", "prefix-short", "prefix-alias"}
		containsKeys := []string{"contains-name", "contains-short", "contains-alias"}

		maxExactPos := maxPos(posOf, exactKeys)
		minPrefixPos := minPos(posOf, prefixKeys)
		maxPrefixPos := maxPos(posOf, prefixKeys)
		minContainsPos := minPos(posOf, containsKeys)

		const missing = int(^uint(0) >> 1) // sentinel for absent key

		if maxExactPos != missing && minPrefixPos != missing {
			if maxExactPos >= minPrefixPos {
				t.Errorf("all exact matches must precede all prefix matches: "+
					"maxExactPos=%d minPrefixPos=%d\nresults=%v",
					maxExactPos, minPrefixPos, teamKeys(results))
			}
		}
		if maxPrefixPos != missing && minContainsPos != missing {
			if maxPrefixPos >= minContainsPos {
				t.Errorf("all prefix matches must precede all contains matches: "+
					"maxPrefixPos=%d minContainsPos=%d\nresults=%v",
					maxPrefixPos, minContainsPos, teamKeys(results))
			}
		}
	})

	t.Run("more_than_eight_matches_limit_applied", func(t *testing.T) {
		// Insert additional "dragon" teams so the total exceeds 8.
		extras := []storage.Team{
			{Key: "extra-1", Name: "Dragon Academy", ShortName: "DAC", Aliases: []string{}},
			{Key: "extra-2", Name: "Mini Dragons", ShortName: "MDR", Aliases: []string{}},
		}
		for i := range extras {
			if err := repo.UpsertTeam(&extras[i]); err != nil {
				t.Fatalf("extra UpsertTeam[%d]: %v", i, err)
			}
		}

		const limit = 5
		results, err := repo.SearchTeams("dragon", limit)
		if err != nil {
			t.Fatalf("SearchTeams: %v", err)
		}
		if len(results) > limit {
			t.Errorf("expected at most %d results, got %d", limit, len(results))
		}
		if len(results) == 0 {
			t.Error("expected at least 1 result, got 0")
		}
	})
}

// TestTeamGetByKeyNotFound verifies that GetTeamByKey wraps gorm.ErrRecordNotFound
// when no team has the given key.
func TestTeamGetByKeyNotFound(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewTeamRepository(db)

	_, err := repo.GetTeamByKey("does-not-exist")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound in error chain, got: %v", err)
	}
}

// TestTeamGetByIDNotFound verifies that GetTeamByID wraps gorm.ErrRecordNotFound
// when no team has the given ID.
func TestTeamGetByIDNotFound(t *testing.T) {
	db := openTestDB(t)
	repo := storage.NewTeamRepository(db)

	_, err := repo.GetTeamByID(99999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound in error chain, got: %v", err)
	}
}

// --- helpers ---------------------------------------------------------------

// teamKeys returns the Key field of each team in the slice, for diagnostic output.
func teamKeys(teams []storage.Team) []string {
	keys := make([]string, len(teams))
	for i, t := range teams {
		keys[i] = t.Key
	}
	return keys
}

const missingPos = int(^uint(0) >> 1)

// maxPos returns the highest (worst) position among the given keys in posOf.
// Returns missingPos if none of the keys appear in posOf.
func maxPos(posOf map[string]int, keys []string) int {
	max := -1
	for _, k := range keys {
		if pos, ok := posOf[k]; ok && pos > max {
			max = pos
		}
	}
	if max == -1 {
		return missingPos
	}
	return max
}

// minPos returns the lowest (best) position among the given keys in posOf.
// Returns missingPos if none of the keys appear in posOf.
func minPos(posOf map[string]int, keys []string) int {
	min := missingPos
	for _, k := range keys {
		if pos, ok := posOf[k]; ok && pos < min {
			min = pos
		}
	}
	return min
}
