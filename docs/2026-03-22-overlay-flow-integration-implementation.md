# Overlay Flow Integration Implementation Plan

**Goal:** Implement an explicit between-sets game phase, render intermission on the main overlay between sets, add a separate finished overlay/template, and align Telegram controls and broadcasts with the new match flow.

**Architecture:** Extend the persisted `Game` record with a new `phase` field and teach the domain layer to treat `between_sets` as an explicit state rather than auto-creating the next set. Then update overlay rendering, runtime startup re-rendering, and Telegram control callbacks to select the main overlay and available actions directly from the persisted phase. Keep the finished screen fully independent by giving it its own config path, template cache entry, view model, and renderer method.

**Tech Stack:** Go, GORM, SQLite, go-telegram/bot, Go HTML templates, table-driven tests

---

### Task 1: Add finished-template config and renderer surface

**Prompt:**
Update the overlay configuration and renderer API to support a separate finished overlay template before changing any match-flow logic. Touch `pkg/config/config.go` to add `FinishedTemplatePath string` under `config.OverlayConfig` with YAML key `finished_template_path`. Update `pkg/config/load.go` so runtime validation requires `overlay.finished_template_path`. Update `pkg/config/load_test.go` to cover the new required field. Update `config.example.yaml` and `README.md` so the finished template path appears anywhere the other overlay template paths are documented.

Then update `pkg/overlay/view_model.go` to add a distinct `FinishedViewModel` type, even if it initially mirrors the intermission fields. Update `pkg/overlay/renderer.go` to cache and load a fourth template, expose `RenderFinished(vm FinishedViewModel) error`, and render it atomically to the same main `overlay.output_path`. Update `pkg/telegram/router.go` so the `OverlayRenderer` interface includes `RenderFinished(...)`. Update any affected fakes in `pkg/telegram/plan_flow_test.go` and `pkg/telegram/game_control_test.go`.

Step 1: Write failing tests in `pkg/config/load_test.go` and `pkg/overlay/renderer_test.go` for the required finished template config and `RenderFinished(...)`.

Step 2: Run `go test ./pkg/config ./pkg/overlay ./pkg/telegram`
Expected: FAIL because the finished template field, view model, renderer method, and interface do not exist yet.

Step 3: Implement the minimal config, view-model, renderer, and test-fake changes in:
- `pkg/config/config.go`
- `pkg/config/load.go`
- `pkg/overlay/view_model.go`
- `pkg/overlay/renderer.go`
- `pkg/telegram/router.go`
- `config.example.yaml`
- `README.md`

Step 4: Run `go test ./pkg/config ./pkg/overlay ./pkg/telegram`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/config pkg/overlay pkg/telegram config.example.yaml README.md` and address any obvious API or doc mismatches.

Step 6: Commit with message `Add finished overlay template support`

---

### Task 2: Add persisted game phase to storage and migration coverage

**Prompt:**
Introduce the explicit persisted game phase on the existing `Game` record. Update `pkg/storage/game.go` to add a `GamePhase` type and constants for `planned`, `set_in_progress`, `between_sets`, and `finished`, plus a `Phase GamePhase` field on `Game`. Keep the existing single-row-per-match structure. Update `pkg/storage/migrate.go` only as needed for `AutoMigrate` to include the new column. Update repository tests in `pkg/storage/game_repository_test.go` to assert the phase round-trips through create/save/load operations.

Add migration/recovery-focused tests in `pkg/app/runtime_test.go` or `pkg/storage/game_repository_test.go` that construct representative legacy-style game rows and verify the app can interpret persisted phase correctly once the new field exists. Keep the migration logic minimal: the codebase uses `AutoMigrate`, so the main risk is default values and how the runtime behaves when phase is missing/zero on older rows. Define the exact fallback behavior in code and tests.

Step 1: Write failing tests for `Game.Phase` persistence and any fallback/default behavior you decide to support for pre-phase rows.

Step 2: Run `go test ./pkg/storage ./pkg/app`
Expected: FAIL because `GamePhase` and the persisted field do not exist yet.

Step 3: Implement the minimal storage model changes in:
- `pkg/storage/game.go`
- `pkg/storage/migrate.go`
- any small helper needed in `pkg/app/runtime.go` or a storage helper file to interpret missing phase values safely

Step 4: Run `go test ./pkg/storage ./pkg/app`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/storage pkg/app` and address schema-default or backward-compatibility issues.

Step 6: Commit with message `Persist explicit game phases`

---

### Task 3: Refactor domain lifecycle to support between-sets explicitly

**Prompt:**
Update the pure game-state rules in `pkg/game` so a set finish no longer auto-starts the next set. Add phase awareness to the domain types in `pkg/game/types.go` and update `pkg/game/lifecycle.go` accordingly. `StartPlannedGame` should move from `planned` to `set_in_progress` and create set 1. `ConfirmSetFinished` should require `set_in_progress`, mark the current set finished, update sets won, and move the match to `between_sets` without creating the next set. Add a new `StartNextSet` transition that requires `between_sets`, increments `CurrentSetNumber`, performs any side swap for the new set, and returns the next set state. `ConfirmGameFinished` should require a safe non-live phase and move the match to `finished`.

Update `pkg/game/lifecycle_test.go` and any helper fixtures to cover the new legal and illegal transitions, including fifth-set side-switch handling now happening when the next set starts instead of when the previous set ends.

Step 1: Write failing lifecycle tests for:
- `planned -> set_in_progress`
- `set_in_progress -> between_sets`
- `between_sets -> set_in_progress`
- `between_sets -> finished`
- illegal finish/start transitions

Step 2: Run `go test ./pkg/game`
Expected: FAIL because the domain types and transitions still reflect the old auto-next-set model.

Step 3: Implement the minimal domain changes in:
- `pkg/game/types.go`
- `pkg/game/lifecycle.go`
- `pkg/game/scoring.go` only if any phase-related helper signatures need it

Step 4: Run `go test ./pkg/game`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/game` and address any inconsistencies between domain transitions and the agreed spec.

Step 6: Commit with message `Model explicit between-set transitions`

---

### Task 4: Add finished view-model builders and phase-based main-overlay rendering

**Prompt:**
Teach the overlay and startup paths to select the main overlay directly from persisted phase. Update `pkg/overlay/model_builders.go` to add a `BuildFinishedViewModel(...)` helper and keep the finished screen independent from intermission even if it reuses similar fields. Update `pkg/app/runtime.go` so `renderCurrentOverlay(...)` uses `game.Phase` instead of the coarse `Status` checks to decide which template renders to the main `overlay.output_path`. The expected mapping is:
- `planned` -> planned main overlay
- `set_in_progress` -> live main overlay
- `between_sets` -> intermission main overlay
- `finished` -> finished main overlay

Preserve the separate intermission side output if the current renderer behavior still wants to generate it for compatibility. Update `pkg/app/runtime_test.go` and `pkg/overlay/renderer_test.go` to verify startup re-render behavior for all four phases, especially restart during `between_sets` and restart during `finished`.

Step 1: Write failing tests for phase-based main-overlay selection and finished view-model rendering.

Step 2: Run `go test ./pkg/app ./pkg/overlay`
Expected: FAIL because runtime and overlay builders still only distinguish planned/live/intermission in the old way.

Step 3: Implement the minimal rendering-path changes in:
- `pkg/overlay/model_builders.go`
- `pkg/app/runtime.go`
- any affected overlay test helpers or fixtures

Step 4: Run `go test ./pkg/app ./pkg/overlay`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/app pkg/overlay` and address any duplicated phase-to-template mapping before it spreads further.

Step 6: Commit with message `Render main overlay from game phase`

---

### Task 5: Update Telegram control layout for explicit set starts and buttonless finished state

**Prompt:**
Refactor the Telegram control view in `pkg/telegram/game_control.go` so it mirrors the persisted phase exactly. In `planned`, show only `Start the game`. In `set_in_progress`, show score buttons, `Finish the set` when finishable, and any allowed side-reversal control. In `between_sets`, hide all score buttons and show `Start next set` when the match should continue or `Finish the game` when match-end rules allow it. In `finished`, remove all inline buttons entirely and leave the message as a final status display only.

Update `buildGameControlMessage(...)` and its tests in `pkg/telegram/game_control_test.go` to assert exact button presence/absence per phase. Keep the stale-message and current-admin ownership checks intact.

Step 1: Write failing Telegram view tests covering button layout in all four phases.

Step 2: Run `go test ./pkg/telegram`
Expected: FAIL because the current control view only knows the old planned/live/finished behavior.

Step 3: Implement the minimal control-view changes in:
- `pkg/telegram/game_control.go`
- any tiny helper in `pkg/telegram` if button predicates need extraction

Step 4: Run `go test ./pkg/telegram`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/telegram` and address any button-state mismatch between the view and the domain rules.

Step 6: Commit with message `Update Telegram controls for game phases`

---

### Task 6: Rework Telegram callbacks for start-next-set and finished overlay flow

**Prompt:**
Update the callback handler in `pkg/telegram/game_control.go` so the persisted phase drives the operational flow. `game:start` should only transition a `planned` game into `set_in_progress` and create set 1. `game:set:finish` should mark the current set finished, update the game phase to `between_sets`, and must not create the next set. Add a new `game:set:start_next` callback that starts the next set from `between_sets`, creates the next `GameSet`, applies any side swap needed for the new set, and returns the game to `set_in_progress`. `game:finish` should move the game to `finished`, clear the current game pointer if that is still the intended finished behavior, and render the finished overlay on the main output.

Keep the async overlay queue and broadcast behavior aligned with the design: do not send a broadcast for entering `between_sets` or for set finish as a standalone event; do send a broadcast when the next set starts; do send a final summary when the game finishes. Update `pkg/telegram/game_control_test.go` to prove the handler no longer auto-creates the next set on set finish, does create it on `Start next set`, removes buttons in finished state, and renders the correct overlay by phase.

Step 1: Write failing callback tests for:
- set finish leaves the game in `between_sets`
- no active set exists immediately after set finish
- `Start next set` creates the next set and resumes scoring
- finished state removes buttons
- between-set transition does not broadcast
- next-set start and game finish do broadcast appropriately

Step 2: Run `go test ./pkg/telegram`
Expected: FAIL because callback handling still uses the old immediate-next-set behavior and old broadcast rules.

Step 3: Implement the minimal callback and broadcast changes in:
- `pkg/telegram/game_control.go`
- `pkg/telegram/broadcast.go` only if any helper changes are required
- any small helper used to build final-summary text

Step 4: Run `go test ./pkg/telegram`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/telegram` and address any mismatch between callback behavior, button visibility, and broadcast wording.

Step 6: Commit with message `Add explicit next-set callback flow`

---

### Task 7: Align plan flow and router test doubles with the expanded renderer contract

**Prompt:**
Clean up any remaining compile or behavior mismatches caused by the new renderer contract and persisted phase field. Review `pkg/telegram/plan_flow.go`, `pkg/telegram/plan_flow_test.go`, and any fake overlay renderers in tests to ensure `/plan` still creates a new game in `planned`, assigns the admin, renders the planned main overlay, and keeps compatibility outputs correct. Confirm that the new `Phase` field is initialized explicitly on planned-game creation rather than relying on zero values. Confirm that all fake renderers implement `RenderFinished(...)` and that no tests silently rely on the old renderer surface.

Step 1: Write or update any failing tests in `pkg/telegram/plan_flow_test.go` that now need explicit phase assertions or finished-renderer stubs.

Step 2: Run `go test ./pkg/telegram`
Expected: FAIL if any test doubles or plan-flow assumptions still reflect the old model.

Step 3: Implement the minimal cleanup in:
- `pkg/telegram/plan_flow.go`
- `pkg/telegram/plan_flow_test.go`
- any shared test helper in `pkg/telegram`

Step 4: Run `go test ./pkg/telegram`
Expected: PASS

Step 5: Run a review against `main` with `git diff main... -- pkg/telegram` and address any initialization or fake-interface drift you find.

Step 6: Commit with message `Align plan flow with renderer changes`

---

### Task 8: Review the branch against `main` and fix issues

**Prompt:**
After the implementation tasks are complete and the branch is green locally, run an explicit review against `main` and fix any findings before the final documentation pass. Focus the review on behavioral regressions, missing tests, mismatches with the approved design, and accidental scope creep. Use the local diff and the existing test suite rather than broad speculative refactors.

Step 1: Run `git diff --stat main...`
Expected: only files relevant to config, storage, game, overlay, telegram, tests, templates, and docs changed.

Step 2: Run `git diff main...`
Expected: the diff matches the approved design: explicit game phase, separate finished template path, explicit next-set start flow, buttonless finished control state, and revised broadcast behavior.

Step 3: Review the changed code with a code-review mindset and write down any findings before editing anything.

Step 4: Implement the minimal code and test changes needed to address the findings.

Step 5: Run `go test ./...`
Expected: PASS

Step 6: Commit with message `Fix review findings for overlay flow`

---

### Task 9: Full regression pass and documentation cleanup

**Prompt:**
Run the full repository test suite after all prior tasks are merged. Review the checked-in docs and user-facing configuration examples so the new behavior is discoverable and internally consistent. Update `README.md` if the runtime behavior around between-set rendering, finished overlays, or broadcast semantics is still under-documented after earlier tasks. Do not broaden the behavior beyond the approved design. The goal is to leave the repository in a coherent, documented, fully tested state.

Step 1: Run `go test ./...`
Expected: PASS

Step 2: Review `git diff --stat main...`
Expected: only files relevant to config, storage, game, overlay, telegram, tests, templates, and docs changed.

Step 3: Review these docs for consistency and update only if needed:
- `README.md`
- `config.example.yaml`
- `docs/2026-03-22-overlay-flow-integration-design.md`

Step 4: Run `go test ./...`
Expected: PASS

Step 5: Run a final review against `main` with `git diff main...` and address any remaining test gaps or accidental scope creep.

Step 6: Commit with message `Document overlay flow integration`

---
