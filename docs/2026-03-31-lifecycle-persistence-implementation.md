# Lifecycle Persistence Implementation Plan

**Goal:** Make match-state mutations atomic while simplifying the runtime to derive current game and phase from authoritative game rows instead of drift-prone persisted pointers.

**Architecture:** Keep rule computation in `pkg/game`, but simplify persistence semantics in `pkg/storage`: the current game is the single non-finished `games` row enforced by SQLite, operator-visible phase is derived from `games.status` plus active-set presence, and all match-state mutations run inside a small transaction helper. Then update Telegram plan and game-control flows to use transactional writes, send retry-safe failure messages to admins, and trigger side effects only from committed state.

**Tech Stack:** Go 1.26, GORM, SQLite, go-telegram/bot, table-driven tests, GitHub

---

### Task 1: Add SQLite invariant and transaction helper to game storage

**Prompt:**
Update `pkg/storage/game_repository.go` and any minimal migration code needed so the database enforces "at most one non-finished game" and the repository exposes a small transaction helper for game persistence work. Keep the repository row-oriented; do not add lifecycle-specific write methods. Add or extend tests in `pkg/storage/game_repository_test.go` to prove the SQLite invariant and the transaction helper behavior. If GORM cannot express the invariant cleanly, use the smallest raw-SQL migration needed for SQLite.

Step 1: Write the failing storage tests.

- Add a test in `pkg/storage/game_repository_test.go` that creates one planned or in-progress game and asserts creating a second non-finished game fails at the database layer.
- Add a test in `pkg/storage/game_repository_test.go` that uses the new transaction helper, performs a write inside the callback, returns an error, and asserts the database rolls back to the previous state.

Step 2: Run the storage tests to verify failure.

Run: `go test ./pkg/storage -run 'Game|AppState' -v`

Expected: FAIL because the SQLite invariant and transaction helper do not exist yet.

Step 3: Implement the minimal storage changes.

- Modify `pkg/storage/game_repository.go` to add a transaction helper such as `WithinTx(func(*GameRepository) error) error`.
- Update `pkg/storage/migrate.go` only as needed to install the SQLite uniqueness rule for non-finished games.
- Keep the rest of the repository API intact for now.

Step 4: Run the storage tests to verify pass.

Run: `go test ./pkg/storage -run 'Game|AppState' -v`

Expected: PASS.

Step 5: Commit the storage foundation.

Run:

```bash
git add pkg/storage/game_repository.go pkg/storage/game_repository_test.go pkg/storage/migrate.go
git commit -m "storage: enforce single active game"
```

---

### Task 2: Remove `app_state.current_game_id` from runtime reads and writes

**Prompt:**
Refactor the game storage read path so the runtime no longer depends on `app_state.current_game_id`. `GetCurrentGame()` should return the single non-finished game directly from `games`, and write paths should stop calling `SetCurrentGameID` and `ClearCurrentGameID`. Keep the `app_state` schema in place for now if that lowers rollout risk, but remove it from the active read/write path. Add or update tests in `pkg/storage/game_repository_test.go`, `pkg/app/runtime_test.go`, and any focused Telegram tests so they prove current-game lookup comes from the non-finished game row only.

Step 1: Write the failing tests.

- Add a repository test proving `GetCurrentGame()` returns the non-finished game without requiring any `app_state` row.
- Add a repository test proving `GetCurrentGame()` returns `nil` when all games are finished.
- Add or update runtime tests in `pkg/app/runtime_test.go` so startup resume works with a non-finished game and no `app_state` row.

Step 2: Run the affected tests to verify failure.

Run: `go test ./pkg/storage ./pkg/app -run 'Game|AppState|Runtime' -v`

Expected: FAIL because reads still depend on `app_state`.

Step 3: Implement the minimal read-path changes.

- Modify `pkg/storage/game_repository.go` so `GetCurrentGame()` queries the single non-finished game directly.
- Update `pkg/telegram/plan_flow.go` and `pkg/telegram/game_control.go` to stop calling `SetCurrentGameID` and `ClearCurrentGameID`.
- Leave the unused `AppState` model and helper methods in place only if needed to avoid unrelated churn.

Step 4: Run the affected tests to verify pass.

Run: `go test ./pkg/storage ./pkg/app -run 'Game|AppState|Runtime' -v`

Expected: PASS.

Step 5: Commit the current-game simplification.

Run:

```bash
git add pkg/storage/game_repository.go pkg/storage/game_repository_test.go pkg/app/runtime_test.go pkg/telegram/plan_flow.go pkg/telegram/game_control.go
git commit -m "storage: derive current game from active rows"
```

---

### Task 3: Make operator-visible phase fully derived

**Prompt:**
Refactor the runtime and UI to stop trusting persisted `games.phase`. Keep the field in the schema temporarily if that lowers migration risk, but make `EffectivePhase` and all consumers derive phase from `games.status` plus active-set presence. Update tests in `pkg/storage/game_repository_test.go`, `pkg/app/runtime_test.go`, and `pkg/telegram/game_control_test.go` so stale persisted phase values can no longer wedge startup or the control UI.

Step 1: Write the failing tests.

- Add a repository test proving `EffectivePhase()` ignores a stale stored `Phase` and derives from status plus active-set presence.
- Add a runtime test proving overlay selection at startup follows derived phase even when the persisted `Phase` field is inconsistent.
- Add a Telegram control test proving button rendering follows derived phase, not the stored `Phase` field.

Step 2: Run the affected tests to verify failure.

Run: `go test ./pkg/storage ./pkg/app ./pkg/telegram -run 'Phase|Runtime|GameControl' -v`

Expected: FAIL because the code still trusts persisted `Phase`.

Step 3: Implement the minimal phase changes.

- Modify `pkg/storage/game.go` so `EffectivePhase` always derives from authoritative facts.
- Update any callers in `pkg/app/runtime.go` and `pkg/telegram/game_control.go` that still assume persisted phase is trusted.
- Keep schema churn minimal; do not remove the column in this task.

Step 4: Run the affected tests to verify pass.

Run: `go test ./pkg/storage ./pkg/app ./pkg/telegram -run 'Phase|Runtime|GameControl' -v`

Expected: PASS.

Step 5: Commit the phase simplification.

Run:

```bash
git add pkg/storage/game.go pkg/storage/game_repository_test.go pkg/app/runtime.go pkg/app/runtime_test.go pkg/telegram/game_control.go pkg/telegram/game_control_test.go
git commit -m "storage: derive lifecycle phase at read time"
```

---

### Task 4: Make `/plan` transactional and retry-safe

**Prompt:**
Refactor `pkg/telegram/plan_flow.go` so planned-game creation runs inside the new game-repository transaction helper, relies on the SQLite single-active-game invariant instead of a race-prone check-then-create flow, and sends a retry-safe error message to the admin when the transaction fails. Keep overlay rendering and success messaging driven by committed state only. Update tests in `pkg/telegram/plan_flow_test.go` to cover transaction failure, duplicate active-game rejection, and no-side-effects-on-failure behavior.

Step 1: Write the failing Telegram tests.

- Add a test proving a failed transactional `/plan` write sends an admin-facing "nothing changed, try again" style message and does not render overlays.
- Add a test proving attempting to plan a second non-finished game is rejected cleanly from the DB-backed invariant path.
- Add a test proving the success path still creates the planned game and renders from committed state.

Step 2: Run the Telegram tests to verify failure.

Run: `go test ./pkg/telegram -run 'Plan' -v`

Expected: FAIL because `/plan` still orchestrates non-transactional writes and pre-check logic.

Step 3: Implement the minimal plan-flow refactor.

- Modify `pkg/telegram/plan_flow.go` to use `WithinTx`.
- Remove the `GetNonFinishedGame` pre-check if it is no longer needed for correctness.
- Keep user-visible success behavior unchanged except for retry-safe failure messaging.

Step 4: Run the Telegram tests to verify pass.

Run: `go test ./pkg/telegram -run 'Plan' -v`

Expected: PASS.

Step 5: Commit the transactional plan flow.

Run:

```bash
git add pkg/telegram/plan_flow.go pkg/telegram/plan_flow_test.go
git commit -m "telegram: make planning atomic"
```

---

### Task 5: Make game-control mutations transactional and retry-safe

**Prompt:**
Refactor `pkg/telegram/game_control.go` so every match-state mutation path (`game:start`, score-affecting lifecycle actions, `game:set:finish`, `game:set:start_next`, `game:finish`, and `game:reverse`) runs inside the game-repository transaction helper. On transactional failure, do not edit the control message, do not enqueue overlay work, do not broadcast, and send the admin a retry-safe failure message. On success, rebuild UI from committed rows only. Update `pkg/telegram/game_control_test.go` to cover rollback-sensitive failure paths and committed-state-only side effects.

Step 1: Write the failing Telegram tests.

- Add tests proving transactional failure in each multi-write path leaves state unchanged and produces no later side effects.
- Add a test proving the admin receives a retry-safe failure message when a mutation fails.
- Add or update tests proving success paths still edit the control message, enqueue overlay work, and broadcast only after the transaction succeeds.

Step 2: Run the Telegram tests to verify failure.

Run: `go test ./pkg/telegram -run 'GameControl' -v`

Expected: FAIL because the handler still writes rows sequentially and can trigger side effects after partial failure.

Step 3: Implement the minimal game-control refactor.

- Modify `pkg/telegram/game_control.go` so all match-state mutations go through `WithinTx`.
- Reload or rebuild state from committed rows after success before editing the control message.
- Keep scoring and lifecycle rule computation in `pkg/game`.

Step 4: Run the Telegram tests to verify pass.

Run: `go test ./pkg/telegram -run 'GameControl' -v`

Expected: PASS.

Step 5: Commit the transactional control flow.

Run:

```bash
git add pkg/telegram/game_control.go pkg/telegram/game_control_test.go
git commit -m "telegram: make game control mutations atomic"
```

---

### Task 6: Review, full regression, docs cleanup, and follow-up issue creation

**Prompt:**
Run the full repository test suite after all prior tasks are merged. Review the implementation against the approved design, update any affected docs if they drifted, and create the follow-up GitHub project issue for removing obsolete schema/runtime baggage later (`app_state.current_game_id` remnants and the unused persisted `games.phase` field). Then review the branch against `main`, fix any real findings, and rerun the full test suite.

Step 1: Run the full test suite.

Run: `go test ./...`

Expected: PASS.

Step 2: Review the diff for scope.

Run: `git diff --stat main...`

Expected: only storage, telegram, runtime, tests, docs, and any required migration changes are present.

Step 3: Create the cleanup follow-up issue.

- Use the GitHub workflow for the current repo to open a project issue describing removal of obsolete `app_state.current_game_id` runtime baggage and the unused persisted `games.phase` field after the new model has shipped safely.

Step 4: Review the branch against `main`, fix any real findings, and rerun impacted tests.

- Inspect the diff with a code-review mindset.
- Address any real issues found.
- Rerun the impacted test packages, then rerun `go test ./...`.

Step 5: Commit the final cleanup and doc alignment.

Run:

```bash
git add docs/2026-03-31-lifecycle-persistence-design.md docs/2026-03-31-lifecycle-persistence-implementation.md README.md pkg/app pkg/storage pkg/telegram
git commit -m "docs: finalize lifecycle persistence rollout"
```

---
