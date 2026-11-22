# Telegram Admin Access Implementation Plan

**Goal**

Let Dash14 users request admin access and let existing admins approve, deny,
or revoke access in Telegram during a match.

**Architecture**

Use `users.is_admin` for the role and an admin version to invalidate stale
role-management buttons. Store requests and the message IDs of admin notices
in SQLite. Change roles and release match control within a transaction.

**Tech Stack**

Go, GORM, SQLite, and Telegram inline keyboards.

## 1. Persist requests and role changes

- Update `pkg/storage/user.go` and `pkg/storage/migrate.go` for the admin version,
  requests, and notification references. Add a partial unique index on the
  requester ID where the request status is `pending`.
- In `pkg/storage/admin_repository.go`, use a conditional write to claim each
  decision. Check the actor's role and version in that write. Keep revocation,
  the last-admin check, and clearing match ownership in one transaction.
- Test duplicate requests, concurrent decisions and revocations, role changes,
  and match preservation with `go test ./pkg/storage`.

## 2. Add the Telegram flow

- Register private `/admin` and `admin:` callbacks in `pkg/telegram/router.go`.
  Implement the menu, request notices, approval and denial, and confirmed
  revocation in `pkg/telegram/admin.go`.
- Use the Telegram sender ID for the requester and private chat identity for
  management actions. Bind buttons to the relevant request and role versions.
- Keep a pending requests entry in the admin menu so an admin can act when a
  notification fails or a new admin joins. Persist references to delivered
  request notices so a decision can update them after restart.
- Test the request, decision, and revocation flows with `go test ./pkg/telegram`.

## 3. Enforce revocation through game and planning actions

- Recheck the caller's role on the transactional game paths in
  `pkg/telegram/game_control.go`, `pkg/telegram/takeover.go`, and
  `pkg/telegram/scoring_panel.go`. Bind open sessions in
  `pkg/telegram/plan_flow.go` to the admin version they were opened under.
- Exercise old planning buttons, scoring controls, and takeover after
  revoking the current operator. Describe daily use and bootstrap in `README.md`.

## 4. Review and publish

- Run `go test ./...`, focused coverage and race tests, and `git diff --check`.
- Have an independent agent review the branch against `origin/main` and fix
  major findings. Rerun the affected checks and have the reviewer verify fixes.
- Commit and push the branch, open a review-ready PR referencing #31, and
  verify its review state and CI results.
