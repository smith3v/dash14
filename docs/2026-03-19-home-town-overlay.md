# Home Town Overlay Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add team hometown support to imports, storage, and planned/intermission overlays so the third line shows hometown instead of short name.

**Architecture:** Extend the `Team` model and YAML import record with a new `Hometown` field, rely on GORM `AutoMigrate` to add the database column, and pass the field through overlay view models used by planned and intermission rendering. Keep live overlay behavior unchanged by preserving short-name fields there.

**Tech Stack:** Go, GORM, SQLite, YAML v3, Go HTML templates

---

### Task 1: Extend import and storage models

**Files:**
- Modify: `pkg/importer/teams_yaml.go`
- Modify: `pkg/importer/importer.go`
- Modify: `pkg/storage/team.go`
- Modify: `pkg/storage/team_repository.go`

**Step 1: Write the failing test**

Add hometown assertions to importer and repository tests so upserts and re-imports must persist the new field.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/importer ./pkg/storage`
Expected: FAIL on missing `Hometown` fields or assertions.

**Step 3: Write minimal implementation**

Add `Hometown` to the YAML record, storage model, and repository upsert assignments, then populate it from imports.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/importer ./pkg/storage`
Expected: PASS

### Task 2: Pass hometown through planned and intermission overlays

**Files:**
- Modify: `pkg/overlay/view_model.go`
- Modify: `pkg/overlay/model_builders.go`
- Modify: `pkg/app/runtime.go`
- Modify: `pkg/telegram/plan_flow.go`
- Modify: `pkg/telegram/game_control.go`
- Test: `pkg/overlay/renderer_test.go`
- Test: `pkg/overlay/model_builders_test.go`

**Step 1: Write the failing test**

Add view-model and template rendering assertions proving planned/intermission use hometown and render nothing when hometown is empty.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/overlay ./pkg/app ./pkg/telegram`
Expected: FAIL because planned/intermission models do not expose hometown yet.

**Step 3: Write minimal implementation**

Add hometown fields to the relevant view models and update builder/call-site wiring while leaving live overlay short names untouched.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/overlay ./pkg/app ./pkg/telegram`
Expected: PASS

### Task 3: Update templates and checked-in team data

**Files:**
- Modify: `templates/planned.html.tmpl`
- Modify: `templates/intermission.html.tmpl`
- Modify: `teams/teams.yaml`
- Modify: `pkg/importer/testdata/teams-valid.yaml`

**Step 1: Write the failing test**

Template tests from Task 2 should expect hometown text instead of short name for planned and intermission output.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/overlay ./pkg/importer`
Expected: FAIL until templates and sample YAML are updated.

**Step 3: Write minimal implementation**

Render hometown on the third line with the existing conditional behavior and backfill hometown for every checked-in team record.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/overlay ./pkg/importer`
Expected: PASS

### Task 4: Full regression verification

**Files:**
- Test: `pkg/...`

**Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS

**Step 2: Review diff**

Run: `git diff --stat`
Expected: only hometown-related files changed.
