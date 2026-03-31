# Review Findings Fixes Design

## Overview

This design addresses two defects found during code review:

1. match lifecycle transitions are persisted in multiple independent writes, so
   a mid-sequence database failure can leave the app in a logically impossible
   state;
2. subscriber broadcasts target `TelegramUserID` as the destination chat, so
   subscriptions created outside a private chat can appear successful while
   later broadcasts fail.

The fix should keep the current package boundaries and runtime shape intact.
The app remains a single-process local Go service backed by SQLite and a
Telegram bot. The main change is to move multi-row lifecycle mutations behind
transactional repository methods, and to store the Telegram chat target used for
subscriber delivery explicitly instead of inferring it from the user record.

## Goals

- Make planning, game start, set finish, next-set start, and game finish
  transitions atomic from the caller's perspective
- Prevent persisted `games`, `game_sets`, and `app_state` rows from drifting
  into combinations the UI cannot recover from
- Make broadcast delivery target the chat that actually subscribed
- Preserve the current command set and operator workflow
- Add tests for the failure modes that produced the review findings

## Non-Goals

- Reworking the whole Telegram routing model
- Supporting multiple concurrent planned or active matches
- Building a durable job queue for broadcasts
- Introducing a generic transaction abstraction across every repository

## Problem 1: Lifecycle Persistence Is Split Across Multiple Writes

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

## Problem 2: Subscriptions Do Not Persist the Delivery Chat

The user table stores a Telegram user identity and a `Subscribed` flag, but no
chat target. Broadcast fan-out later sends to `ChatID = TelegramUserID`. That
works only when the intended destination is a private DM with the same numeric
identifier. If a user subscribes in a group or channel context, the bot records
the subscription but does not retain the chat that should receive updates.

This leaves the app with misleading behavior: `/start` confirms subscription,
yet later broadcasts may fail because the stored destination is not the
subscribed chat.

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
Removing `Phase` is a broader behavioral change and is unnecessary to fix the
reviewed defects.

### C. Add Transactional Lifecycle Methods And Persist Broadcast Chat Targets

This is the recommended approach.

- move each multi-row lifecycle transition into one repository-level
  transactional method;
- keep `Game.Phase` persisted for now, but only update it inside those atomic
  methods;
- add an explicit subscription chat target to storage and use it for broadcast
  fan-out.

This keeps the changes local, makes invariants enforceable in one place, and
matches the existing architecture.

## Chosen Design

### Transactional Lifecycle API

`pkg/storage` should grow explicit methods for the lifecycle transitions that
currently span multiple repository calls. The exact names can be finalized
during implementation, but the shape should be close to:

- `CreatePlannedGameAndSetCurrent(game *Game) error`
- `StartGame(gameID uint, updatedGame *Game, initialSet *GameSet) error`
- `StartNextSet(gameID uint, updatedGame *Game, nextSet *GameSet) error`
- `FinishSet(gameID uint, finishedSet *GameSet, updatedGame *Game) error`
- `FinishGameAndClearCurrent(gameID uint, updatedGame *Game) error`

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

### Broadcast Delivery Target

Storage should persist the chat used for subscription. The simplest design is to
extend `storage.User` with a `SubscriptionChatID int64` field.

Behavior:

- `/start` upserts the user profile and records `SubscriptionChatID` from
  `update.Message.Chat.ID`, then marks the user subscribed
- `/stop` keeps using the current chat and may also refresh the stored
  `SubscriptionChatID` so the latest subscription context is remembered
- broadcasts send to `SubscriptionChatID`
- subscribers with `SubscriptionChatID == 0` should be skipped defensively and
  logged at warn level

This keeps the current user-centric subscription model while making delivery
match the actual subscribed chat.

## Migration Strategy

`AutoMigrate` should add the new nullable-or-zero-default subscription chat
column without manual SQL migrations. Existing user rows will initially have
`SubscriptionChatID = 0`. That is acceptable as a transitional state:

- users who run `/start` again will get a valid chat target
- broadcast code should skip `0` targets rather than attempting delivery

No backfill is required for the first version.

## Telegram Handler Changes

Handlers should become thinner:

- keep authority checks and callback validation in `pkg/telegram`
- continue using `pkg/game` to compute legal lifecycle state transitions
- replace multi-step repository write sequences with one transactional storage
  call per lifecycle action

This keeps business-rule decisions in `pkg/game`, Telegram-specific messaging in
`pkg/telegram`, and cross-row persistence guarantees in `pkg/storage`.

## Error Handling

Lifecycle transitions:

- if a transactional write fails, the handler should not edit the control
  message, enqueue overlay work, or broadcast an update
- the error should be logged with the operation name and game ID when present

Subscriptions:

- if persisting the subscription chat fails, `/start` or `/stop` should return
  the existing generic error message
- if a broadcast encounters a subscribed user with `SubscriptionChatID == 0`,
  log and skip rather than failing the whole fan-out

## Testing Strategy

Add focused tests for the exact failure modes behind the review findings.

Storage tests:

- `/plan` equivalent transaction rolls back when setting current game fails
- `game:start` rolls back when set creation fails after game update
- `game:set:start_next` rolls back when next-set creation fails
- `game:set:finish` rolls back when saving the updated game fails after marking
  the set finished
- `game:finish` rolls back when clearing current game fails

Telegram tests:

- `/start` stores the subscription chat ID from the incoming message
- broadcasts send to the stored chat ID, not `TelegramUserID`
- a subscribed user with no stored chat is skipped

Regression tests should continue proving the normal success paths.

## Rollout Notes

This change should be implemented in two stages:

1. add the storage model and transactional repository methods with tests;
2. update Telegram handlers to use the new methods and add broadcast chat tests.

No user-facing command changes are required. The only visible behavioral change
is that subscriptions will reliably target the chat that actually subscribed,
and lifecycle transitions will either fully commit or not change the database at
all.
