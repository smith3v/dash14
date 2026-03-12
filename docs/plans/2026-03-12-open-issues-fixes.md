# Open Issues Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Resolve GitHub issues #2, #3, and #4 with isolated, test-backed commits.

**Architecture:** Keep fixes scoped by issue: Telegram broadcast behavior in `telegram`, match lifecycle rules in `game` + docs, and CI configuration in `.github/workflows`. Preserve existing package boundaries and table-driven tests.

**Tech Stack:** Go 1.26, GORM/SQLite, GitHub Actions

---

### Task 1: Issue #2 - Do Not Broadcast Updates To Current Admin

**Files:**
- Modify: `telegram/broadcast.go`
- Modify: `telegram/game_control.go`
- Modify: `telegram/start_stop_test.go`
- Modify: `telegram/game_control_test.go`

**Step 1: Write the failing tests**

- Add a test for broadcast exclusion behavior in `telegram/start_stop_test.go`.
- Add a game-control test in `telegram/game_control_test.go` that verifies score update broadcasts are sent to subscribed users but skipped for the current admin.

**Step 2: Run tests to verify failure**

Run: `go test ./telegram -run 'Broadcast|GameControl' -v`

Expected: FAIL for new exclusion expectations.

**Step 3: Write minimal implementation**

- Add an exclusion-aware broadcast path in `telegram/broadcast.go`.
- Update game-control callback flow in `telegram/game_control.go` to broadcast update messages while excluding `CurrentAdminUserID`.
- Keep existing `Broadcast` behavior unchanged for callers that do not pass exclusions.

**Step 4: Run tests to verify pass**

Run: `go test ./telegram -run 'Broadcast|GameControl' -v`

Expected: PASS.

**Step 5: Commit**

Run:

```bash
git add telegram/broadcast.go telegram/game_control.go telegram/start_stop_test.go telegram/game_control_test.go
git commit -m "telegram: skip broadcasts to active admin"
```

### Task 2: Issue #3 - Set Count Logic (Minimum 4 Sets, 5th Only At 2-2)

**Files:**
- Modify: `docs/2026-03-09-dash14-design.md`
- Modify: `game/lifecycle.go`
- Modify: `game/lifecycle_test.go`
- Modify: `telegram/game_control.go`
- Modify: `telegram/game_control_test.go`

**Step 1: Write the failing tests**

- Extend lifecycle tests to assert:
  - Set 3 completion never prompts game finish.
  - Set 4 at `2-2` creates set 5.
  - Set 4 at non-`2-2` prompts finish.
  - `ConfirmGameFinished` rejects early finish attempts (<4 completed sets or `2-2` after 4 sets).
- Add/update game-control test to ensure finish prompt behavior matches new rules.

**Step 2: Run tests to verify failure**

Run: `go test ./game ./telegram -run 'ConfirmSetFinished|ConfirmGameFinished|GameControl' -v`

Expected: FAIL against old 3-set logic.

**Step 3: Write minimal implementation**

- Update match-rule section in design doc to the new set-count policy.
- Change `ConfirmSetFinished` transitions in `game/lifecycle.go`:
  - always continue through set 4,
  - create set 5 only when set score reaches `2-2` after set 4,
  - prompt finish after set 4 when not tied, or after set 5.
- Add a helper for game-finish eligibility and apply it in `ConfirmGameFinished`.
- Update Telegram control button logic to show `Is game finished?` only when the game is actually finish-eligible.

**Step 4: Run tests to verify pass**

Run: `go test ./game ./telegram -v`

Expected: PASS.

**Step 5: Commit**

Run:

```bash
git add docs/2026-03-09-dash14-design.md game/lifecycle.go game/lifecycle_test.go telegram/game_control.go telegram/game_control_test.go
git commit -m "game: require at least four sets before finish"
```

### Task 3: Issue #4 - Configure CI To Run Tests On Push

**Files:**
- Create: `.github/workflows/tests.yml`

**Step 1: Write minimal workflow**

- Add GitHub Actions workflow that runs on `push` and `pull_request`.
- Set up Go from `go.mod`.
- Run `go test ./...`.

**Step 2: Validate locally**

Run: `go test ./...`

Expected: PASS.

**Step 3: Commit**

Run:

```bash
git add .github/workflows/tests.yml
git commit -m "ci: run go tests on push and pull request"
```
