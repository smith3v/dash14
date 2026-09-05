# Replace Planned Game Design

## Goal

Let an administrator replace a planned, not-yet-started game through the
existing `/plan` wizard. The bot checks the current game immediately after the
command. It stops when a game is in progress and asks for confirmation before
starting team selection when a game is merely planned.

The current planned game remains authoritative and unchanged while the admin
selects replacement teams. It is updated atomically only after both new teams
have been chosen. A game that has started cannot be replaced and must be
completed through the normal score controls.

## User flow

After the existing admin check, `/plan` reads the current game before creating
a plan state or requesting a team name.

### No current game

The bot starts the existing flow without extra confirmation:

```text
Please enter the home team name:
```

After the home and guest teams are selected, the bot creates the planned game
as it does today.

### Game in progress

The bot does not start a plan state and replies:

```text
A game is currently in progress: Croonenburg HS 3 vs Kroefi HS 1.
Finish this game before planning another one.
```

No team prompts follow, and all game and overlay state remains unchanged.

### Game planned

The bot stores a pending confirmation state and replies:

```text
A game is already planned: Croonenburg HS 3 vs Kroefi HS 1.
Would you like to plan another game instead?
```

The inline keyboard has two explicit buttons:

- `Yes, plan another`
- `No, keep current`

`No, keep current` clears the pending plan state and confirms that the current
game was kept. `Yes, plan another` verifies that the same game is still
planned, then continues with the existing home-team and guest-team selection
flow. There is no second replacement confirmation after team selection.

When both replacement teams have been chosen, the bot atomically updates the
planned game, renders the normal planned and intermission overlays for the new
teams, and replies:

```text
Planned game updated: Spaarnestad HS 11 vs Albatros.
```

The wording should follow the bot's existing English interface unless
localisation is introduced separately.

## Plan state and callbacks

Extend `planState` to distinguish a normal new plan from a replacement. A
replacement state records:

- whether it is waiting for the initial confirmation;
- the existing game's ID;
- the existing home and guest team IDs;
- the selected replacement home team, once chosen.

The original team IDs form an optimistic-concurrency fingerprint. They prevent
an older wizard from overwriting a replacement completed by another admin.

New callback data remains in the existing plan namespace:

- `plan:replace:start`
- `plan:replace:keep`

Only the admin who owns the in-memory plan state can act on its confirmation.
The handler acknowledges and ignores callbacks for missing or superseded state.
Starting `/plan` again replaces that admin's earlier pending plan state.

Before handling `plan:replace:start`, the bot re-reads the current game. If the
expected game is still planned, it clears the confirmation flag and prompts for
the home team. If that game has started, disappeared, or already been replaced,
the bot clears the state, leaves persistence untouched, and asks the admin to
run `/plan` again when appropriate.

## Persistence and consistency

A normal plan continues to create a new game only after both teams are chosen.
A replacement performs one guarded database update after both teams are chosen.
The update matches all of the following:

- the stored game ID;
- `status = planned`;
- the original home team ID;
- the original guest team ID.

It sets both new team IDs together, restores the normal left/right side
assignment, keeps the lifecycle in the planned phase, assigns control to the
admin completing the replacement, and clears the old control-message ID. The
planned-game invariants remain set to their initial values: set number 1, zero
sets won, and no fifth-set side switch.

The repository should expose a focused operation such as
`ReplacePlannedGame`, returning a conflict result when the guarded update
affects no row. A single SQL update is atomic; it may also run inside the
existing transaction boundary for consistency with game creation. No game row
is deleted, no fictional finished match is created, and no schema migration is
required.

If the current game starts while replacement teams are being selected, its
status no longer matches `planned`, so the update changes nothing. If another
admin replaces it first, the original team IDs no longer match. In either case,
the bot reports that the planned game changed and preserves the current game.

## Control-panel safety

The previous `/game` message becomes stale after the planned game's teams are
updated. The replacement update clears `ControlMessageID`. Game callbacks must
require an exact non-zero match between the callback message ID and the current
game's `ControlMessageID`. This prevents the old `Start the game` button from
starting the updated game.

The bot should also make a best-effort Telegram edit to remove the old inline
keyboard and state that the planned game was replaced. Correctness must depend
on the persisted message-ID check, not on the edit succeeding.

## Error handling

If the initial current-game lookup fails, `/plan` reports a generic error and
does not start a plan state. A failed or conflicting replacement leaves the
old planned game unchanged because the guarded update is atomic.

Overlay rendering happens only after persistence succeeds, matching the
existing planning flow. If rendering fails, the updated game remains planned
and the bot reports that the overlay needs attention. The plan state is cleared
after a successful persistence update so repeated confirmation or team
callbacks cannot apply the replacement again.

## Verification

Add Telegram handler tests for:

- `/plan` with no current game immediately requests the home team;
- `/plan` with an in-progress game identifies it and starts no wizard;
- `/plan` with a planned game identifies it and displays both buttons;
- `No, keep current` clears the wizard and changes no state or overlay;
- `Yes, plan another` continues into the unchanged team-selection flow;
- both replacement teams are selected before any persisted value changes;
- choosing both teams atomically updates the same planned game row and renders
  the new overlays;
- a game started before confirmation or during team selection is not replaced;
- a concurrent replacement makes an older wizard fail without overwriting it;
- stale or repeated plan callbacks do not alter game state;
- callbacks from the previous `/game` message cannot start the updated game.

Add repository tests for a successful guarded update, mismatched IDs,
non-planned status, and rollback or no-change behavior on failure. Run
`go test ./...` after implementation.

## Implementation plan

The file-by-file implementation sequence, focused tests, commits, and final
branch review are defined in
`docs/2026-08-30-replace-planned-game-implementation.md`.
