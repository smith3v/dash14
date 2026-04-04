# Subscription Chat Delivery Design

## Overview

This design addresses one defect found during code review: subscriber
broadcasts target `TelegramUserID` as the destination chat, so subscriptions
created outside a private chat can appear successful while later broadcasts
fail.

The fix keeps the current package boundaries and runtime shape intact. The app
remains a single-process local Go service backed by SQLite and a Telegram bot.
Instead of adding chat-target persistence, the bot should only allow
subscriptions in private chats where `TelegramUserID` already matches the
delivery destination.

## Goals

- Prevent misleading subscription success in group or channel contexts
- Preserve the existing user-based subscription model
- Keep broadcast delivery using `TelegramUserID`
- Add tests for the rejected non-private-chat cases behind the review finding

## Non-Goals

- Reworking the Telegram routing model
- Supporting subscriptions for groups, channels, or multiple chats per user
- Adding a subscription chat column or separate subscription table
- Building a durable job queue for broadcasts

## Problem

The user table stores a Telegram user identity and a `Subscribed` flag.
Broadcast fan-out later sends to `ChatID = TelegramUserID`. That only works
reliably for a private DM with the bot. When `/start` is accepted in a group or
channel context, the bot confirms subscription even though future broadcasts
still target the user’s private chat identity.

That creates a misleading flow: the bot says the subscription succeeded in the
current chat, but later updates are sent somewhere else or fail.

## Alternatives Considered

### A. Keep Accepting Any Chat And Send To `TelegramUserID`

This is the current behavior. It is inconsistent and misleading, so it is not
acceptable.

### B. Persist A Single Subscription Chat Target On The User

This would add a `SubscriptionChatID` field and make the latest chat win for a
user.

It is a workable patch, but it complicates a product model that is currently
user-based. It also introduces awkward semantics: `/stop` in one chat would
disable delivery for all chats, and a later `/start` would silently move the
delivery target.

### C. Add A Separate Subscription Table Keyed By Chat

This would be the correct shape if the product needed group subscriptions or
multiple delivery targets per user.

That is broader than the current requirement and would change subscription
semantics more than necessary.

### D. Restrict Subscriptions To Private Chats

This is the recommended approach.

- allow `/start` and `/stop` only in private chats
- reject non-private chats with a clear instructional message
- keep `users.subscribed` as a per-user flag
- keep broadcasts sending to `TelegramUserID`

This matches the existing storage model and product intent with the least
complexity.

## Chosen Design

### Storage Model

No schema changes are required.

The existing `storage.User` model remains:

- Telegram identity keyed by `TelegramUserID`
- subscription state in `Subscribed`
- admin authorization in `IsAdmin`

No chat-target field is added.

### Repository API

No repository API changes are required.

The existing behavior remains:

- `UpsertTelegramUser(...)` creates or updates the user profile
- `SetSubscription(...)` toggles the user’s subscription state
- `ListSubscribedUsers(...)` returns subscribed users

### Telegram Behavior

Behavior should be:

- `/start` in a private chat upserts the user and marks them subscribed
- `/stop` in a private chat marks the user unsubscribed
- `/start` in a non-private chat sends a rejection message and does not touch
  storage
- `/stop` in a non-private chat sends a rejection message and does not touch
  storage
- broadcasts continue sending to `TelegramUserID`

The rejection message should clearly tell the user to open the bot directly and
run the command in a private chat.

## Migration Strategy

No migration is required.

This is a behavior fix, not a data-model change. Existing subscribed users in
private chats continue to work as before. The only visible change is that group
or channel `/start` and `/stop` no longer pretend to succeed.

## Error Handling

- if private-chat subscription persistence fails, `/start` or `/stop` should
  return the existing generic error message
- if `/start` or `/stop` is received outside a private chat, the bot should
  send an instructional rejection message and return without writing to storage
- per-user broadcast send failures remain best-effort and must not abort later
  sends

## Testing Strategy

Add focused Telegram tests for the failure mode behind the review finding.

Telegram tests:

- `/start` in a private chat still subscribes normally
- `/start` in a non-private chat does not create or update subscription state
- `/stop` in a private chat still unsubscribes normally
- `/stop` in a non-private chat does not change existing subscription state
- broadcasts still send to `TelegramUserID`

Regression tests should continue proving the normal success paths.

## Rollout Notes

This change can be implemented directly in the Telegram handlers and tests.

No command changes are required. The visible result is that subscriptions only
work in private chats, which matches the existing user-based delivery model and
removes the misleading group-subscription path.
