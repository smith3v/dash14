# Season Team Catalog Implementation Plan

**Goal:** Prepare the 2026–2027 Heren 2e Klasse C and cup team catalog and its
missing official logo sources without modifying the production database or
importer.

**Architecture:** Keep `teams/teams.yaml` as the source catalog consumed by
the existing `--import` mode. Reuse existing source assets for clubs already
represented, add a single source asset per missing club, and append only the
new season's records.

**Tech Stack:** Go 1.26, YAML, existing `pkg/importer` parser and tests,
official Volleybal.nl/Nevobo club-logo assets.

---

### Task 1: Add the missing club-logo sources

**Prompt:**

1. Download the official Nevobo logo for each club without a suitable existing
   source asset: Albatros, Compaen, VCC'92, The Setfighters, and VVM'63.
2. Store one image per club in `teams/logos/` using a stable club filename.
3. Inspect the downloaded image metadata and confirm it is a readable image.
4. Verify no existing image in `teams/logos/` was replaced or removed.
5. Commit the source assets with the catalog update.

---

### Task 2: Append the season records

**Prompt:**

1. Append only the thirteen unique Heren 2e Klasse C and cup records to
   `teams/teams.yaml`.
2. Preserve every existing record byte-for-byte.
3. Fill `key`, `name`, `short_name`, `hometown`, `logo`, and `aliases` for
   each new record.
4. Point teams from the same club at the same logo source file.
5. Parse the catalog with the existing importer parser and inspect the
   thirteen appended records.
6. Commit the YAML and logo assets with a clear season-catalog message.

---

### Task 3: Verify and review

**Prompt:**

1. Run `GOCACHE="$PWD/.gocache" GOMODCACHE="$PWD/.gomodcache" go test ./pkg/importer`.
2. Run `git diff main -- teams/ docs/` and verify only catalog assets and
   supporting documentation are present.
3. Review the change for name, key, hometown, logo-reference, and alias
   accuracy against the official standings.
4. Fix any real finding, rerun the importer tests, and amend the local commit
   if needed.
