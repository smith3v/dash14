# Score Flow Improvements Implementation Plan

**Goal:** Reduce score-update latency by keeping SQLite commits synchronous while moving overlay refresh and subscriber broadcasts off the hot path, adding cached overlay templates with configurable refresh, and tuning SQLite for the app’s runtime workload.

**Architecture:** Extend overlay config with an optional template-cache refresh interval, then move template parsing into an in-memory renderer cache that is loaded once at startup and optionally refreshed in the background. Keep score writes authoritative in SQLite, but reorder Telegram callback handling so the control message updates immediately after commit while overlay rendering and broadcasts run through bounded background workers. Apply SQLite pragmas during database open so reads and writes contend less under the new background activity.

**Tech Stack:** Go, GORM, SQLite, go-telegram/bot, Go HTML templates, table-driven tests

---

### Task 1: Add configurable template-cache refresh interval to config

**Prompt:**
Update `pkg/config/config.go` to add `TemplateCacheRefreshIntervalSeconds int` to `config.OverlayConfig` with YAML key `template_cache_refresh_interval_seconds`. Update `pkg/config/load.go` so runtime validation rejects negative values for this field and continues to treat zero as the default indefinite-cache mode. Update `pkg/config/load_test.go` to cover three cases: field omitted defaults to `0`, a positive value loads successfully, and a negative value fails validation. Update `config.example.yaml` to document the new field with a commented explanation that `0` means load templates once and never refresh them automatically.

Step 1: Write the failing tests in `pkg/config/load_test.go` for omitted, positive, and negative values.

Step 2: Run `go test ./pkg/config`
Expected: FAIL because the new field and validation do not exist yet.

Step 3: Implement the minimal config-model, validation, and example-config changes.

Step 4: Run `go test ./pkg/config`
Expected: PASS

Step 5: Commit with message `Add overlay template refresh config`

---

### Task 2: Apply SQLite runtime pragmas during database open

**Prompt:**
Update `pkg/storage/db.go` so `storage.Open` applies the configured SQLite pragmas needed by the design: `PRAGMA foreign_keys = ON`, `PRAGMA journal_mode = WAL`, `PRAGMA synchronous = NORMAL`, and `PRAGMA busy_timeout = 5000`. Keep the function behavior otherwise unchanged: create the parent directory, open the DB, apply pragmas, and return the configured `*gorm.DB`. Add or update tests in `pkg/storage/db_test.go` to open a temporary database and assert the effective values for `journal_mode`, `synchronous`, and `busy_timeout`. Make the tests verify that opening still succeeds and that the pragma setup is part of the normal initialization path.

Step 1: Write the failing database-open tests in `pkg/storage/db_test.go`.

Step 2: Run `go test ./pkg/storage`
Expected: FAIL because the new pragmas are not applied yet.

Step 3: Implement the minimal pragma application in `pkg/storage/db.go`.

Step 4: Run `go test ./pkg/storage`
Expected: PASS

Step 5: Commit with message `Tune SQLite open pragmas`

---

### Task 3: Cache parsed overlay templates in memory

**Prompt:**
Refactor `pkg/overlay/renderer.go` so parsed templates are loaded once into an in-memory cache owned by `overlay.Renderer` instead of reparsing files on every render call. Keep the public renderer API the same: `RenderPlanned`, `RenderLive`, and `RenderIntermission` should still render atomically to their output files and continue publishing logos as they do now. Add any supporting types you need in `pkg/overlay/renderer.go` or `pkg/overlay/model_builders.go` only if they clearly belong there; do not broaden the surface area more than necessary. Update `pkg/overlay/renderer_test.go` to prove the renderer uses the cached parsed templates and that rendering still produces correct output after the original template file on disk changes when refresh is disabled.

Step 1: Write the failing renderer tests in `pkg/overlay/renderer_test.go` for “load once, reuse many times” behavior.

Step 2: Run `go test ./pkg/overlay`
Expected: FAIL because templates are still parsed from disk on every render.

Step 3: Implement the minimal in-memory template cache in `pkg/overlay/renderer.go`.

Step 4: Run `go test ./pkg/overlay`
Expected: PASS

Step 5: Commit with message `Cache parsed overlay templates`

---

### Task 4: Add optional background template refresh

**Prompt:**
Extend the overlay renderer so template refresh is configurable through `config.OverlayConfig.TemplateCacheRefreshIntervalSeconds`. When the value is `0`, the renderer must parse templates once during construction and never start a refresh goroutine. When the value is greater than zero, the renderer should start a background refresh loop that reparses all configured templates into a fresh snapshot and swaps the cache only if the full parse succeeds. Keep partial refreshes impossible: on any parse failure, retain the previous snapshot and log the error. You may add lifecycle plumbing in `pkg/app/runtime.go` if the renderer needs a context-aware `Start` method or cleanup hook; if you do, keep startup ownership explicit and testable. Update `pkg/overlay/renderer_test.go` and `pkg/app/runtime_test.go` to cover both `0` interval and positive interval behavior.

Step 1: Write the failing tests for zero-interval no-refresh behavior and positive-interval full-snapshot refresh behavior.

Step 2: Run `go test ./pkg/overlay ./pkg/app`
Expected: FAIL because refresh behavior is not implemented yet.

Step 3: Implement the minimal background refresh mechanism and any required runtime wiring.

Step 4: Run `go test ./pkg/overlay ./pkg/app`
Expected: PASS

Step 5: Commit with message `Add configurable template refresh loop`

---

### Task 5: Introduce asynchronous subscriber broadcast worker

**Prompt:**
Refactor `pkg/telegram/broadcast.go` so subscriber broadcasts are queued onto a bounded in-process worker instead of executing synchronously in the caller. Keep fan-out semantics best-effort: one subscriber failure must not stop the rest. The worker should drop new jobs and log a warning when the queue is full rather than blocking the score-update path. Reuse the existing `Router` logger and repositories; do not introduce a separate queueing subsystem or persistence layer. If router construction needs to initialize the worker, update `pkg/telegram/router.go` and any affected tests. Add focused tests in `pkg/telegram/router_test.go` or `pkg/telegram/game_control_test.go` that prove broadcast enqueue is non-blocking and queue saturation drops work instead of blocking.

Step 1: Write the failing async-broadcast tests covering normal enqueue and full-queue behavior.

Step 2: Run `go test ./pkg/telegram`
Expected: FAIL because broadcasts still run synchronously and there is no queue behavior to assert.

Step 3: Implement the minimal bounded broadcast queue and worker in `pkg/telegram/broadcast.go` with any necessary router wiring in `pkg/telegram/router.go`.

Step 4: Run `go test ./pkg/telegram`
Expected: PASS

Step 5: Commit with message `Make broadcasts asynchronous`

---

### Task 6: Reorder game callbacks so admin UI updates before async side effects

**Prompt:**
Update `pkg/telegram/game_control.go` so score-changing callbacks keep SQLite writes synchronous, rebuild the control message from committed state, and call `EditMessageText` before enqueueing overlay rendering and broadcast work. Overlay rendering should become background work as part of the same eventual-consistency strategy used for broadcasts. Keep the existing authority checks, stale-message checks, and committed-state semantics intact. If you need a dedicated overlay job queue, add it in `pkg/telegram` with a bounded design similar to broadcasts, but keep the implementation minimal and local to the router package. Update `pkg/telegram/game_control_test.go` to assert that the callback path edits the control message from committed state before scheduling async side effects and that SQLite write failures still stop all later work.

Step 1: Write the failing Telegram tests in `pkg/telegram/game_control_test.go` for callback ordering and failure handling.

Step 2: Run `go test ./pkg/telegram`
Expected: FAIL because overlay rendering and broadcast scheduling still happen after the current synchronous path, with no explicit ordering guarantees in tests.

Step 3: Implement the minimal callback reordering and background overlay scheduling.

Step 4: Run `go test ./pkg/telegram`
Expected: PASS

Step 5: Commit with message `Prioritize control updates in game callbacks`

---

### Task 7: Full regression pass and documentation cleanup

**Prompt:**
Run the full repository test suite after all prior tasks are merged. Review the checked-in docs and config example so the new behavior is discoverable and consistent with the design. If needed, update `README.md` to mention the optional overlay template refresh interval and the fact that broadcasts/overlay updates are eventually consistent relative to the admin control message. Do not broaden the behavior beyond what the design approved. The goal is to leave the repository in a coherent, documented, fully tested state.

Step 1: Run `go test ./...`
Expected: PASS

Step 2: Review `git diff --stat`
Expected: only files relevant to config, storage, overlay, telegram, tests, and docs changed.

Step 3: Update `README.md` only if the new runtime behavior or config knob is not already documented clearly enough.

Step 4: Run `go test ./...`
Expected: PASS

Step 5: Commit with message `Document score flow runtime changes`

---
