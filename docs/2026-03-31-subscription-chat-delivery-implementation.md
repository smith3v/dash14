# Subscription Chat Delivery Implementation Plan

**Goal:** Fix the subscription delivery review finding by storing the subscribed Telegram chat target and routing broadcasts through it.

**Architecture:** Extend `storage.User` with a persisted subscription chat target and add a chat-aware upsert path in `pkg/storage` so `/start` and `/stop` can refresh it safely. Then update Telegram start/stop handlers and broadcast fan-out to use the stored chat ID, while preserving exclusion behavior keyed by Telegram user ID.

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

### Task 2: Update `/start`, `/stop`, and broadcast fan-out to use subscription chat IDs

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

### Task 3: Review, full regression, and documentation cleanup

**Prompt:**
Run the full repository test suite after all prior tasks are merged. Review the design and implementation docs for consistency with the shipped behavior. Update `README.md` if needed so subscription behavior clearly states that updates go to the chat that ran `/start`, and add a short note if operators should re-run `/start` after deploying the migration so old subscribers get a stored chat target. Then review the branch against `main`, fix any findings that are real, and rerun the full test suite.

Step 1: Run `go test ./...`
Expected: PASS

Step 2: Review `git diff --stat`
Expected: only storage, telegram, tests, docs, and any necessary README changes are present.

Step 3: Review the branch against `main` with a code-review mindset, address any real findings, and rerun the impacted tests.

Step 4: Run `go test ./...`
Expected: PASS

Step 5: Commit with message `docs: finalize subscription chat delivery fix`

---
