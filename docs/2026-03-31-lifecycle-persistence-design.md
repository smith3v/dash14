# Lifecycle Persistence Design

## Overview

This design addresses one defect found during code review: match lifecycle
transitions are persisted in multiple independent writes, so a mid-sequence
database failure can leave the app in a logically impossible state.

The fix should keep the current package boundaries and runtime shape intact.
The app remains a single-process local Go service backed by SQLite and a
Telegram bot. The main change is to move multi-row lifecycle mutations behind
transactional repository methods so the database either commits the entire
transition or preserves the previous state.

## Goals

- Make planning, game start, set finish, next-set start, and game finish
  transitions atomic from the caller's perspective
- Prevent persisted `games`, `game_sets`, and `app_state` rows from drifting
  into combinations the UI cannot recover from
- Preserve the current command set and operator workflow
- Add tests for the failure modes behind the review finding

## Non-Goals

- Reworking the Telegram routing model
- Supporting multiple concurrent planned or active matches
- Removing persisted phase from the schema in this change
- Introducing a generic transaction abstraction across every repository

## Problem

Today the Telegram handlers orchestrate lifecycle transitions by calling several
repository methods in sequence. For example:

- `/plan` creates a `games` row and then updates `app_state.current_game_id`
- `game:start` saves the game and then creates set 1
- `game:set:start_next` saves the game and then creates the next set
- `game:set:finish` saves the finished set and then saves the updated game
- `game:finish` saves the finished game and then clears the current game pointer

Any error after the first successful write can leave a partially committed
transition behind. Because `Game.EffectivePhase` trusts the persisted `Phase`
when present, later reads do not derive a safe phase from the actual set state.
That means the control UI can become stuck in a phase that has no valid active
set, or `/plan` can be blocked by an orphan non-finished game that was never
made current.

## Alternatives Considered

### A. Keep Handler-Orchestrated Writes And Patch Recovery Logic

One option is to keep the current repository surface and add cleanup or repair
logic in the handlers after individual write failures. For example, if creating
set 1 fails after `SaveGame`, the handler could attempt a compensating write to
restore the game phase.

This is not the recommended approach. It duplicates recovery logic in every
handler branch, still leaves windows where partial state is committed, and is
hard to reason about when a second failure happens during cleanup.

### B. Stop Persisting `Game.Phase`

Another option is to derive operator phase entirely from coarse game status plus
active-set presence, removing `Phase` as a persisted source of truth. That
would reduce one class of drift.

This helps but is not sufficient. The app can still persist an orphan planned
game or a current-game pointer that does not match the intended lifecycle.
Removing `Phase` is a broader behavioral change and is unnecessary to fix this
review finding.

### C. Add Transactional Lifecycle Methods

This is the recommended approach.

- move each multi-row lifecycle transition into one repository-level
  transactional method;
- keep `Game.Phase` persisted for now, but only update it inside those atomic
  methods.

This keeps the changes local, makes invariants enforceable in one place, and
matches the existing architecture.

## Chosen Design

### Transactional Lifecycle API

`pkg/storage` should grow explicit methods for the lifecycle transitions that
currently span multiple repository calls. The exact names can be finalized
during implementation, but the shape should be close to:

- `CreatePlannedGameAndSetCurrent(game *Game) error`
- `StartGame(updatedGame *Game, initialSet *GameSet) error`
- `StartNextSet(updatedGame *Game, nextSet *GameSet) error`
- `FinishSet(updatedGame *Game, finishedSet *GameSet) error`
- `FinishGameAndClearCurrent(updatedGame *Game) error`

Each method should run in a single SQLite transaction and either commit the
entire transition or roll back completely. The Telegram layer may still compute
the next in-memory state using the `pkg/game` package, but persistence of the
result belongs in `pkg/storage`.

The repository should continue exposing simple read methods. The new
transactional methods are for write paths that must preserve cross-table
invariants.

### Invariants To Preserve

After these changes, the following should always hold after a successful
transaction:

- planned game:
  - `status=planned`
  - `phase=planned`
  - no active set exists
  - `app_state.current_game_id` points at the planned game
- set in progress:
  - `status=in_progress`
  - `phase=set_in_progress`
  - exactly one unfinished set exists for the game
- between sets:
  - `status=in_progress`
  - `phase=between_sets`
  - no unfinished set exists
- finished game:
  - `status=finished`
  - `phase=finished`
  - `app_state.current_game_id` is cleared

If the transaction fails, the previous state must remain intact.

## Telegram Handler Changes

Handlers should become thinner:

- keep authority checks and callback validation in `pkg/telegram`
- continue using `pkg/game` to compute legal lifecycle state transitions
- replace multi-step repository write sequences with one transactional storage
  call per lifecycle action

This keeps business-rule decisions in `pkg/game`, Telegram-specific messaging in
`pkg/telegram`, and cross-row persistence guarantees in `pkg/storage`.

## Error Handling

- if a transactional write fails, the handler should not edit the control
  message, enqueue overlay work, or broadcast an update
- the error should be logged with the operation name and game ID when present

## Testing Strategy

Add focused tests for the exact failure modes behind the review finding.

Storage tests:

- `/plan` equivalent transaction rolls back when setting current game fails
- `game:start` rolls back when set creation fails after game update
- `game:set:start_next` rolls back when next-set creation fails
- `game:set:finish` rolls back when saving the updated game fails after marking
  the set finished
- `game:finish` rolls back when clearing current game fails

Telegram tests:

- plan flow and game-control handlers use the transactional repository methods
- failed transactional writes do not trigger later side effects

Regression tests should continue proving the normal success paths.

## Rollout Notes

This change should be implemented in two stages:

1. add the storage transactional methods with tests;
2. update Telegram handlers to use them.

No user-facing command changes are required. The visible result is that
lifecycle transitions will either fully commit or not change the database at
all.
