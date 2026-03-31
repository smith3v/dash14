# Lifecycle Persistence Implementation Plan

**Goal:** Fix the lifecycle consistency review finding by making multi-row match lifecycle writes atomic.

**Architecture:** Keep rule computation in `pkg/game`, but move multi-row lifecycle persistence into explicit transactional methods in `pkg/storage` so partial writes cannot strand the app in an impossible phase. Then update Telegram plan and game-control flows to use those transactional methods instead of orchestrating separate writes themselves.

**Tech Stack:** Go, GORM, SQLite, go-telegram/bot, table-driven tests

---

### Task 1: Add transactional lifecycle write methods to the game repository

**Prompt:**
Refactor `pkg/storage/game_repository.go` so the lifecycle writes that currently require multiple calls can be performed atomically inside repository-owned transactions. Add focused methods for the transitions used by Telegram: create planned game plus set current game, start game plus create first set, start next set plus create next set, finish set plus update game aggregate state, and finish game plus clear current game. Keep existing read helpers and simple save methods unless a call site no longer needs them. Add or extend tests in `pkg/storage/game_repository_test.go` to simulate failures inside each transactional sequence and assert the entire transition rolls back, leaving the pre-operation database state unchanged.

Step 1: Write failing storage tests for rollback behavior of each new transactional method.

Step 2: Run `go test ./pkg/storage -run 'GameRepository|AppState' -v`
Expected: FAIL because the transactional methods and rollback guarantees do not exist yet.

Step 3: Implement the minimal repository transaction methods in `pkg/storage/game_repository.go`.

Step 4: Run `go test ./pkg/storage -run 'GameRepository|AppState' -v`
Expected: PASS

Step 5: Commit with message `storage: make lifecycle transitions atomic`

---

### Task 2: Switch game-control and plan flows to transactional repository methods

**Prompt:**
Update `pkg/telegram/plan_flow.go` and `pkg/telegram/game_control.go` to replace the existing multi-call lifecycle write sequences with the new transactional repository methods from `pkg/storage`. Keep the existing authority checks, state derivation via `pkg/game`, control-message rendering, overlay scheduling, and broadcast behavior unchanged except for the persistence call boundaries. Remove now-redundant partial-write handling from the handlers where appropriate. Update `pkg/telegram/game_control_test.go` and `pkg/telegram/plan_flow_test.go` to cover the specific rollback-sensitive paths identified in review: failed set creation after game update, failed game update after finished-set save, and failed current-game assignment during planning.

Step 1: Write the failing Telegram tests for lifecycle rollback-sensitive paths.

Step 2: Run `go test ./pkg/telegram -run 'Plan|GameControl' -v`
Expected: FAIL because handlers still orchestrate the multi-step writes directly.

Step 3: Implement the minimal handler refactor to call the transactional repository methods.

Step 4: Run `go test ./pkg/telegram -run 'Plan|GameControl' -v`
Expected: PASS

Step 5: Commit with message `telegram: use atomic lifecycle persistence`

---

### Task 3: Review, full regression, and documentation cleanup

**Prompt:**
Run the full repository test suite after all prior tasks are merged. Review the design and implementation docs for consistency with the shipped behavior. Update `README.md` only if the lifecycle behavior or operator expectations are documented ambiguously. Then review the branch against `main`, fix any findings that are real, and rerun the full test suite.

Step 1: Run `go test ./...`
Expected: PASS

Step 2: Review `git diff --stat`
Expected: only storage, telegram, tests, docs, and any necessary README changes are present.

Step 3: Review the branch against `main` with a code-review mindset, address any real findings, and rerun the impacted tests.

Step 4: Run `go test ./...`
Expected: PASS

Step 5: Commit with message `docs: finalize lifecycle persistence fix`

---
