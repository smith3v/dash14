package storage

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// TeamRepository provides persistence operations for Team records.
type TeamRepository struct {
	db *gorm.DB
}

// NewTeamRepository constructs a TeamRepository backed by the given database.
func NewTeamRepository(db *gorm.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

// UpsertTeam inserts a new team or updates an existing one matched by Key.
// All updatable fields are overwritten on conflict.
func (r *TeamRepository) UpsertTeam(team *Team) error {
	result := r.db.
		Where(Team{Key: team.Key}).
		Assign(Team{
			Name:      team.Name,
			ShortName: team.ShortName,
			LogoPath:  team.LogoPath,
			Aliases:   team.Aliases,
		}).
		FirstOrCreate(team)
	if result.Error != nil {
		return fmt.Errorf("storage: upsert team %q: %w", team.Key, result.Error)
	}

	// FirstOrCreate with Assign handles both paths, but we call Save explicitly
	// on the update path (RowsAffected == 0) to ensure all assigned fields are
	// persisted reliably.
	if result.RowsAffected == 0 {
		if err := r.db.Save(team).Error; err != nil {
			return fmt.Errorf("storage: update team %q: %w", team.Key, err)
		}
	}
	return nil
}

// GetTeamByKey returns the team with the given key. It returns a wrapped
// gorm.ErrRecordNotFound when no team exists with that key.
func (r *TeamRepository) GetTeamByKey(key string) (*Team, error) {
	var team Team
	if err := r.db.Where("key = ?", key).First(&team).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("storage: team with key %q not found: %w", key, gorm.ErrRecordNotFound)
		}
		return nil, fmt.Errorf("storage: get team by key %q: %w", key, err)
	}
	return &team, nil
}

// GetTeamByID returns the team with the given ID. It returns a wrapped
// gorm.ErrRecordNotFound when no team exists with that ID.
func (r *TeamRepository) GetTeamByID(id uint) (*Team, error) {
	var team Team
	if err := r.db.First(&team, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("storage: team with id %d not found: %w", id, gorm.ErrRecordNotFound)
		}
		return nil, fmt.Errorf("storage: get team by id %d: %w", id, err)
	}
	return &team, nil
}

// SearchTeams returns up to limit teams whose name, short_name, or any alias
// matches query. Results are ranked: exact matches first, then prefix matches,
// then contains matches. Within each rank results are ordered alphabetically by
// name.
//
// Alias matching uses SQLite LIKE on the JSON text column because the
// glebarez/sqlite driver does not expose json_each() in a GORM-compatible form.
// Each alias in the JSON array is wrapped in double-quotes, so the patterns
// anchor on those quote characters to reduce false positives.
//
// The rank CASE expression in ORDER BY is built with literal SQL string values
// rather than ? placeholders, because GORM does not pass extra bindings for the
// ORDER BY clause when using clause.Expr Vars. The values are escaped using
// standard SQLite single-quote doubling before embedding.
func (r *TeamRepository) SearchTeams(query string, limit int) ([]Team, error) {
	if query == "" || limit <= 0 {
		return nil, nil
	}

	// Lower-case the query for case-insensitive matching against lower(column).
	q := strings.ToLower(query)

	// Build pattern strings.
	exact := q
	prefix := q + "%"
	contains := "%" + q + "%"

	// Alias JSON patterns. The JSON serialization wraps each string value in
	// double-quotes: ["foo","bar baz"]. We exploit this to anchor matches:
	//   exact alias:    %"<q>"%   — value bounded on both sides
	//   prefix alias:   %"<q>%    — value starts after an opening quote
	//   contains alias: %<q>%     — q appears anywhere in the JSON text
	exactAlias := `%"` + q + `"%`
	prefixAlias := `%"` + q + `%`
	containsAlias := "%" + q + "%"

	// sqlLit returns a safely embedded SQLite string literal using single-quote
	// doubling (the only required escaping for SQLite string literals).
	sqlLit := func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}

	// CASE expression assigns a numeric rank to each row. Lower is better.
	// We embed literals directly so the expression is self-contained in ORDER BY.
	rankSQL := fmt.Sprintf(
		`CASE`+
			` WHEN lower(name)=%[1]s OR lower(short_name)=%[1]s OR aliases LIKE %[4]s THEN 1`+
			` WHEN lower(name) LIKE %[2]s OR lower(short_name) LIKE %[2]s OR aliases LIKE %[5]s THEN 2`+
			` WHEN lower(name) LIKE %[3]s OR lower(short_name) LIKE %[3]s OR aliases LIKE %[6]s THEN 3`+
			` ELSE 4 END`,
		sqlLit(exact),
		sqlLit(prefix),
		sqlLit(contains),
		sqlLit(exactAlias),
		sqlLit(prefixAlias),
		sqlLit(containsAlias),
	)

	// WHERE uses parameterised bindings (safe from injection).
	const whereSQL = `(` +
		`lower(name)=? OR lower(short_name)=? OR aliases LIKE ? ` +
		`OR lower(name) LIKE ? OR lower(short_name) LIKE ? OR aliases LIKE ? ` +
		`OR lower(name) LIKE ? OR lower(short_name) LIKE ? OR aliases LIKE ?` +
		`)`
	whereArgs := []interface{}{
		exact, exact, exactAlias,
		prefix, prefix, prefixAlias,
		contains, contains, containsAlias,
	}

	var teams []Team
	if err := r.db.
		Where(whereSQL, whereArgs...).
		Order(rankSQL + ", name ASC").
		Limit(limit).
		Find(&teams).Error; err != nil {
		return nil, fmt.Errorf("storage: search teams %q: %w", query, err)
	}
	return teams, nil
}
