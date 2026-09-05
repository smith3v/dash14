# Replace Planned Game Implementation Plan

**Goal:** Check game availability as soon as `/plan` starts and let an admin
replace a still-planned game through an explicit confirmation followed by the
existing team-selection wizard.

**Architecture:** Extend the in-memory Telegram plan state with an optional
replacement snapshot. Persist a replacement with one guarded SQL update that
matches the original game ID, status, and team IDs, so the old plan remains
unchanged until both new teams are known and concurrent starts or replacements
cannot be overwritten. Clear the old control-message ID in that same update and
reject every game callback whose message ID is not the current non-zero control
message ID.

**Tech Stack:** Go 1.26, `github.com/go-telegram/bot`, GORM, SQLite, table-driven
Go tests.

This plan implements the approved
`docs/2026-08-30-replace-planned-game-design.md`. It was prepared with
`@writing-plans`; if product behavior changes during implementation, stop and
return to `@brainstorming` before changing the approved flow.

---

### Task 1: Add the guarded planned-game repository update

**Prompt:**

Implement the persistence primitive first, using tests to define its atomicity
and conflict behavior.

1. In `pkg/storage/game_repository_test.go`, add
   `TestReplacePlannedGameUpdatesExpectedRow`. Create a planned game with
   deliberately non-default mutable fields, call the new method, and assert:
   the row ID is unchanged; both team IDs change together; sides are restored
   to `left` and `right`; scores and set wins are zero; set number is 1; status
   and phase remain `planned`; the completing admin is assigned;
   `ControlMessageID` becomes zero; and `SideSwitchedInSet5` becomes false.
   Count `games` rows and assert that replacement did not create a second row.
2. Add table-driven `TestReplacePlannedGameRejectsChangedGame` cases for a
   started game, wrong expected home team, wrong expected guest team, and wrong
   game ID. Assert `errors.Is(err, storage.ErrPlannedGameChanged)` and verify
   that every persisted field remains unchanged.
3. Add `TestReplacePlannedGameRollsBackWithTransaction`. Run a successful
   replacement inside `WithinTx`, return a sentinel error afterward, and assert
   that the original teams and control metadata are restored after rollback.
4. Run the focused tests and confirm that they initially fail because the API
   does not exist:

   ```sh
   go test ./pkg/storage -run 'TestReplacePlannedGame' -count=1
   ```

   Expected result: build failure naming the missing method or sentinel.
5. In `pkg/storage/game_repository.go`, add this package-level conflict value:

   ```go
   var ErrPlannedGameChanged = errors.New("storage: planned game changed")
   ```

6. Add a repository method with explicit expected and replacement values:

   ```go
   func (r *GameRepository) ReplacePlannedGame(
       gameID uint,
       expectedHomeTeamID uint,
       expectedGuestTeamID uint,
       newHomeTeamID uint,
       newGuestTeamID uint,
       adminUserID int64,
   ) error
   ```

   Implement it with `r.db.Model(&Game{}).Where(...)` matching `id`,
   `status = GameStatusPlanned`, `home_team_id`, and `guest_team_id`. Use one
   `Updates(map[string]any{...})` call to write the two new team IDs and all
   planned-game invariants. Wrap database errors as
   `storage: replace planned game <id>: ...`. Return
   `ErrPlannedGameChanged` when `RowsAffected != 1`.
7. Run the focused repository tests again:

   ```sh
   go test ./pkg/storage -run 'TestReplacePlannedGame' -count=1
   ```

   Expected result: `ok .../pkg/storage`.
8. Run the entire storage package to catch repository regressions:

   ```sh
   go test ./pkg/storage -count=1
   ```

   Expected result: `ok .../pkg/storage`.
9. Format and commit this isolated persistence change:

   ```sh
   gofmt -w pkg/storage/game_repository.go pkg/storage/game_repository_test.go
   git add pkg/storage/game_repository.go pkg/storage/game_repository_test.go
   git commit -m "storage: add guarded planned game replacement"
   ```

---

### Task 2: Gate `/plan` before the team wizard

**Prompt:**

Implement the immediate current-game check and the initial confirmation without
changing game persistence yet.

1. In `pkg/telegram/plan_flow_test.go`, replace the obsolete
   `TestPlanRejectedWhenNonFinishedGameExists` expectation with focused entry
   tests:

   - `TestPlanWithoutCurrentGameStartsTeamSelection` preserves the existing
     `Please enter the home team name:` behavior.
   - `TestPlanWithInProgressGameStopsBeforeTeamSelection` asserts that the
     reply names both current teams, says the match must be finished, and leaves
     no entry in `r.plans`.
   - `TestPlanWithPlannedGameRequestsReplacementConfirmation` asserts that the
     reply names both current teams and includes one inline row containing
     `Yes, plan another` with callback `plan:replace:start` and
     `No, keep current` with callback `plan:replace:keep`.
   - `TestPlanReplacementKeepClearsState` clicks the negative button, checks
     the confirmation text, and verifies that the original game and overlays
     are unchanged.
   - `TestPlanReplacementStartContinuesTeamSelection` clicks the positive
     button and expects the normal home-team prompt.
   - `TestPlanReplacementIgnoresTextBeforeConfirmation` sends plain text before
     either button is clicked and verifies that no team search response is
     produced.
2. Run these tests and confirm they fail against the old handler:

   ```sh
   go test ./pkg/telegram -run 'TestPlan(WithoutCurrent|WithInProgress|WithPlanned|Replacement)' -count=1
   ```

   Expected result: assertion failures because `/plan` currently always asks
   for the home team.
3. In `pkg/telegram/plan_flow.go`, add a private replacement snapshot and attach
   it optionally to `planState`:

   ```go
   type plannedGameReplacement struct {
       GameID                    uint
       ExpectedHomeTeamID        uint
       ExpectedGuestTeamID       uint
       PreviousControlMessageID  int
       AwaitingConfirmation      bool
   }

   type planState struct {
       HomeTeam    *storage.Team
       Replacement *plannedGameReplacement
   }
   ```

   Use idiomatic alignment from `gofmt`; the field layout above documents the
   required data rather than prescribing manual whitespace.
4. Refactor `handlePlan` so it deletes any earlier state for the admin before
   reading the current game. Handle the three outcomes exactly as specified in
   the design:

   - no game: store an empty `planState` and prompt for the home team;
   - `in_progress`: load both team records, identify the current match, send the
     blocking message, and do not store plan state;
   - `planned`: load both team records, store a replacement snapshot including
     the old control-message ID, and send the two-button confirmation.

   Treat repository or team lookup failures as `Something went wrong. Please
   try again.` with no plan state.
5. Update `handlePlanText` to ignore input while
   `Replacement.AwaitingConfirmation` is true.
6. Add exact callback branches in `handlePlanCallback` before the existing team
   callbacks:

   - `plan:replace:keep` requires an awaiting replacement, deletes the state,
     and confirms that the existing plan was kept;
   - `plan:replace:start` re-reads the current game and verifies game ID,
     `planned` status, and both expected team IDs. On success, clear only the
     awaiting flag, retain the replacement snapshot, and prompt for the home
     team. On mismatch, delete the state and tell the admin that the game
     changed and `/plan` must be run again.

   Ignore repeated, missing, or superseded confirmation callbacks after
   acknowledging the Telegram callback.
7. Run the focused tests:

   ```sh
   go test ./pkg/telegram -run 'TestPlan(WithoutCurrent|WithInProgress|WithPlanned|Replacement)' -count=1
   ```

   Expected result: `ok .../pkg/telegram`.
8. Run all existing plan-flow tests; update only assertions made obsolete by
   the approved early gate:

   ```sh
   go test ./pkg/telegram -run 'TestPlan' -count=1
   ```

   Expected result: `ok .../pkg/telegram`.
9. Format and commit the entry-flow change:

   ```sh
   gofmt -w pkg/telegram/plan_flow.go pkg/telegram/plan_flow_test.go
   git add pkg/telegram/plan_flow.go pkg/telegram/plan_flow_test.go
   git commit -m "telegram: confirm replacement before planning"
   ```

---

### Task 3: Persist and render the completed replacement

**Prompt:**

Connect the existing home/guest selection flow to the guarded repository update
after both new teams are known.

1. In `pkg/telegram/plan_flow_test.go`, add:

   - `TestPlanReplacementDoesNotPersistBeforeBothTeams` to click the positive
     confirmation and select only the home team, then assert every field of the
     old game is unchanged and no overlay was rendered;
   - `TestPlanReplacementUpdatesSameGameAndRenders` to complete both team
     selections, assert the same game ID now has both new teams and reset
     control metadata, assert one planned and one intermission render with the
     new identities, check `Planned game updated: ...`, and verify plan state is
     gone;
   - `TestPlanReplacementRejectsGameStartedDuringWizard` to change the current
     game to `in_progress` after confirmation but before guest selection, then
     verify the new teams are not written and no overlay is rendered;
   - `TestPlanReplacementRejectsConcurrentReplacement` to change the persisted
     team fingerprint before the first wizard completes, then verify the newer
     replacement wins and the stale wizard cannot overwrite it;
   - `TestPlanReplacementCallbackCannotApplyTwice` to replay the final guest
     callback and verify no additional write or render occurs;
   - `TestPlanReplacementDisablesPreviousControlMessage` to seed a non-zero old
     control ID and verify a best-effort edit removes its keyboard after a
     successful replacement.
2. Extend `txFailGames` only as needed to simulate transaction setup failure for
   replacement. Add a failure test that verifies the old planned game and both
   overlays remain unchanged when `WithinTx` fails.
3. Run the new replacement completion tests and confirm failure before wiring
   persistence:

   ```sh
   go test ./pkg/telegram -run 'TestPlanReplacement(DoesNotPersist|UpdatesSame|Rejects|Callback|Disables|Transactional)' -count=1
   ```

   Expected result: assertion failures because finalization still attempts to
   create a second game.
4. Refactor `finalizePlannedGame` in `pkg/telegram/plan_flow.go` into two small
   persistence paths plus one shared rendering path:

   - for a normal state, keep transactional `CreateGame` behavior;
   - for a replacement state, call `repo.ReplacePlannedGame(...)` inside
     `r.games.WithinTx`, passing the stored fingerprint and both selected team
     IDs;
   - translate `storage.ErrPlannedGameChanged` into a specific message that the
     game changed and was not replaced; use the existing retry-safe generic
     message for other transaction failures;
   - delete the admin's plan state immediately after successful persistence,
     before Telegram edits or overlay rendering, so repeated callbacks cannot
     apply the operation again;
   - render `PlannedViewModel` and the zero-score intermission view model through
     the existing renderer calls;
   - send `Planned game created: ...` for creation and
     `Planned game updated: ...` for replacement.
5. After a successful replacement, make a best-effort `EditMessageText` call
   for a non-zero previous control ID. Use the current chat ID, text such as
   `This planned game was replaced. Run /game for current controls.`, and an
   empty `InlineKeyboardMarkup`. Log an edit failure but do not roll back or
   report replacement failure; the persisted callback guard is authoritative.
6. Preserve the existing behavior that overlay errors do not roll back a
   successfully persisted game. Ensure error logging uses the updated game ID,
   which is the replacement ID for updates and the generated ID for creates.
7. Run the focused completion tests:

   ```sh
   go test ./pkg/telegram -run 'TestPlanReplacement(DoesNotPersist|UpdatesSame|Rejects|Callback|Disables|Transactional)' -count=1
   ```

   Expected result: `ok .../pkg/telegram`.
8. Run every plan-flow test:

   ```sh
   go test ./pkg/telegram -run 'TestPlan' -count=1
   ```

   Expected result: `ok .../pkg/telegram`.
9. Format and commit the completed replacement flow:

   ```sh
   gofmt -w pkg/telegram/plan_flow.go pkg/telegram/plan_flow_test.go
   git add pkg/telegram/plan_flow.go pkg/telegram/plan_flow_test.go
   git commit -m "telegram: atomically replace planned games"
   ```

---

### Task 4: Reject callbacks from invalidated game controls

**Prompt:**

Make the persisted control-message ID check enforce the replacement boundary.

1. In `pkg/telegram/game_control_test.go`, add
   `TestGameControlRejectsCallbackWhenControlMessageWasInvalidated`. Create a
   planned game with the calling admin, open `/game` to obtain a real control
   message ID, replace the game through the repository so its
   `ControlMessageID` becomes zero, then replay `game:start` from the old
   message. Assert the game is still `planned`, no set was created, and no live
   overlay was rendered.
2. Run the focused test and confirm that it fails under the current permissive
   zero-ID behavior:

   ```sh
   go test ./pkg/telegram -run 'TestGameControlRejectsCallbackWhenControlMessageWasInvalidated' -count=1
   ```

   Expected result: failure because the old callback starts the replacement.
3. In `pkg/telegram/game_control.go`, replace the stale-message condition with
   an exact non-zero requirement:

   ```go
   if game.ControlMessageID == 0 || messageID != game.ControlMessageID {
       // answer with the existing stale-control message and return
   }
   ```

4. Run the focused test again:

   ```sh
   go test ./pkg/telegram -run 'TestGameControlRejectsCallbackWhenControlMessageWasInvalidated' -count=1
   ```

   Expected result: `ok .../pkg/telegram`.
5. Run all game-control tests to verify that valid current controls still work:

   ```sh
   go test ./pkg/telegram -run 'TestGameControl' -count=1
   ```

   Expected result: `ok .../pkg/telegram`.
6. Format and commit the safety check:

   ```sh
   gofmt -w pkg/telegram/game_control.go pkg/telegram/game_control_test.go
   git add pkg/telegram/game_control.go pkg/telegram/game_control_test.go
   git commit -m "telegram: reject invalidated game controls"
   ```

---

### Task 5: Verify and review the complete branch

**Prompt:**

Run the project-wide checks, review every branch change against `main`, and fix
any finding before declaring the feature complete.

1. Run formatting verification on all changed Go files:

   ```sh
   test -z "$(gofmt -l pkg/storage/game_repository.go pkg/storage/game_repository_test.go pkg/telegram/plan_flow.go pkg/telegram/plan_flow_test.go pkg/telegram/game_control.go pkg/telegram/game_control_test.go)"
   ```

   Expected result: exit status 0 and no output.
2. Run static analysis:

   ```sh
   go vet ./...
   ```

   Expected result: exit status 0 and no diagnostics.
3. Run the full deterministic test suite:

   ```sh
   go test ./... -count=1
   ```

   Expected result: every package reports `ok` or `[no test files]`.
4. Check patch hygiene and inspect the complete implementation against the
   branch point:

   ```sh
   git diff --check main...HEAD
   git diff --stat main...HEAD
   git diff main...HEAD
   ```

   Expected result: no whitespace errors; the diff is limited to the approved
   storage, Telegram flow, tests, and documentation. Review specifically for
   accidental mutation before both teams are selected, unguarded concurrent
   updates, stale callback acceptance, state leaks, and changed normal-plan
   behavior.
5. If review finds an issue, add a regression test first, make the smallest
   correction, rerun the focused test and `go test ./... -count=1`, then commit
   the fix with a message describing the corrected behavior. Do not create an
   empty review commit when no change is needed.
6. Confirm the final branch state:

   ```sh
   git status --short --branch
   git log --oneline main..HEAD
   ```

   Expected result: a clean `codex/replace-planned-game` worktree and the
   feature's small, ordered implementation commits.
