# Lifecycle Persistence Design

## Overview

This design addresses one defect found during code review: match lifecycle
transitions are persisted in multiple independent writes, so a mid-sequence
database failure can leave the app in a logically impossible state.

This revision takes a simpler long-term approach:

- make all match-state mutations transactional;
- stop using `app_state.current_game_id` in the runtime read/write path;
- stop treating persisted `games.phase` as authoritative;
- enforce "at most one non-finished game" directly in SQLite.

The app remains a single-process local Go service backed by SQLite and a
Telegram bot. The package boundaries stay the same: `pkg/game` computes legal
transitions, `pkg/storage` provides persistence mechanics, and `pkg/telegram`
orchestrates operator interactions.

## Goals

- Make every match-state mutation apply completely or roll back completely
- Reduce the number of persisted lifecycle truths that can drift apart
- Keep the current command set and operator workflow intact
- Ensure admins can retry a failed mutation without manual cleanup
- Add focused tests for transaction rollback and invariant enforcement

## Non-Goals

- Reworking the Telegram routing model
- Supporting multiple concurrent planned or active matches
- Adding operator recovery commands for multi-game repair in this change
- Removing the `app_state` table or `games.phase` column from the schema in
  this change

## Problem

Today the Telegram handlers orchestrate lifecycle transitions by calling several
repository methods in sequence. For example:

- `/plan` creates a `games` row and then updates `app_state.current_game_id`
- `game:start` saves the game and then creates set 1
- `game:set:start_next` saves the game and then creates the next set
- `game:set:finish` saves the finished set and then saves the updated game
- `game:finish` saves the finished game and then clears the current game pointer

Any error after the first successful write can leave a partially committed
transition behind. The current design also stores the same lifecycle fact in
multiple places:

- `games.status`
- `games.phase`
- unfinished or finished `game_sets` rows
- `app_state.current_game_id`

Because `Game.EffectivePhase` trusts the persisted `Phase` when present, later
reads do not derive a safe phase from actual set state. Because startup and
runtime read the current game through `app_state.current_game_id`, later reads
also trust a pointer that can drift from the actual non-finished match.

The result is more state coupling than the app needs. A simpler design is to
persist only the minimum authoritative facts and derive the rest.

## Alternatives Considered

### A. Keep Handler-Orchestrated Writes And Patch Recovery Logic

One option is to keep the current repository surface and add cleanup or repair
logic in the handlers after individual write failures.

This is not recommended. It duplicates recovery logic in every handler branch,
still leaves windows where partial state is committed, and is hard to reason
about when a second failure happens during cleanup.

### B. Keep Redundant Persisted Truths But Add Lifecycle-Specific Storage Methods

Another option is to add methods such as
`CreatePlannedGameAndSetCurrent`, `StartGame`, `StartNextSet`, `FinishSet`, and
`FinishGameAndClearCurrent`.

This is safe, but it overfits the storage API to the current set of Telegram
actions and preserves the redundant `phase` and `current_game_id` truths that
caused the drift problem in the first place.

### C. Simplify The State Model And Add A Small Transaction Helper

This is the recommended approach.

- `games.status` remains the authoritative persisted lifecycle state;
- active-set existence comes from `game_sets`;
- operator-visible phase is always derived from status plus active-set presence;
- current game is the single non-finished game, enforced by SQLite;
- all match-state mutations run inside a transaction helper.

This removes unnecessary persisted coupling while still giving the app atomic
updates and retry-safe operator behavior.

## Chosen Design

### Authoritative State

After this change, the runtime should treat the following as authoritative:

- `games.status` for coarse lifecycle state
- `game_sets.is_finished` rows for whether there is an active set
- the SQLite invariant that there can be at most one non-finished game

The runtime should no longer treat these as authoritative:

- `app_state.current_game_id`
- persisted `games.phase`

`app_state.current_game_id` is removed from the runtime read/write path
immediately. The table may remain in the schema for compatibility, but the app
must not depend on it.

`games.phase` may also remain in the schema for compatibility, but runtime code
must always derive the effective phase from `games.status` plus active-set
presence instead of trusting the stored phase value.

### Current Game Selection

`pkg/storage` should define the current game as the single row whose
`status <> 'finished'`.

`GetCurrentGame()` should query that row directly instead of reading
`app_state.current_game_id`. The app must not "guess" among multiple
non-finished rows. Instead, SQLite should enforce the invariant that only one
such row can exist.

To support that, the schema should gain a SQLite-level uniqueness rule for
non-finished games. The exact SQL can be finalized during implementation, but
the intended behavior is:

- any number of finished games may exist;
- zero or one planned/in-progress game may exist;
- attempts to create a second non-finished game fail atomically.

This removes the need for a race-prone "check then set current game" flow.

### Derived Phase

Operator-visible phase should always be derived from persisted state:

- `planned` when `status=planned`
- `finished` when `status=finished`
- `set_in_progress` when `status=in_progress` and an unfinished set exists
- `between_sets` when `status=in_progress` and no unfinished set exists

`Game.EffectivePhase` should stop preferring the persisted `Phase` field.
Instead, it should always derive from authoritative facts available at read
time.

This ensures that a stale or partially written `games.phase` value cannot wedge
the UI or startup resume path.

### Transaction Helper

`pkg/storage` should add a small transaction helper for game persistence work.
The exact naming can be finalized during implementation, but the shape should
be close to:

- `WithinTx(func(repo *GameRepository) error) error`

The helper should run the callback inside one SQLite transaction and expose a
repository instance backed by the transactional `*gorm.DB`.

This design intentionally does not add one storage method per lifecycle action.
Instead, handlers may continue to:

- validate authority and callback semantics in `pkg/telegram`;
- compute legal state transitions in `pkg/game`;
- persist the resulting rows through ordinary repository methods;
- do so inside a single transaction whenever match state is mutated.

### Transaction Boundaries

Every match-state mutation should go through the transaction helper, including:

- `/plan`
- `game:start`
- `game:set:finish`
- `game:set:start_next`
- `game:finish`
- `game:reverse`

Even when a mutation currently touches only one row, it should still use the
transaction helper so the rule stays uniform and future changes do not create
special-case non-atomic paths.

The important guarantee is: if a mutation fails, the previously committed state
remains intact and the admin can safely try again.

### Invariants To Preserve

After any successful commit, the following should always hold:

- planned game:
  - exactly one non-finished game may exist in SQLite
  - `status=planned`
  - no unfinished set exists
- set in progress:
  - `status=in_progress`
  - exactly one unfinished set exists for the game
- between sets:
  - `status=in_progress`
  - no unfinished set exists
- finished game:
  - `status=finished`
  - no current game exists unless another non-finished game is created later

If the transaction fails, the previous state must remain intact.

## Telegram Handler Changes

Handlers should become simpler, but not by pushing lifecycle policy deeper into
storage.

- keep authority checks and callback validation in `pkg/telegram`
- continue using `pkg/game` to compute legal lifecycle state transitions
- run each state mutation inside the storage transaction helper
- remove reads and writes of `app_state.current_game_id`
- stop depending on persisted `games.phase`

Concretely:

- `/plan` should create the planned game inside a transaction and rely on the
  SQLite invariant to reject a second non-finished game
- `game:start` should save the updated game and create set 1 inside one
  transaction
- `game:set:finish` should save the finished set and updated game inside one
  transaction
- `game:set:start_next` should save the updated game and create the next set
  inside one transaction
- `game:finish` should save the finished game inside one transaction, with no
  app-state pointer clearing step

## Error Handling

If a transactional write fails:

- the transaction must roll back completely;
- the control message must not be edited;
- overlay work must not be enqueued;
- broadcasts must not be sent;
- the error should be logged with the operation name and game ID when present;
- the admin should receive a direct retry-safe failure message stating that the
  state was not changed and the action can be tried again.

On success, handlers should rebuild UI state from freshly committed rows rather
than relying on partially mutated in-memory structs left over from a failed
attempt.

## Testing Strategy

Add focused tests for the rollback and invariant behavior behind this review
finding.

Storage tests:

- `/plan` equivalent transaction rolls back on create failure
- `game:start` rolls back when set creation fails after game update
- `game:set:start_next` rolls back when next-set creation fails
- `game:set:finish` rolls back when saving the updated game fails after marking
  the set finished
- `game:finish` rolls back when saving the finished game fails
- SQLite rejects creation of a second non-finished game
- `GetCurrentGame()` reads the non-finished game without consulting `app_state`
- effective phase is derived from status plus active-set presence, not trusted
  persisted phase

Telegram tests:

- plan flow and game-control handlers use transactional writes
- failed transactional writes do not trigger later side effects
- failed transactional writes send the admin a retry-safe failure message
- success paths continue to update the control message, overlay, and broadcast
  from committed state only

Regression tests should continue proving the normal success paths.

## Rollout Notes

This change should be implemented in stages:

1. add the SQLite invariant and storage transaction helper with tests;
2. update storage reads to stop using `app_state.current_game_id` and to derive
   phase from authoritative state;
3. update Telegram handlers to use transactional mutations and retry-safe error
   messaging;
4. keep `app_state` and `games.phase` in the schema temporarily if that lowers
   rollout risk, but treat them as compatibility baggage only.
5. create a follow-up issue in the GitHub project to remove the now-unused
   `app_state.current_game_id` read/write path remnants and the obsolete
   persisted `games.phase` field once the new design has shipped and settled.

No user-facing command changes are required. The visible result is that match
state changes will either fully commit or not change the database at all, and
the runtime will no longer depend on drift-prone persisted pointers or phase
flags.
