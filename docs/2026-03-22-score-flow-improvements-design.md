# Score Flow Improvements Design

## Overview

This design reduces perceived latency for Telegram score-button actions without
making score persistence eventually consistent. The critical path remains:
validate the callback, load the current game, compute the new state, and commit
the SQLite changes synchronously. After the database commit succeeds, the admin
control message should be updated immediately. Overlay file rendering and
subscriber broadcasts become background work and are allowed to lag slightly
behind the committed match state.

The intent is to improve operator responsiveness first. The admin who presses a
score button should see the control message reflect the new score as quickly as
possible. OBS overlay output and broadcast subscribers still converge to the
same committed state, but they no longer block the control-message refresh.

## Goals

- Keep score and game-state writes to SQLite synchronous and durable
- Reduce visible latency after Telegram score-button presses
- Avoid reparsing overlay templates on every render
- Optionally refresh template cache automatically so template edits are picked
  up without restarting the process
- Tune SQLite for a local single-process workload
- Make subscriber broadcasts asynchronous and non-blocking for score updates

## Non-Goals

- Event sourcing or a queued write-behind database layer
- Cross-process coordination for multiple app instances sharing one database
- Guaranteed ordered delivery to every subscriber under all failure modes
- Live template editing with sub-second change detection

## Current Bottlenecks

On each score callback, the app currently performs several synchronous steps in
one handler path:

1. Load current game, teams, and active set
2. Save the updated set and sometimes the game
3. Render overlay files
4. Edit the Telegram control message
5. Broadcast the update to subscribers

The SQLite write itself is only one part of this path. The current design also
parses templates from disk on every render and may perform subscriber sends in a
tight sequential loop. This means the admin-visible message update waits behind
file I/O, template parsing, and broadcast work that do not need to be on the
critical path.

## Proposed Runtime Flow

For score-changing actions, the new flow should be:

1. Acknowledge the callback query immediately
2. Load current state and validate authority/staleness
3. Compute the new score or lifecycle transition
4. Commit the SQLite changes synchronously
5. Rebuild the control-message view from the committed in-memory state
6. Edit the Telegram control message immediately
7. Enqueue overlay rendering
8. Enqueue subscriber broadcast

This keeps persistence authoritative while moving slower side effects out of the
response path. If overlay rendering or broadcasting fails, the committed state
remains correct and the admin already sees the accepted update. Background
failures should be logged and may be retried according to the rules below.

## Template Cache Design

Overlay templates should be cached in memory rather than parsed on every render.
The renderer should maintain one parsed template per configured template path:

- planned
- live
- intermission

The cache should be owned by the overlay renderer and protected for concurrent
reads and periodic refreshes. A simple and sufficient design is:

- store the active parsed templates in a small struct
- guard swaps with `sync.RWMutex` or use `atomic.Pointer` to replace the whole
  cache snapshot
- parse all configured templates during renderer construction
- fail fast at startup if initial parsing fails

Rendering uses the current in-memory template snapshot only. It should not touch
template source files on the hot path.

## Template Refresh Configuration

Overlay config should gain a new optional field for template cache refresh
interval, expressed in whole seconds in the YAML file.

Proposed field:

- `overlay.template_cache_refresh_interval_seconds`

Semantics:

- missing or `0`: cache indefinitely after the initial load
- positive integer: periodically refresh the template snapshot at that interval
- negative value: invalid configuration

The zero-value behavior is intentional and should require no explicit config in
existing deployments. With the field absent, the renderer loads templates once
during startup, stores them in memory, and never reparses them again for the
life of the process.

## Template Refresh Policy

When `overlay.template_cache_refresh_interval_seconds` is greater than zero, a
background refresh job should reparse all configured template files into a fresh
snapshot and only swap the cache if the full parse succeeds. Partial updates are
not acceptable: if one template fails to parse, the previous snapshot remains
active for all templates.

This design favors continuity over immediacy. Operators may edit templates on
disk and see changes after the configured interval, but a bad edit must not
break live rendering immediately. Refresh failures should be logged at error
level with the specific path and parse failure. Repeated failures should
continue to retry on the next interval without crashing the app.

When the configured interval is `0`, the refresh mechanism should perform no
periodic work. The initial load still occurs once during renderer construction,
after which the refresh goroutine should not be started. In effect, the refresh
functionality is "load once, then exit and never run again."

No filesystem watch mechanism is required. A simple interval-based polling loop
is sufficient when refresh is enabled.

## SQLite Tuning

SQLite remains the source of truth and score commits stay synchronous. The app
should tune SQLite for a local application with one writer and frequent reads:

- `PRAGMA foreign_keys = ON`
- `PRAGMA journal_mode = WAL`
- `PRAGMA synchronous = NORMAL`
- `PRAGMA busy_timeout = 5000`

`WAL` improves read/write concurrency and reduces lock contention between the
handler path and any background reads. `synchronous = NORMAL` is an intentional
trade-off: it preserves SQLite durability semantics appropriate for local app
state while reducing fsync pressure versus stricter defaults. `busy_timeout`
allows short waits on transient lock conflicts rather than immediate failure.

These pragmas should be applied during database open. The app should log the
chosen mode at startup so runtime configuration is visible in logs.

## Broadcast Model

Subscriber broadcast should become asynchronous relative to score-button
handling. After a successful SQLite commit and control-message edit, the handler
should enqueue a broadcast job and return. A background worker then resolves the
current subscriber list and delivers the message.

The initial design should use a single in-process worker and a bounded channel.
This keeps ordering simple and avoids unbounded memory growth. If the queue is
full, the app should drop the new broadcast and log a warning rather than block
the score-update path. Dropping a broadcast is acceptable because broadcasts are
informational and the Telegram admin control remains authoritative.

The worker should continue best-effort fan-out behavior: one subscriber failure
must not abort the rest of the sends.

## Ordering and Consistency Rules

The system should obey these ordering guarantees:

- SQLite commit happens before any async side effect is scheduled
- Telegram control-message edit happens from committed state
- Overlay rendering and broadcasts are derived from committed state only
- Background work may arrive later, but must never represent uncommitted state

The design does not guarantee strict ordering between overlay refresh and
broadcast delivery. Either may finish first. That is acceptable because both are
secondary views of the same committed state.

## Error Handling

Critical-path failures:

- callback validation failure: reject immediately
- SQLite write failure: do not edit the control message and do not enqueue side
  effects
- control-message edit failure: log it, but still enqueue overlay rendering and
  broadcast because the state commit already succeeded

Background failures:

- template refresh parse failure: keep old cache, log error
- overlay render failure: log error; no retry in the first version
- broadcast queue full: drop and log warning
- per-user broadcast send failure: log warning and continue

## Testing Strategy

Tests should cover behavior, not just structure:

- renderer tests proving templates are parsed once and reused
- renderer tests proving `0` refresh interval loads templates once and never
  starts periodic refresh
- renderer tests proving refresh swaps templates only on fully successful parse
  when a positive interval is configured
- config-load tests proving the new interval field defaults to `0` and rejects
  negative values
- database open tests asserting WAL and busy-timeout pragmas are applied
- Telegram callback tests proving `EditMessageText` happens before async
  broadcast scheduling
- async broadcast tests proving the score path does not block on subscriber send
- queue saturation tests proving broadcasts are dropped with logging rather than
  blocking

## Rollout Notes

These changes can be implemented incrementally:

1. Add SQLite pragmas during open
2. Extend config with the optional template refresh interval
3. Introduce renderer template cache without periodic refresh
4. Add interval-based template refresh, enabled only when the configured value
   is greater than zero
5. Introduce async broadcast worker
6. Reorder the score callback path so control-message edits happen before async
   side effects

That sequence keeps each change isolated and testable while moving the
highest-value latency improvements first.
