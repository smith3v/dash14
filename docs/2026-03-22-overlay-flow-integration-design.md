# Overlay Flow Integration Design

## Overview

This design updates the match-control flow so the main OBS overlay follows the
real operator workflow instead of requiring scene changes between live play and
set breaks. Today the app renders a separate intermission page, but the main
overlay remains on the live scoreboard until another action changes it. That
forces the operator to switch OBS scenes manually during a busy part of the
match.

The new flow makes set breaks explicit in persisted game state. When a set is
confirmed finished, the main overlay should switch to the intermission
presentation. It should stay there until the admin explicitly starts the next
set. When the match is fully finished, the main overlay should switch to a new
finished template that shows the final match score and the completed set
history.

The intended operator flow becomes:

- planned overlay
- start set 1 -> live overlay
- finish set -> intermission overlay on the main output
- start next set -> live overlay on the main output
- repeat until the match is eligible to finish
- finish game -> finished overlay on the main output

This design deliberately models the between-set period as a first-class game
phase rather than inferring it indirectly from the absence or presence of an
active set record.

## Goals

- Show the intermission overlay on the main overlay output between sets
- Require an explicit `Start next set` action before score entry resumes
- Add a distinct finished overlay for the final match state
- Make restart behavior deterministic by persisting the operator-visible phase
- Keep Telegram control behavior aligned with the rendered overlay state

## Non-Goals

- Redesign the visual appearance of the existing planned, live, or intermission
  templates beyond what the new flow requires
- Remove the separate intermission output file if existing OBS setups still use
  it
- Introduce multi-match scheduling or concurrent active matches
- Change the underlying volleyball scoring rules

## Current Problems

The current implementation has two structural mismatches with the intended
operator workflow:

1. `ConfirmSetFinished` advances the match immediately into the next set by
   incrementing `CurrentSetNumber` and creating the next `GameSet`.
2. The main overlay output only distinguishes between planned and in-progress
   rendering, while the intermission overlay is written to a separate output
   file.

As a result, once a set is finished, the app already behaves as if the next set
exists and the main overlay does not naturally enter an intermission state.
This forces the operator to compensate in OBS instead of the application
expressing the match flow directly.

## State Model

The game record should gain an explicit persisted phase field that describes
what the operator and overlay are currently doing.

Proposed phases:

- `planned`
- `set_in_progress`
- `between_sets`
- `finished`

This phase is distinct from low-level set records. The current game phase
should answer:

- which actions the admin may perform
- which main overlay template should be rendered
- whether score buttons are enabled
- whether an active unfinished set should exist

The intended invariants are:

- `planned`: no active set exists
- `set_in_progress`: exactly one active unfinished set exists
- `between_sets`: no active unfinished set exists
- `finished`: no active unfinished set exists

This is more robust than deriving phase from a mix of `status`,
`CurrentSetNumber`, and active-set presence in multiple places.

## Match State Transitions

The domain flow should become:

1. `/plan` creates a game in `planned`
2. `Start the game` creates set 1 and transitions to `set_in_progress`
3. Score changes are allowed only in `set_in_progress`
4. `Finish the set` marks the current set finished and transitions to
   `between_sets`
5. `Start next set` creates the next set and transitions back to
   `set_in_progress`
6. `Finish the game` transitions to `finished`

Two details matter here:

First, `Finish the set` should no longer create the next set automatically. The
absence of the next set is intentional and is what keeps the match in the
between-set phase until the admin explicitly resumes play.

Second, side switching for the next set should happen when the next set starts,
not when the previous set finishes. That maps better to operator intent:
finishing a set closes what just happened; starting the next set prepares what
happens next.

## Domain Logic Changes

`pkg/game/lifecycle.go` should be updated so the state machine reflects the new
explicit phase.

Recommended shape:

- `StartPlannedGame`:
  - require `planned`
  - set phase to `set_in_progress`
  - initialise set 1
- `ConfirmSetFinished`:
  - require `set_in_progress`
  - validate finishability
  - mark the current set finished
  - update sets won
  - if the match is still continuing, set phase to `between_sets`
  - do not create the next set
- `StartNextSet`:
  - require `between_sets`
  - if the match is not already finish-eligible, increment
    `CurrentSetNumber`, perform any side swap for the new set, and create the
    next `SetState`
  - set phase to `set_in_progress`
- `ConfirmGameFinished`:
  - require `between_sets`
  - set phase to `finished`

This avoids allowing the match to be marked finished while score entry is still
open for a live set.

## Overlay Selection

Overlay rendering should be selected directly from the persisted game phase.

Main overlay mapping:

- `planned` -> planned template
- `set_in_progress` -> live template
- `between_sets` -> intermission template
- `finished` -> finished template

This mapping should be centralized in the runtime re-render path and the
Telegram overlay-render job path so both startup and live operation use the
same rules.

The renderer should gain a fourth template:

- planned
- live
- intermission
- finished

The finished template should render to the same main `overlay.output_path` as
the planned, live, and between-set overlays. It should present:

- home and guest team identity
- final sets won
- completed set history

The existing separate `intermission.html` output may remain as an auxiliary
output for compatibility, but it should no longer be required for the normal
between-set flow.

The finished template must be configured and maintained as a separate template
file, not as a shared alias of the intermission template. The purpose is to let
operators and maintainers evolve the end-of-match presentation independently
from the between-set presentation without coupling layout or styling changes.

## View Model Strategy

The finished overlay needs almost the same data as the intermission overlay:

- both team names
- short names and hometowns where relevant
- logos
- aggregate sets won
- full set-score history

Even though the data overlaps heavily with the intermission screen, the spec
should require a distinct finished-overlay rendering path so the final-match
screen can evolve independently. In practice that means:

- a separate finished template path in config
- a separate cached finished template in the renderer
- a dedicated `RenderFinished(...)` renderer entry point
- a dedicated `FinishedViewModel` type, even if its fields initially overlap
  with `IntermissionViewModel`

This keeps independence explicit in the API surface and avoids accidentally
coupling future finished-screen changes to the between-set template contract.

## Telegram Control Behavior

The Telegram control message should mirror the explicit phase so the admin is
never offered actions that conflict with the visible overlay.

Phase-specific controls:

- `planned`
  - show `Start the game`
  - hide score buttons
- `set_in_progress`
  - show score buttons
  - show `Finish the set` when the active set is finishable
  - show `Reverse overlay sides`
- `between_sets`
  - hide score buttons
  - show `Start next set` when the match should continue
  - show `Finish the game` when the match is eligible to end
  - keep `Reverse overlay sides` only if it remains useful during the break
- `finished`
  - remove all inline buttons from the control message
  - leave the message as a final status display only

This is the main operator-facing behavior change: once a set is finished, score
entry is disabled until `Start next set` is pressed.

## Persistence and Migration

`pkg/storage/game.go` should add and persist a new `phase` field on the
existing `Game` record. This does not introduce multiple database rows for the
same match and does not create a separate phase-history table. The persistence
shape remains:

- one row in `games` per match
- zero or more rows in `game_sets` for that match
- one `phase` column on the `games` row describing the current operator-visible
  state of that match

Migration of existing rows should be conservative and deterministic:

- existing planned games -> `planned`
- existing finished games -> `finished`
- existing in-progress games with an active unfinished set ->
  `set_in_progress`
- existing in-progress games without an active unfinished set ->
  `between_sets`

The last case is the safest recovery assumption. If the app finds a match in
progress but without an active set, treating it as between sets is preferable to
pretending score entry is live.

## Runtime Startup Behavior

`pkg/app/runtime.go` should re-render the current overlay entirely from
persisted phase and existing set rows on startup.

Expected startup behavior:

- no current game -> render nothing
- `planned` -> render planned main overlay and intermission side output
- `set_in_progress` -> render live main overlay and intermission side output
- `between_sets` -> render intermission on the main overlay and intermission
  side output
- `finished` -> render finished main overlay and intermission side output if
  desired for compatibility

The important property is that a restart during a set break comes back showing
the set-break overlay on the main output without any manual fix-up.

## Broadcast Semantics

Broadcast behavior should remain concise and should avoid noisy operational
messages that are only relevant to the admin’s control flow.

Broadcasts should be sent for:

- game started
- score changed
- next set started
- game finished, with a brief final summary

Broadcasts should not be sent for:

- transition into `between_sets`
- set finished as a standalone event

The admin control message remains the authoritative UI. Broadcast wording does
not need to become materially more complex, but it should reflect the actual
public milestones of the match. In particular, subscribers should not receive a
message that implies a new set has started while the game is only waiting in
`between_sets`.

## Error Handling

Critical-path expectations:

- If finishing a set succeeds in SQLite, the game must remain in
  `between_sets` until an explicit next-set start succeeds.
- If creating the next set fails, the game must remain in `between_sets`; do
  not partially transition to `set_in_progress`.
- If finished-overlay rendering fails after the game is committed as finished,
  log the error but keep the database state authoritative.

This design continues the existing rule that persistence is authoritative and
rendering is a derived side effect.

## Testing Strategy

Tests should cover the new state model end to end.

Domain tests:

- `planned -> set_in_progress` on `StartPlannedGame`
- `set_in_progress -> between_sets` on `ConfirmSetFinished`
- `between_sets -> set_in_progress` on `StartNextSet`
- `between_sets -> finished` on `ConfirmGameFinished`
- illegal transitions fail cleanly

Telegram tests:

- score buttons are present only in `set_in_progress`
- `Finish the set` does not create the next set
- `Start next set` creates the next set and re-enables scoring
- `Finish the game` is available only when match-end rules allow it

Overlay tests:

- renderer loads and renders the new finished template
- main overlay path uses intermission template during `between_sets`
- main overlay path uses finished template during `finished`

Startup tests:

- runtime re-renders correctly from each persisted phase
- restart during `between_sets` restores the intermission main overlay
- restart during `finished` restores the finished main overlay

Migration tests:

- old planned, in-progress, and finished rows map to the expected new phase

## Rollout Plan

Implementation can proceed in small steps:

1. Add the new finished template config and renderer support
2. Add explicit persisted game phase
3. Refactor domain lifecycle functions to stop auto-creating the next set
4. Add `Start next set`
5. Update Telegram controls and callback handling
6. Update startup and async overlay rendering to select templates by phase
7. Add regression coverage for restart behavior and finished overlay rendering

This order keeps the flow change incremental while preserving clear test
boundaries between storage, domain logic, Telegram control behavior, and
rendering.
