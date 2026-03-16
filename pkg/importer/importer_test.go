package importer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/smith3v/dash14/pkg/storage"
)

// openTestDB opens a fresh SQLite database in a temp directory and runs all
// migrations. Each call produces an isolated database.
func openTestDB(t *testing.T) *storage.TeamRepository {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("storage.Migrate: %v", err)
	}
	return storage.NewTeamRepository(db)
}

// writeTempLogo writes a small fake PNG file to dir with the given name and
// returns the full path.
func writeTempLogo(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	// Minimal content — the importer does not validate image format.
	if err := os.WriteFile(path, []byte("fake-logo-data"), 0o644); err != nil {
		t.Fatalf("writeTempLogo: %v", err)
	}
	return path
}

// TestCopyLogo_FileExistsWithStableName verifies that CopyLogo creates a file
// in the logo directory whose name is {teamKey}{ext}, regardless of the
// original source filename.
func TestCopyLogo_FileExistsWithStableName(t *testing.T) {
	srcDir := t.TempDir()
	logoDir := t.TempDir()

	// Source file has a different base name than the team key.
	srcPath := writeTempLogo(t, srcDir, "original-name.png")

	store := NewLogoStore(logoDir)
	relPath, err := store.CopyLogo("tbilisi-wolves", srcPath)
	if err != nil {
		t.Fatalf("CopyLogo: unexpected error: %v", err)
	}

	wantFilename := "tbilisi-wolves.png"
	if relPath != wantFilename {
		t.Errorf("CopyLogo returned %q, want %q", relPath, wantFilename)
	}

	destPath := filepath.Join(logoDir, wantFilename)
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Errorf("expected file %q to exist in logo directory, but it does not", destPath)
	}
}

// TestCopyLogo_SourceEqualsDestination_NoTruncate verifies that when source
// already points at the managed destination path, CopyLogo is a no-op and does
// not truncate the file.
func TestCopyLogo_SourceEqualsDestination_NoTruncate(t *testing.T) {
	logoDir := t.TempDir()
	srcPath := filepath.Join(logoDir, "same-team.png")
	content := []byte("same-file-logo-content")

	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("write source logo: %v", err)
	}

	store := NewLogoStore(logoDir)
	relPath, err := store.CopyLogo("same-team", srcPath)
	if err != nil {
		t.Fatalf("CopyLogo: unexpected error: %v", err)
	}
	if relPath != "same-team.png" {
		t.Fatalf("CopyLogo returned %q, want %q", relPath, "same-team.png")
	}

	got, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source logo after copy: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("source file content changed: got %q, want %q", string(got), string(content))
	}
}

// TestCopyLogo_RelativePathOnly verifies that CopyLogo returns only the
// filename (no directory component) so that only a relative path is stored in
// the database.
func TestCopyLogo_RelativePathOnly(t *testing.T) {
	srcDir := t.TempDir()
	logoDir := t.TempDir()

	srcPath := writeTempLogo(t, srcDir, "logo.png")

	store := NewLogoStore(logoDir)
	relPath, err := store.CopyLogo("my-team", srcPath)
	if err != nil {
		t.Fatalf("CopyLogo: unexpected error: %v", err)
	}

	// Must not contain any directory separator.
	if filepath.Dir(relPath) != "." {
		t.Errorf("CopyLogo returned %q which contains a directory component; want filename only", relPath)
	}

	wantFilename := "my-team.png"
	if relPath != wantFilename {
		t.Errorf("CopyLogo returned %q, want %q", relPath, wantFilename)
	}
}

// TestCopyLogo_EmptySourcePath verifies that CopyLogo returns ("", nil) when
// sourcePath is empty, without creating any files or returning an error.
func TestCopyLogo_EmptySourcePath(t *testing.T) {
	logoDir := t.TempDir()

	store := NewLogoStore(logoDir)
	relPath, err := store.CopyLogo("some-team", "")
	if err != nil {
		t.Fatalf("CopyLogo with empty source: unexpected error: %v", err)
	}
	if relPath != "" {
		t.Errorf("CopyLogo with empty source: got %q, want %q", relPath, "")
	}
}

// TestImportTeams_LogoCopiedAndTeamUpserted verifies that ImportTeams creates
// a team record in the database with the logo filename as its LogoPath, and
// that the logo file exists in the logo directory.
func TestImportTeams_LogoCopiedAndTeamUpserted(t *testing.T) {
	srcDir := t.TempDir()
	logoDir := t.TempDir()
	yamlDir := t.TempDir()

	srcPath := writeTempLogo(t, srcDir, "lokomotiv.png")

	yamlContent := `- key: lokomotiv
  name: Lokomotiv Novosibirsk
  short_name: LOK
  logo: ` + srcPath + `
  aliases:
    - Loko
`
	yamlPath := filepath.Join(yamlDir, "teams.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	repo := openTestDB(t)
	store := NewLogoStore(logoDir)
	imp := NewImporter(repo, store)

	if err := imp.ImportTeams(yamlPath); err != nil {
		t.Fatalf("ImportTeams: unexpected error: %v", err)
	}

	// Verify the team was upserted.
	team, err := repo.GetTeamByKey("lokomotiv")
	if err != nil {
		t.Fatalf("GetTeamByKey: %v", err)
	}
	if team.Name != "Lokomotiv Novosibirsk" {
		t.Errorf("Name: got %q, want %q", team.Name, "Lokomotiv Novosibirsk")
	}
	if team.ShortName != "LOK" {
		t.Errorf("ShortName: got %q, want %q", team.ShortName, "LOK")
	}

	// Verify LogoPath is relative (filename only).
	wantLogoPath := "lokomotiv.png"
	if team.LogoPath != wantLogoPath {
		t.Errorf("LogoPath: got %q, want %q", team.LogoPath, wantLogoPath)
	}
	if filepath.Dir(team.LogoPath) != "." {
		t.Errorf("LogoPath %q must not contain a directory component", team.LogoPath)
	}

	// Verify logo file was written to the logo directory.
	destPath := filepath.Join(logoDir, wantLogoPath)
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Errorf("expected logo file at %q, but it does not exist", destPath)
	}
}

// TestImportTeams_ReImportUpdatesTeam verifies that importing the same team
// twice (with changed metadata) updates the existing record rather than
// creating a duplicate, and that exactly one row exists with the updated data.
func TestImportTeams_ReImportUpdatesTeam(t *testing.T) {
	srcDir := t.TempDir()
	logoDir := t.TempDir()
	yamlDir := t.TempDir()

	// First import.
	logo1 := writeTempLogo(t, srcDir, "zenit-v1.png")
	yaml1 := `- key: zenit
  name: Zenit Saint Petersburg
  short_name: ZEN
  logo: ` + logo1 + `
  aliases:
    - Zenit SPb
`
	yamlPath := filepath.Join(yamlDir, "teams.yaml")
	if err := os.WriteFile(yamlPath, []byte(yaml1), 0o644); err != nil {
		t.Fatalf("write yaml1: %v", err)
	}

	repo := openTestDB(t)
	store := NewLogoStore(logoDir)
	imp := NewImporter(repo, store)

	if err := imp.ImportTeams(yamlPath); err != nil {
		t.Fatalf("first ImportTeams: unexpected error: %v", err)
	}

	first, err := repo.GetTeamByKey("zenit")
	if err != nil {
		t.Fatalf("GetTeamByKey after first import: %v", err)
	}
	firstID := first.ID

	// Second import — same key, different name, short_name, logo, and aliases.
	logo2 := writeTempLogo(t, srcDir, "zenit-v2.png")
	yaml2 := `- key: zenit
  name: Zenit Kazan
  short_name: ZKZ
  logo: ` + logo2 + `
  aliases:
    - Kazan Zenit
    - ZKZ Stars
`
	if err := os.WriteFile(yamlPath, []byte(yaml2), 0o644); err != nil {
		t.Fatalf("write yaml2: %v", err)
	}

	if err := imp.ImportTeams(yamlPath); err != nil {
		t.Fatalf("second ImportTeams: unexpected error: %v", err)
	}

	second, err := repo.GetTeamByKey("zenit")
	if err != nil {
		t.Fatalf("GetTeamByKey after second import: %v", err)
	}

	// Same row must be updated — no new row created.
	if second.ID != firstID {
		t.Errorf("re-import created a new row: first ID=%d, second ID=%d", firstID, second.ID)
	}

	// Metadata must reflect the second import.
	if second.Name != "Zenit Kazan" {
		t.Errorf("Name after re-import: got %q, want %q", second.Name, "Zenit Kazan")
	}
	if second.ShortName != "ZKZ" {
		t.Errorf("ShortName after re-import: got %q, want %q", second.ShortName, "ZKZ")
	}

	// Logo filename is stable — the extension is preserved but the base name is
	// always {teamKey}, so both imports produce the same dest filename.
	wantLogoPath := "zenit.png"
	if second.LogoPath != wantLogoPath {
		t.Errorf("LogoPath after re-import: got %q, want %q", second.LogoPath, wantLogoPath)
	}
}

// TestImportTeams_NoLogoField verifies that a team record with no logo field
// is imported successfully and stored with an empty LogoPath.
func TestImportTeams_NoLogoField(t *testing.T) {
	yamlDir := t.TempDir()
	logoDir := t.TempDir()

	yaml := `- key: minimal-team
  name: Minimal Team
  short_name: MIN
`
	yamlPath := filepath.Join(yamlDir, "teams.yaml")
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	repo := openTestDB(t)
	store := NewLogoStore(logoDir)
	imp := NewImporter(repo, store)

	if err := imp.ImportTeams(yamlPath); err != nil {
		t.Fatalf("ImportTeams: unexpected error: %v", err)
	}

	team, err := repo.GetTeamByKey("minimal-team")
	if err != nil {
		t.Fatalf("GetTeamByKey: %v", err)
	}
	if team.LogoPath != "" {
		t.Errorf("LogoPath: got %q, want empty string", team.LogoPath)
	}
}
