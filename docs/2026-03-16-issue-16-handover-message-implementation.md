# Handover Message Implementation Plan

**Goal:** Update the takeover notification so it identifies the new admin by `@username`, or by display name when no username exists.

**Architecture:** Persist each Telegram user's display name in the local `users` table alongside the username, populate it whenever `/start` or `/stop` upserts the user record, and centralize takeover-recipient formatting in the Telegram router. This keeps the takeover path deterministic and avoids adding a live Telegram API lookup just to render a notification string.

**Tech Stack:** Go, GORM, SQLite, go-telegram/bot, table-driven tests

---

### Task 1: Persist display names in user storage

**Prompt:**
Update `/Users/neuron/dev/dash14/pkg/storage/user.go` to add a `DisplayName` field to `storage.User`. Update `/Users/neuron/dev/dash14/pkg/storage/user_repository.go` so `UpsertTelegramUser` accepts `(telegramUserID int64, username string, displayName string)` and writes both `username` and `display_name` on conflict while preserving `Subscribed` and `IsAdmin`. Update `/Users/neuron/dev/dash14/pkg/storage/user_repository_test.go` to verify initial insert stores the display name and repeat upserts update both username and display name without duplicating rows or resetting subscription state. Run focused storage tests for the repository changes.

---

### Task 2: Populate display names from Telegram updates

**Prompt:**
Update `/Users/neuron/dev/dash14/pkg/telegram/start_stop.go` to derive a display name from `update.Message.From` and pass it into the user upsert on both `/start` and `/stop`. Prefer a helper that combines first and last names when present and falls back to username or a stable placeholder only if both names are empty. Update `/Users/neuron/dev/dash14/pkg/telegram/start_stop_test.go` helpers as needed so test-created admin records include a realistic display name. Run the relevant Telegram tests that cover start/stop flows.

---

### Task 3: Format takeover notifications with username/display-name fallback

**Prompt:**
Update `/Users/neuron/dev/dash14/pkg/telegram/takeover.go` to format the notification recipient label from the new admin’s stored user record: use `@username` when available, otherwise `DisplayName`. Add or update tests in `/Users/neuron/dev/dash14/pkg/telegram/takeover_test.go` to cover both cases and confirm the previous admin receives the improved message text. Run the targeted Telegram test suite for takeover behavior and then run `go test ./...` to verify the full repo.

---
