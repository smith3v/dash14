# dash14 Implementation Plan

**Goal:** Build `dash14`, a local Go application that manages one volleyball match through Telegram, persists state in SQLite, and serves a planned/live OBS overlay over a local HTTP server.

**Architecture:** Implement the app as a single Go binary with a small `cmd/dash14` entrypoint and focused internal packages for `config`, `logging`, `storage`, `game`, `telegram`, `overlay`, and `importer`. Keep overlay templates separate for planned and live modes, serve both through `/overlay`, and use `/api/overlay` as the shared JSON source with `overlay_mode` and `overlay_revision` driving full-page reloads.

**Tech Stack:** Go 1.26, `github.com/go-telegram/bot`, `gorm.io/gorm`, `gorm.io/driver/sqlite`, YAML config parsing, standard `net/http`, standard Go `html/template`

---

### Task 1: Scaffold the Go module and binary entrypoint

**Prompt:**
Create the initial Go project skeleton for `dash14`.

- Implement the minimal code for:
  - `go.mod` with module path matching the repo name
  - `cmd/dash14/main.go`
  - empty internal package directories:
    - `internal/config`
    - `internal/logging`
    - `internal/storage`
    - `internal/game`
    - `internal/telegram`
    - `internal/overlay`
    - `internal/importer`
  - make `main.go` parse `--config` and `--import` flags and print a placeholder startup message
- Write tests for the new code:
  - add `cmd/dash14/main_test.go` only if flag parsing logic is moved into a testable helper
- Run the tests and make sure they pass:
  - `go test ./...`
  - expected output: all packages report `ok` or `[no test files]`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Scaffold dash14 module"`

---

### Task 2: Add configuration types, YAML loading, and config example

**Prompt:**
Implement YAML config loading for runtime and import mode.

- Implement the minimal code for:
  - `internal/config/config.go`
  - `internal/config/load.go`
  - `config.example.yaml`
  - config fields required by the spec:
    - Telegram token
    - SQLite path
    - HTTP listen address
    - HTTP listen port
    - planned template path
    - live template path
    - managed logo directory
    - logging level
    - logging file path
  - validation helpers that fail fast on missing required config
  - wire config loading into `cmd/dash14/main.go`
- Write tests for the new code:
  - `internal/config/load_test.go`
  - cover valid YAML, missing required fields, and bad YAML syntax
- Run the tests and make sure they pass:
  - `go test ./internal/config ./cmd/dash14`
  - expected output: `ok` for both packages
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add YAML configuration loading"`

---

### Task 3: Recreate the reusable logging setup

**Prompt:**
Implement a small logging package that follows the `easy-recall-bot` structure closely enough for reuse, but adapted to YAML config.

- Implement the minimal code for:
  - inspect the external reference project before coding; if local code is unavailable, use the documented structure in the spec and keep the API minimal
  - `internal/logging/logging.go`
  - expose logger construction from config level/file path
  - wire logger initialization into `cmd/dash14/main.go`
- Write tests for the new code:
  - `internal/logging/logging_test.go`
  - cover invalid log level and file-backed logger initialization
- Run the tests and make sure they pass:
  - `go test ./internal/logging ./cmd/dash14`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add logging setup"`

---

### Task 4: Initialize SQLite, GORM, and schema migration

**Prompt:**
Build the database bootstrap layer.

- Implement the minimal code for:
  - `internal/storage/db.go`
  - `internal/storage/migrate.go`
  - `internal/storage/models.go`
  - create GORM models for:
    - `Team`
    - `Game`
    - `GameSet`
    - `User`
    - `AppState`
  - include `overlay_revision` on `Game`
  - open SQLite and run `AutoMigrate`
  - wire DB open + migration into `cmd/dash14/main.go`
- Write tests for the new code:
  - `internal/storage/db_test.go`
  - cover opening a temp SQLite file and migrating all tables
- Run the tests and make sure they pass:
  - `go test ./internal/storage`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add SQLite storage bootstrap"`

---

### Task 5: Implement storage repositories for teams, users, games, and app state

**Prompt:**
Add explicit storage methods instead of leaking raw GORM calls everywhere.

- Implement the minimal code for:
  - `internal/storage/teams.go`
  - `internal/storage/users.go`
  - `internal/storage/games.go`
  - `internal/storage/app_state.go`
  - methods needed by the spec:
    - upsert team by `key`
    - list/search teams
    - create/update user
    - set `subscribed`
    - check `is_admin`
    - create game
    - fetch current game
    - update current game admin
    - persist active set scores
    - persist `overlay_revision`
    - load/store `current_game_id`
- Write tests for the new code:
  - `internal/storage/teams_test.go`
  - `internal/storage/users_test.go`
  - `internal/storage/games_test.go`
  - `internal/storage/app_state_test.go`
- Run the tests and make sure they pass:
  - `go test ./internal/storage`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add storage repositories"`

---

### Task 6: Implement the YAML team importer and logo copy logic

**Prompt:**
Build import-only mode for known teams.

- Implement the minimal code for:
  - `internal/importer/importer.go`
  - `internal/importer/types.go`
  - `testdata/teams.yaml`
  - parse a YAML file with explicit `key`
  - ignore unknown keys during import
  - copy logo files into the managed logo directory
  - store relative logo paths in SQLite
  - wire `--import` mode into `cmd/dash14/main.go`
- Write tests for the new code:
  - `internal/importer/importer_test.go`
  - cover unknown keys, team upsert by `key`, and logo copy path behavior
  - store test fixtures under `internal/importer/testdata`
- Run the tests and make sure they pass:
  - `go test ./internal/importer ./cmd/dash14`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add team import mode"`

---

### Task 7: Define the core game types and scoring rules

**Prompt:**
Implement the pure match logic in the `game` package without Telegram or HTTP concerns.

- Implement the minimal code for:
  - `internal/game/types.go`
  - `internal/game/rules.go`
  - encode:
    - best-of-5 match rules
    - sets 1-4 to 25, win by 2
    - set 5 to 15, win by 2
    - 5th-set side switch at 8 points only once
    - set finishable detection
    - game finishable detection at 3 sets won
- Write tests for the new code:
  - `internal/game/rules_test.go`
  - cover normal set completion, deuce, 5th-set finish, and one-time side switch
- Run the tests and make sure they pass:
  - `go test ./internal/game`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add core match rules"`

---

### Task 8: Build the game service for state transitions

**Prompt:**
Create the orchestration layer that mutates persisted match state.

- Implement the minimal code for:
  - `internal/game/service.go`
  - `internal/game/errors.go`
  - methods for:
    - create planned game
    - start planned game
    - increment/decrement points
    - confirm set finish
    - confirm game finish
    - reverse overlay sides
    - bump `overlay_revision`
    - take over current game
  - use storage repositories instead of raw GORM in this package
- Write tests for the new code:
  - `internal/game/service_test.go`
  - cover planned-to-live transition, admin takeover, set finish confirmation, and overlay revision bumping
- Run the tests and make sure they pass:
  - `go test ./internal/game ./internal/storage`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add game state transition service"`

---

### Task 9: Add fuzzy team search for the planning wizard

**Prompt:**
Implement team search behavior for `/plan`.

- Implement the minimal code for:
  - `internal/game/search.go`
  - use a simple, deterministic fuzzy scoring approach that supports:
    - exact key/name hit
    - case-insensitive substring match
    - alias matching
    - stable ordering of results
  - return enough metadata for Telegram to decide:
    - 1 match => auto-select
    - 2-8 matches => show options
    - more than 8 => ask for refinement
- Write tests for the new code:
  - `internal/game/search_test.go`
  - cover exact match, alias match, ordering, 8-result cap behavior, and 9-plus refinement case
- Run the tests and make sure they pass:
  - `go test ./internal/game`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add fuzzy team search"`

---

### Task 10: Implement overlay JSON view-model generation

**Prompt:**
Create the shared overlay payload used by both planned and live pages.

- Implement the minimal code for:
  - `internal/overlay/viewmodel.go`
  - map current game state into one plain JSON structure containing:
    - home and guest team names
    - logo paths
    - set score
    - point score
    - current set number
    - overlay left/right placement
    - game status
    - `overlay_mode`
    - `overlay_revision`
  - keep the same payload shape for planned and live modes
- Write tests for the new code:
  - `internal/overlay/viewmodel_test.go`
  - cover no current game, planned game, live game, and 5th-set swapped sides
- Run the tests and make sure they pass:
  - `go test ./internal/overlay`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add overlay API view model"`

---

### Task 11: Implement the HTTP overlay server and `/api/overlay`

**Prompt:**
Serve the overlay API and current overlay page from the app.

- Implement the minimal code for:
  - `internal/overlay/server.go`
  - `internal/overlay/handlers.go`
  - start an `http.Server` from config
  - implement:
    - `GET /api/overlay`
    - `GET /overlay`
  - `GET /overlay` must choose the planned or live template based on current game state
  - wire the server startup into `cmd/dash14/main.go`
- Write tests for the new code:
  - `internal/overlay/handlers_test.go`
  - use `httptest` to cover `/overlay` template selection and `/api/overlay` JSON output
- Run the tests and make sure they pass:
  - `go test ./internal/overlay ./cmd/dash14`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Serve overlay over HTTP"`

---

### Task 12: Add separate planned and live overlay templates with shared JavaScript

**Prompt:**
Create the browser assets for OBS.

- Implement the minimal code for:
  - `web/templates/overlay_planned.html`
  - `web/templates/overlay_live.html`
  - `web/static/overlay.js`
  - `web/static/overlay.css`
  - keep templates separate but share the same JS polling logic
  - poll `/api/overlay`
  - update dynamic fields in place
  - trigger `window.location.reload()` when `overlay_mode` or `overlay_revision` changes
- Write tests for the new code:
  - if template rendering helpers exist, add `internal/overlay/templates_test.go`
  - otherwise add server-side tests asserting the expected template markers appear in each mode
- Run the tests and make sure they pass:
  - `go test ./internal/overlay`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add planned and live overlay templates"`

---

### Task 13: Implement subscriber and admin identity helpers for Telegram

**Prompt:**
Build the shared Telegram-facing user lifecycle helpers first.

- Implement the minimal code for:
  - `internal/telegram/users.go`
  - `internal/telegram/auth.go`
  - helper flows for:
    - `/start` create or update user and subscribe
    - `/stop` unsubscribe but keep the row
    - checking `is_admin`
    - resolving the current game admin username for status messages
- Write tests for the new code:
  - `internal/telegram/users_test.go`
  - cover subscribe, unsubscribe, and non-admin/admin authorization checks
- Run the tests and make sure they pass:
  - `go test ./internal/telegram`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add Telegram user lifecycle helpers"`

---

### Task 14: Implement `/plan` wizard state and handlers

**Prompt:**
Build the planning workflow in Telegram.

- Implement the minimal code for:
  - `internal/telegram/plan.go`
  - `internal/telegram/session.go`
  - support wizard state for:
    - waiting for home team search
    - waiting for home team choice
    - waiting for guest team search
    - waiting for guest team choice
  - connect fuzzy search from `internal/game/search.go`
  - create the planned game through the game service
  - assign current game admin automatically
- Write tests for the new code:
  - `internal/telegram/plan_test.go`
  - cover 1 match, 2-8 matches, and too-many-matches behavior
- Run the tests and make sure they pass:
  - `go test ./internal/telegram`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add planning wizard"`

---

### Task 15: Implement `/game` control thread and inline score buttons

**Prompt:**
Build the active match control message for the current game admin.

- Implement the minimal code for:
  - `internal/telegram/game.go`
  - `internal/telegram/controls.go`
  - `internal/telegram/callbacks.go`
  - `/game` behavior:
    - current admin opens a fresh control thread
    - old control thread becomes invalid
    - non-current admin gets the takeover guidance message
  - inline controls for:
    - home `-1` and `+1`
    - guest `-1` and `+1`
    - clear team labeling in button text
- Write tests for the new code:
  - `internal/telegram/game_test.go`
  - cover current-admin success, non-current-admin refusal, and control-thread invalidation bookkeeping
- Run the tests and make sure they pass:
  - `go test ./internal/telegram`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add game control thread"`

---

### Task 16: Implement special-state admin actions and takeover flow

**Prompt:**
Finish the admin control loop.

- Implement the minimal code for:
  - extend:
    - `internal/telegram/callbacks.go`
    - `internal/telegram/game.go`
  - add handlers for:
    - `Start the game`
    - `Is set finished?`
    - `Is game finished?`
    - `Reverse sides`
    - `/takeover`
  - send notifications to previous and new admin on successful takeover
  - bump `overlay_revision` on planned-to-live transition
- Write tests for the new code:
  - `internal/telegram/callbacks_test.go`
  - cover takeover notifications and planned-to-live transition behavior
- Run the tests and make sure they pass:
  - `go test ./internal/telegram ./internal/game`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add admin takeover and special actions"`

---

### Task 17: Implement subscriber broadcasts for state-changing actions

**Prompt:**
Send concise update messages to subscribed users after admin mutations.

- Implement the minimal code for:
  - `internal/telegram/broadcast.go`
  - integrate broadcasts from planning, score changes, set finish, game finish, and takeover-related game state changes where appropriate
  - ensure broadcast failures are logged and do not abort the primary state update
- Write tests for the new code:
  - `internal/telegram/broadcast_test.go`
  - cover subscribed-only delivery and failure isolation
- Run the tests and make sure they pass:
  - `go test ./internal/telegram`
  - expected output: `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Add subscriber broadcasts"`

---

### Task 18: Wire the real Telegram bot runtime

**Prompt:**
Connect all Telegram handlers to the actual bot library.

- Implement the minimal code for:
  - `internal/telegram/bot.go`
  - initialize `github.com/go-telegram/bot`
  - register:
    - `/start`
    - `/stop`
    - `/plan`
    - `/game`
    - `/takeover`
    - callback query handling for inline buttons
  - wire bot startup into `cmd/dash14/main.go`
- Write tests for the new code:
  - keep unit coverage focused on handler helpers; if bot construction needs a test, add `internal/telegram/bot_test.go`
- Run the tests and make sure they pass:
  - `go test ./...`
  - expected output: all packages `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Wire Telegram bot runtime"`

---

### Task 19: Add end-to-end smoke fixtures and docs for local testing

**Prompt:**
Make the project runnable by a new engineer without reverse engineering.

- Implement the minimal code for:
  - `testdata/config.local.yaml`
  - `testdata/teams.local.yaml`
  - `README.md`
  - document:
    - how to import teams
    - how to mark a user as admin manually in SQLite
    - how to run the app
    - how to point OBS at `http://127.0.0.1:<port>/overlay`
- Write tests for the new code:
  - no code tests required; keep this task doc-focused
- Run the tests and make sure they pass:
  - `go test ./...`
  - expected output: all packages `ok`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Document local setup and smoke test flow"`

---

### Task 20: Run the full verification pass and clean up rough edges

**Prompt:**
Do one integration-quality pass before calling the implementation complete.

- Implement the minimal code for:
  - fix any remaining package API mismatches, dead code, or obvious naming issues found while running the full suite
  - run `gofmt` on the entire repository
  - update any stale examples in `config.example.yaml` or `README.md`
- Write tests for the new code:
  - add only the missing focused tests required to cover regressions discovered during the verification pass
- Run the tests and make sure they pass:
  - `go test ./...`
  - expected output: all packages `ok`
  - optional extra confidence run: `go test -cover ./...`
- Commit with the summary of the change as a commit message:
  - `git commit -m "Finalize dash14 implementation"`

---

### Notes for the Implementer

- Follow the spec in [docs/2026-03-09-dash14-design.md](docs/2026-03-09-dash14-design.md) exactly unless a contradiction appears. If the implementation exposes a real design gap, stop and use `@brainstorming` before changing behavior.
- Keep changes DRY and YAGNI. Do not build a generic workflow engine for Telegram; only implement the wizard states and callbacks required by the current spec.
- Prefer small commits after each task. If a task grows past 5-10 minutes, split it before coding.
