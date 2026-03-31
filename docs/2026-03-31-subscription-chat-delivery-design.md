# Subscription Chat Delivery Design

## Overview

This design addresses one defect found during code review: subscriber
broadcasts target `TelegramUserID` as the destination chat, so subscriptions
created outside a private chat can appear successful while later broadcasts
fail.

The fix should keep the current package boundaries and runtime shape intact.
The app remains a single-process local Go service backed by SQLite and a
Telegram bot. The main change is to store the Telegram chat target used for
subscriber delivery explicitly instead of inferring it from the user record.

## Goals

- Make broadcast delivery target the chat that actually subscribed
- Preserve the current command set and user-facing subscription flow
- Avoid overwriting admin flags or unrelated user state during subscription
  updates
- Add tests for the failure modes behind the review finding

## Non-Goals

- Reworking the whole Telegram routing model
- Backfilling historical chat IDs from unavailable prior context
- Building a durable job queue for broadcasts
- Changing broadcast exclusion semantics, which still operate on Telegram user
  ID

## Problem

The user table stores a Telegram user identity and a `Subscribed` flag, but no
chat target. Broadcast fan-out later sends to `ChatID = TelegramUserID`. That
works only when the intended destination is a private DM with the same numeric
identifier. If a user subscribes in a group or channel context, the bot records
the subscription but does not retain the chat that should receive updates.

This leaves the app with misleading behavior: `/start` confirms subscription,
yet later broadcasts may fail because the stored destination is not the
subscribed chat.

## Alternatives Considered

### A. Keep Sending To `TelegramUserID`

This is the current behavior. It only works reliably when the subscribed chat
is the user’s private conversation with the bot. It does not reflect what the
subscription flow actually records, so it is not acceptable.

### B. Introduce A Separate Subscription Table

Another option is to split subscriptions into a dedicated table keyed by user
and chat, supporting multiple chat targets per user.

This is a broader feature change than needed. The current product model is one
subscription state per user, so a separate table adds complexity without
solving a current requirement.

### C. Persist A Single Subscription Chat Target On The User

This is the recommended approach.

- extend `storage.User` with `SubscriptionChatID`
- update `/start` and `/stop` to persist the current `update.Message.Chat.ID`
- send broadcasts to `SubscriptionChatID`
- skip and log subscribed users whose stored chat target is still zero

This keeps the data model simple and matches the existing user-centric
subscription behavior.

## Chosen Design

### Storage Model

Extend `storage.User` with:

- `SubscriptionChatID int64`

The field should default to zero so `AutoMigrate` can add it safely to existing
rows without a manual backfill step.

### Repository API

The storage layer should support persisting profile updates together with a chat
target. The simplest shape is:

- keep `UpsertTelegramUser(...)` as a compatibility wrapper
- add `UpsertTelegramUserWithChat(telegramUserID, username, displayName, chatID)`

On first insert, the user should still start with `Subscribed=true`. On later
updates, only the profile fields and `SubscriptionChatID` should change.
`Subscribed` and `IsAdmin` must remain untouched.

### Telegram Behavior

Behavior should be:

- `/start` upserts the user profile and records `SubscriptionChatID` from
  `update.Message.Chat.ID`, then marks the user subscribed
- `/stop` keeps using the current chat and may also refresh the stored
  `SubscriptionChatID` so the latest subscription context is remembered
- broadcasts send to `SubscriptionChatID`
- `BroadcastExcept` should continue excluding by Telegram user ID, not chat ID

This keeps the current user-facing semantics while making delivery match the
actual subscribed chat.

## Migration Strategy

`AutoMigrate` should add the new zero-default subscription chat column without
manual SQL migrations. Existing user rows will initially have
`SubscriptionChatID = 0`. That is acceptable as a transitional state:

- users who run `/start` again will get a valid chat target
- `/stop` can also refresh the stored chat target
- broadcast code should skip `0` targets rather than attempting delivery

No backfill is required for the first version because the current schema does
not contain reliable historical chat context.

## Error Handling

- if persisting the subscription chat fails, `/start` or `/stop` should return
  the existing generic error message
- if a broadcast encounters a subscribed user with `SubscriptionChatID == 0`,
  log and skip rather than failing the whole fan-out
- per-user send failures should continue to be best-effort and must not abort
  later sends

## Testing Strategy

Add focused tests for the exact failure modes behind the review finding.

Storage tests:

- chat ID is inserted on first contact
- chat ID is updated on later contact
- admin and subscription flags are not reset by chat-aware upsert

Telegram tests:

- `/start` stores the subscription chat ID from the incoming message
- `/stop` preserves existing behavior while refreshing the stored chat target
- broadcasts send to the stored chat ID, not `TelegramUserID`
- a subscribed user with no stored chat is skipped

Regression tests should continue proving the normal success paths.

## Rollout Notes

This change can be implemented in two stages:

1. add the storage field and chat-aware upsert with tests;
2. update `/start`, `/stop`, and broadcast delivery to use the stored chat.

No command changes are required. The visible result is that subscriptions will
reliably target the chat that actually subscribed.
