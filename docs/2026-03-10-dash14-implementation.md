# Dash14 Implementation Plan

**Goal:** Build the first working version of `dash14`: a local Go application that stores one volleyball match in SQLite, renders OBS overlay HTML from templates, and exposes control through a Telegram bot.

**Architecture:** Keep `cmd/dash14` thin and put behavior into focused packages: `config`, `logging`, `storage`, `game`, `overlay`, `importer`, and `telegram`. Keep match rules pure in `game`, persistence in `storage`, and Telegram/update orchestration in `telegram` so most behavior is testable without the network.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3`, `log/slog`, GORM with SQLite (`gorm.io/gorm`, `gorm.io/driver/sqlite`), `html/template`, and a Telegram Bot API client such as `github.com/go-telegram/bot`.

---

Use `docs/2026-03-09-dash14-design.md` as the product contract. If a task reveals a design gap large enough to change behavior, stop and use `@brainstorming` before changing the contract.

For every task below:
1. Implement only the named scope.
2. Add or update tests in the same task.
3. Run `gofmt -w` on touched Go files before testing.
4. Run the listed test command and expect exit code `0` with only `ok` or `[no test files]` lines.
5. Run `/review` on the task diff, fix every finding immediately, and rerun the same test command.
6. Create exactly one commit for the task with a subject line at or under 72 characters and a detailed body that explains what changed and why.

### Task 1: Bootstrap the Go module and CLI startup flags

**Prompt:**
1. Create `go.mod` using the repository's real module path, then add `cmd/dash14/main.go` and `cmd/dash14/options.go`. Support exactly two startup forms: `dash14 --config config.yaml` and `dash14 --config config.yaml --import teams.yaml`. Do not add subcommands. Keep `main.go` thin by delegating to a `run(ctx, options)` function.
2. Add `cmd/dash14/options_test.go` covering valid runtime mode, valid import mode, missing `--config`, and unexpected positional arguments. Usage errors should return a non-zero exit path and a readable message.
3. Run `go test ./cmd/dash14`.
4. Run `/review`, fix the findings, and rerun `go test ./cmd/dash14`.
5. Commit with subject `cli: add dash14 startup modes` and a body describing why the app uses flags instead of subcommands.

---

### Task 2: Load and validate YAML configuration

**Prompt:**
1. Update `.gitignore` to exclude local runtime artifacts: `config.yaml`, `*.db`, `*.db-shm`, `*.db-wal`, `out/`, and `var/logos/`. Create `config/config.go`, `config/load.go`, and `config.example.yaml`. The config should include Telegram token, SQLite path, planned template path, live template path, overlay output path, managed logo directory, logging level, and logging file path.
2. Implement `config.Load(path string) (Config, error)` using `gopkg.in/yaml.v3` with strict top-level validation so typos fail fast. Add `ValidateRuntime()` and `ValidateImport()` methods so import mode can skip Telegram-specific runtime requirements if needed.
3. Add `config/load_test.go` for valid YAML, missing required fields, strict unknown-key rejection, and the runtime vs. import validation split.
4. Run `go test ./config`.
5. Run `/review`, fix the findings, rerun `go test ./config`, and commit with subject `config: load and validate yaml settings`.

---

### Task 3: Add structured logging setup

**Prompt:**
1. Create `logging/logger.go` so the package builds a `*slog.Logger` from config level and optional file path. If `Logging.FilePath` is set, create the parent directory and open the file in append mode; otherwise log to stderr. Return any cleanup needed by the caller.
2. Add `logging/logger_test.go` to cover valid levels, invalid levels, and writing to a temp file.
3. Run `go test ./logging`.
4. Run `/review`, fix the findings, rerun `go test ./logging`, and commit with subject `logging: add slog logger construction`.

---

### Task 4: Open SQLite and add the migration harness

**Prompt:**
1. Create `storage/db.go` and `storage/migrate.go`. Implement `storage.Open(path string) (*gorm.DB, error)` using SQLite, enable `PRAGMA foreign_keys = ON`, and create parent directories for the database file. Add a `storage.Migrate(db *gorm.DB) error` function even if the model list is still small at this point.
2. Add `storage/db_test.go` that opens a temp database, verifies the connection works, and verifies `Migrate` returns nil on a fresh database.
3. Run `go test ./storage`.
4. Run `/review`, fix the findings, rerun `go test ./storage`, and commit with subject `storage: add sqlite bootstrap and migrations`.

---

### Task 5: Implement team storage and search

**Prompt:**
1. Add `storage/team.go`, `storage/team_repository.go`, and update `storage/migrate.go`. The `Team` model should include `ID`, `Key`, `Name`, `ShortName`, `LogoPath`, `Aliases`, `CreatedAt`, and `UpdatedAt`. Store aliases with GORM JSON serialization so import and fuzzy matching can round-trip cleanly without a second table.
2. Implement repository methods for `UpsertTeam`, `GetTeamByKey`, `GetTeamByID`, and `SearchTeams(query string, limit int)`. The search must rank exact matches first, then prefix matches, then contains matches across `name`, `short_name`, and aliases.
3. Add `storage/team_repository_test.go` covering upsert-by-key, alias persistence, and ranked search behavior for 1 match, 2-8 matches, and more than 8 matches.
4. Run `go test ./storage -run Team`.
5. Run `/review`, fix the findings, rerun `go test ./storage -run Team`, and commit with subject `storage: add team persistence and search`.

---

### Task 6: Implement user storage and subscription state

**Prompt:**
1. Add `storage/user.go` and `storage/user_repository.go`, and update `storage/migrate.go`. The `User` model should include `TelegramUserID`, `Username`, `Subscribed`, `IsAdmin`, `CreatedAt`, and `UpdatedAt`, with a uniqueness constraint on `TelegramUserID`.
2. Implement repository methods for `UpsertTelegramUser`, `SetSubscription`, `GetUserByTelegramID`, and `ListSubscribedUsers`.
3. Add `storage/user_repository_test.go` covering initial insert, username updates on repeated `/start`, unsubscribe-on-`/stop`, and listing only subscribed users.
4. Run `go test ./storage -run User`.
5. Run `/review`, fix the findings, rerun `go test ./storage -run User`, and commit with subject `storage: add user and subscription state`.

---

### Task 7: Implement game, set, and app-state persistence

**Prompt:**
1. Add `storage/game.go`, `storage/game_repository.go`, `storage/app_state.go`, and update `storage/migrate.go`. Model `Game`, `GameSet`, and `AppState` with the fields from the design doc, including status enums, current set number, overlay-side flags, and `current_admin_user_id`.
2. Implement repository methods for `CreateGame`, `CreateSet`, `GetCurrentGame`, `GetGameByID`, `GetActiveSet`, `SaveGame`, `SaveSet`, `SetCurrentGameID`, and `ClearCurrentGameID`. Keep repository methods small and transactional where the update touches both a game and a set.
3. Add `storage/game_repository_test.go` covering planned-game creation, active-set lookup, and singleton `AppState` updates.
4. Run `go test ./storage -run 'Game|AppState'`.
5. Run `/review`, fix the findings, rerun `go test ./storage -run 'Game|AppState'`, and commit with subject `storage: add game and app state records`.

---

### Task 8: Add overlay templates and atomic HTML rendering

**Prompt:**
1. Create `templates/planned.html.tmpl` and `templates/live.html.tmpl` using `overlay-example.html` as the visual baseline, but split planned and live states cleanly. Create `overlay/view_model.go` and `overlay/renderer.go`. The renderer should read template files from config paths, execute them with a presentation-focused view model, and write the output atomically with a temp file plus rename.
2. Add `overlay/renderer_test.go` with temp-directory tests for planned rendering, live rendering, and atomic replacement of an existing output file.
3. Run `go test ./overlay`.
4. Run `/review`, fix the findings, rerun `go test ./overlay`, and commit with subject `overlay: render planned and live html`.

---

### Task 9: Parse team import YAML with unknown-key tolerance

**Prompt:**
1. Create `importer/teams_yaml.go`, `importer/testdata/teams-valid.yaml`, `importer/testdata/teams-missing-key.yaml`, and `importer/teams_yaml_test.go`. Model the import file as a list of teams containing `key`, `name`, `short_name`, `logo`, and `aliases`. Unknown keys inside each team record must be ignored.
2. Implement a parser that requires `key`, preserves aliases, and returns clear line-oriented errors when a required field is missing.
3. Run `go test ./importer -run YAML`.
4. Run `/review`, fix the findings, rerun `go test ./importer -run YAML`, and commit with subject `importer: parse team yaml files`.

---

### Task 10: Copy logo assets and upsert imported teams

**Prompt:**
1. Create `importer/logo_store.go`, `importer/importer.go`, and `importer/importer_test.go`. Copy source logos into the managed logo directory from config using a stable filename derived from the team key and source extension. Store only the relative path in SQLite.
2. Use the storage team repository to upsert teams by `key`, replacing metadata and logo path when the YAML changes. Keep logo-copy logic separate from YAML parsing so both are easy to test.
3. Add temp-directory tests covering logo copy, relative-path persistence, and re-import of an existing team.
4. Run `go test ./importer`.
5. Run `/review`, fix the findings, rerun `go test ./importer`, and commit with subject `importer: upsert teams and manage logos`.

---

### Task 11: Implement scoring rules for sets 1 through 4

**Prompt:**
1. Create `game/types.go`, `game/scoring.go`, and `game/scoring_test.go`. Implement pure functions that apply `+1` and `-1` score changes to the active set, prevent scores from going below zero, and report whether the set is finishable under the 25-point, win-by-2 rule for sets 1-4.
2. Keep this task focused on score math only. Do not create the next set or finish the game yet.
3. Add table-driven tests for normal scoring, deuce scoring, decrement behavior, and finishable state detection.
4. Run `go test ./game -run Scoring`.
5. Run `/review`, fix the findings, rerun `go test ./game -run Scoring`, and commit with subject `game: add standard-set scoring rules`.

---

### Task 12: Extend the scoring engine for the fifth set

**Prompt:**
1. Extend `game/scoring.go` and `game/scoring_test.go` so set 5 uses the 15-point, win-by-2 rule and automatically flips overlay sides when either team reaches 8 points. The side-switch signal must be emitted at most once.
2. Make the side-switch behavior explicit in the result type so Telegram and overlay code can react without re-deriving the rule.
3. Add tests for an exact 8-point trigger, deuce after 8, and protection against a second automatic side switch.
4. Run `go test ./game -run Scoring`.
5. Run `/review`, fix the findings, rerun `go test ./game -run Scoring`, and commit with subject `game: handle fifth-set side switching`.

---

### Task 13: Implement match lifecycle transitions

**Prompt:**
1. Add `game/lifecycle.go` and `game/lifecycle_test.go`. Implement pure transitions for `StartPlannedGame`, `ConfirmSetFinished`, `ConfirmGameFinished`, and `ReverseOverlaySides`. `ConfirmSetFinished` must mark the set finished, increment sets won, swap overlay sides for the next set, create the next set when needed, and emit whether the match is now ready for finish confirmation.
2. Keep the rule that the app never auto-finishes the match. When a team reaches 3 sets won, return a result that tells the caller to prompt for game finish instead of mutating the game to `finished`.
3. Add tests for start-of-game setup, next-set creation after a confirmed set finish, prompt-to-finish-game at 3 sets won, and manual reverse of overlay sides.
4. Run `go test ./game`.
5. Run `/review`, fix the findings, rerun `go test ./game`, and commit with subject `game: add match lifecycle transitions`.

---

### Task 14: Add Telegram client bootstrap and routing

**Prompt:**
1. Create `telegram/bot.go`, `telegram/router.go`, and `telegram/router_test.go`. Wrap the chosen Telegram library behind a small interface that covers the operations this app needs: sending messages, editing messages, answering callback queries, and registering update handlers.
2. Register command entry points for `/start`, `/stop`, `/plan`, `/game`, and `/takeover`, but keep their handlers as stubs for now. The tests should use a fake client so no real network calls are made.
3. Run `go test ./telegram -run Router`.
4. Run `/review`, fix the findings, rerun `go test ./telegram -run Router`, and commit with subject `telegram: add bot bootstrap and router`.

---

### Task 15: Implement subscriber flow, admin guard, and broadcasts

**Prompt:**
1. Create `telegram/start_stop.go`, `telegram/auth.go`, `telegram/broadcast.go`, and `telegram/start_stop_test.go`. Implement `/start` to upsert the user and mark them subscribed, `/stop` to mark them unsubscribed, and an admin guard helper that checks `users.is_admin` before mutating state.
2. Implement a broadcast helper that sends concise text updates to all subscribed users and logs per-user failures without aborting the whole broadcast.
3. Add tests for `/start`, `/stop`, non-admin rejection, and partial broadcast failure handling.
4. Run `go test ./telegram -run 'Start|Stop|Auth|Broadcast'`.
5. Run `/review`, fix the findings, rerun `go test ./telegram -run 'Start|Stop|Auth|Broadcast'`, and commit with subject `telegram: add subscribers and auth guards`.

---

### Task 16: Implement `/plan` search and selection flow

**Prompt:**
1. Create `telegram/plan_flow.go` and `telegram/plan_flow_test.go`. Build the admin-only `/plan` wizard that asks for the home team first, then the guest team, using the storage team search method and a small in-memory conversation state keyed by Telegram user ID.
2. Follow the design exactly: auto-select a single match, show selectable options for 2-8 matches, and ask the admin to refine the query when there are more than 8 results. Keep home/guest identity separate from overlay left/right presentation.
3. Add tests for the 1-match, 2-8-match, and >8-match branches, plus a non-admin trying `/plan`.
4. Run `go test ./telegram -run Plan`.
5. Run `/review`, fix the findings, rerun `go test ./telegram -run Plan`, and commit with subject `telegram: add plan team search wizard`.

---

### Task 17: Finish `/plan` by creating the planned game

**Prompt:**
1. Extend `telegram/plan_flow.go` and add any small helper needed in `storage/game_repository.go` or `overlay/renderer.go`. When the guest team is selected, create a new `planned` game, set the planning admin as `current_admin_user_id`, store the current game in `app_state`, and immediately render the planned overlay.
2. Reject `/plan` when a non-finished game already exists. The response should explain why the command is blocked.
3. Add tests for successful game creation, current-admin assignment, planned-overlay rendering, and rejection when another non-finished game exists.
4. Run `go test ./telegram -run Plan` and `go test ./storage -run 'Game|AppState'`.
5. Run `/review`, fix the findings, rerun the same commands, and commit with subject `telegram: create planned games from wizard`.

---

### Task 18: Implement `/game` control message ownership checks

**Prompt:**
1. Create `telegram/game_control.go` and `telegram/game_control_test.go`. Implement `/game` so the current game admin gets a fresh control message and the previous control thread is invalidated. If another admin already owns the game, reply with the takeover guidance from the design doc instead of giving write access.
2. Render the control message so it clearly shows home-team and guest-team score controls, even if some buttons are still no-ops at the end of this task.
3. Add tests for current-admin access, replacement of an older control thread, and the non-owner response.
4. Run `go test ./telegram -run GameControl`.
5. Run `/review`, fix the findings, rerun `go test ./telegram -run GameControl`, and commit with subject `telegram: add game control thread ownership`.

---

### Task 19: Wire score controls, finish prompts, and broadcasts

**Prompt:**
1. Extend `telegram/game_control.go` so inline buttons can start a planned game, increment or decrement either team's score, show `Is set finished?` when the current set is finishable, show `Is game finished?` when a team reaches 3 sets won, and reverse overlay sides manually when needed.
2. Persist state changes through the storage repositories, apply the pure `game` transitions, rerender the overlay after each successful mutation, update the control message in place, and broadcast concise read-only updates to subscribed users.
3. Add tests for score button routing, set-finish confirmation, game-finish prompting without auto-finish, and broadcast text generation.
4. Run `go test ./telegram -run GameControl`, `go test ./game`, and `go test ./overlay`.
5. Run `/review`, fix the findings, rerun the same commands, and commit with subject `telegram: connect controls to scoring flow`.

---

### Task 20: Implement `/takeover` and invalidate stale control threads

**Prompt:**
1. Create `telegram/takeover.go` and `telegram/takeover_test.go`. Implement `/takeover` so one admin can claim the current planned or active game, update `current_admin_user_id`, notify both the previous admin and the new admin, and invalidate the old control thread.
2. Keep the contract from the design doc: takeover changes ownership, but the new admin must still run `/game` to open a fresh control message.
3. Add tests for successful takeover, rejection when there is no current game, and rejection when the caller is not an admin.
4. Run `go test ./telegram -run Takeover`.
5. Run `/review`, fix the findings, rerun `go test ./telegram -run Takeover`, and commit with subject `telegram: add admin takeover flow`.

---

### Task 21: Wire the full application startup and resume path

**Prompt:**
1. Create `app/runtime.go`, `app/runtime_test.go`, and update `cmd/dash14/main.go`. The runtime path must load config, initialize logging, open SQLite, run migrations, construct repositories and services, load the current game if one exists, render the overlay immediately, and then start receiving Telegram updates. The import path must run the same early bootstrap steps, execute the importer, and exit without starting the bot.
2. Keep all startup failures fail-fast with clear error wrapping: invalid config, logging setup failure, SQLite open failure, migration failure, invalid template path, and import parse failure.
3. Add smoke tests that build the runtime graph with fakes for Telegram and temp directories for SQLite and overlay output. Verify import mode exits after import and runtime mode renders the current overlay before blocking on updates.
4. Run `go test ./...`.
5. Run `/review`, fix the findings, rerun `go test ./...`, and commit with subject `app: wire runtime and import startup paths`.

