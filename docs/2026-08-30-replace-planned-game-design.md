# Replace Planned Game Design

## Goal

Let an administrator replace a planned, not-yet-started game while using the
existing `/plan` wizard. After selecting both new teams, the bot asks for
explicit confirmation instead of replying that another game is planned. A game
that has already started remains non-replaceable and must be completed through
the normal score controls.

## User flow

The `/plan` flow remains unchanged until the guest team has been selected. The
bot then reads the current game.

- When no game exists, it creates the new planned game exactly as it does now.
- When the current game is `planned`, it retains the selected home and guest
  teams in the admin's plan state and sends this confirmation message:

  ```text
  A game is already planned: Croonenburg HS 3 vs Kroefi HS 1.
  Replace it with Spaarnestad HS 11 vs Albatros?
  ```

  The inline keyboard contains `Yes, replace` and `No, keep current game`.
- `No` discards the in-progress `/plan` state and confirms that the current
  game was kept. It does not modify the overlay or game-control message.
- `Yes` replaces the game and replies `Planned game created: …`. The normal
  planned and intermission overlays are rendered for the new teams.
- When the current game is in progress, the bot replies that it cannot be
  replaced because it has already started. It discards the new plan; no
  existing state is changed.

The wording shown to users should follow the bot's existing English interface
unless interface localisation is introduced separately.

## State and callback design

Extend `planState` with the chosen guest team and a `waitingForReplacement`
flag. New callback data is scoped to the existing plan namespace:

- `plan:replace:yes`
- `plan:replace:no`

Only the administrator who owns that plan state may use the confirmation. A
callback with no corresponding state is ignored after its Telegram loading
indicator is acknowledged. Starting a fresh `/plan` replaces any earlier
pending state, as it does today.

At confirmation time, the handler must re-read the current game. This makes
the confirmation safe if another administrator starts the game, takes over, or
replaces it between the prompt and the button click.

## Persistence and consistency

Replacement is one database transaction:

1. Load the current non-finished game.
2. If it is still `planned`, delete that game record.
3. Create the new planned game.

The deletion is conditional on the old game's ID and `planned` status. A
planned game has no sets, so it can be removed without deleting scored match
data. This avoids inserting a fictional 0–0 finished match into the history.
If the game has changed to `in_progress`, the transaction aborts and preserves
both the game and the pending replacement request only long enough to report
the conflict; the new game is not created.

The repository should expose a focused operation for deleting a planned game,
rather than a general unguarded delete. No schema migration is needed: the
existing unique index continues to guarantee that exactly one non-finished game
exists.

## Control-panel safety

The previous `/game` control message is stale once its planned game is
replaced. Game callbacks must require an exact match between the callback's
message ID and the current game's `ControlMessageID`; a zero control-message
ID is never valid for a callback. This prevents an old `Start the game` button
from starting the replacement game. When practical, the old control message
should also be edited to remove its inline keyboard and state that the game was
replaced; correctness must not depend on that best-effort Telegram edit.

## Error handling

Database failures leave the old game intact because deletion and creation share
one transaction. The bot reports that replacement failed and asks the admin to
try again. Overlay rendering remains after the committed transaction, matching
the current planning flow: if rendering fails, the new game remains planned and
the bot reports that the overlay needs attention.

## Verification

Add table-driven Telegram handler tests for:

- a normal plan with no current game;
- planned game → prompt includes both old and new team names;
- `No` keeps the original game and produces no new game or overlay render;
- `Yes` removes the old planned game, creates the new one, and renders it;
- a current in-progress game cannot be replaced;
- a game that becomes in-progress before confirmation is not replaced;
- stale or repeated confirmation callbacks do not alter game state;
- callbacks from the old `/game` control message cannot start the replacement.

Add repository tests for the conditional planned-game deletion and transaction
rollback. Run `go test ./...` after implementation.
