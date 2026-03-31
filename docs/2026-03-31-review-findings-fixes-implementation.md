# Review Findings Fixes Implementation Plan

**Goal:** Fix the two code-review findings by making match lifecycle writes atomic and by storing the subscribed Telegram chat target used for broadcasts.

**Architecture:** Keep rule computation in `pkg/game`, but move multi-row lifecycle persistence into explicit transactional methods in `pkg/storage` so partial writes cannot strand the app in an impossible phase. Extend `storage.User` with a persisted subscription chat target and route `/start`, `/stop`, and broadcast fan-out through that field so delivery matches the actual subscribed chat.

**Tech Stack:** Go, GORM, SQLite, go-telegram/bot, table-driven tests

---

### Task 1: Add subscription chat target to storage

**Prompt:**
Update `pkg/storage/user.go` to add a persisted `SubscriptionChatID int64` field to `storage.User`. Update `pkg/storage/user_repository.go` so user upsert methods can persist the chat ID seen during `/start` and `/stop` without overwriting admin flags. If the existing API shape becomes awkward, add a focused method such as `UpsertTelegramUserWithChat(...)` and keep call sites simple. Update `pkg/storage/migrate.go` only if needed for clarity, but rely on `AutoMigrate` rather than manual SQL. Add repository tests in `pkg/storage/user_repository_test.go` that prove the chat ID is inserted on first contact, updated on later contact, and does not reset `IsAdmin` or unrelated fields.

Step 1: Write the failing tests in `pkg/storage/user_repository_test.go` for insert and update behavior of `SubscriptionChatID`.

Step 2: Run `go test ./pkg/storage -run 'User' -v`
Expected: FAIL because the field and persistence path do not exist yet.

Step 3: Implement the minimal storage-model and repository changes.

Step 4: Run `go test ./pkg/storage -run 'User' -v`
Expected: PASS

Step 5: Commit with message `storage: persist subscriber chat target`

---

### Task 2: Add transactional lifecycle write methods to the game repository

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

### Task 3: Update `/start`, `/stop`, and broadcast fan-out to use subscription chat IDs

**Prompt:**
Update `pkg/telegram/start_stop.go` so `/start` and `/stop` persist the incoming `update.Message.Chat.ID` as the subscription chat target while keeping the current success messages and subscription semantics. Update `pkg/telegram/broadcast.go` so broadcasts send to the stored subscription chat ID instead of `TelegramUserID`, and skip invalid zero chat IDs with a warning rather than failing the whole fan-out. Update Telegram tests in `pkg/telegram/start_stop_test.go` and any other focused test file so they prove `/start` stores the chat target, `/stop` preserves behavior, broadcasts use the stored chat target, and users without a stored chat are skipped safely.

Step 1: Write the failing Telegram tests for chat-target persistence and broadcast delivery.

Step 2: Run `go test ./pkg/telegram -run 'Start|Stop|Broadcast' -v`
Expected: FAIL because the chat target is not stored or used yet.

Step 3: Implement the minimal `/start`, `/stop`, and broadcast changes.

Step 4: Run `go test ./pkg/telegram -run 'Start|Stop|Broadcast' -v`
Expected: PASS

Step 5: Commit with message `telegram: deliver broadcasts to subscribed chat`

---

### Task 4: Switch game-control and plan flows to transactional repository methods

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

### Task 5: Review, full regression, and documentation cleanup

**Prompt:**
Run the full repository test suite after all prior tasks are merged. Review the design and implementation docs for consistency with the shipped behavior. Update `README.md` if needed so subscription behavior clearly states that updates go to the chat that ran `/start`, and add a short note if operators should re-run `/start` after deploying the migration so old subscribers get a stored chat target. Then review the branch against `main`, fix any findings that are real, and rerun the full test suite.

Step 1: Run `go test ./...`
Expected: PASS

Step 2: Review `git diff --stat`
Expected: only storage, telegram, tests, docs, and any necessary README changes are present.

Step 3: Review the branch against `main` with a code-review mindset, address any real findings, and rerun the impacted tests.

Step 4: Run `go test ./...`
Expected: PASS

Step 5: Commit with message `docs: finalize review-finding fixes`

---
