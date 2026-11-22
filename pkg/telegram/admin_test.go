package telegram

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

func TestAdminRequestDecisionAfterRestart(t *testing.T) {
	for _, action := range []string{"Approve", "Deny"} {
		t.Run(action, func(t *testing.T) {
			store := openTestStore(t)
			r, fb := newRouterWithStore(t, store)
			const requester, firstAdmin, secondAdmin = 100, 101, 102
			store.createAdminUser(t, firstAdmin, "first")
			store.createAdminUser(t, secondAdmin, "second")
			if err := store.users.SetSubscription(secondAdmin, false); err != nil {
				t.Fatal(err)
			}

			r.handleAdmin(t.Context(), nil, makeTextUpdate(requester, requester, "/admin"))
			menu := sentAdminMessage(t, fb, requester, "Ask the current")
			request := adminTestButton(t, menu.ReplyMarkup, "Request admin access")
			pressAdminTestButton(t, r, requester, menu.MessageID, request)
			pressAdminTestButton(t, r, requester, menu.MessageID, request)
			if got := len(fb.SentMessages()); got != 3 {
				t.Fatalf("got %d sends, want menu and one notice per admin", got)
			}
			first := sentAdminMessage(t, fb, firstAdmin, "Admin access requested by")
			second := sentAdminMessage(t, fb, secondAdmin, "Admin access requested by")
			if !strings.Contains(first.Text, "TestUser (100)") {
				t.Fatalf("requester identity missing from %q", first.Text)
			}

			// A new router has no in-memory state from the request.
			restarted, after := newRouterWithStore(t, store)
			button := adminTestButton(t, first.ReplyMarkup, action)
			outsider := makeCallbackUpdateForMessage(requester, firstAdmin, first.MessageID, "outsider", button)
			outsider.CallbackQuery.Message.Message.Chat.Type = models.ChatTypePrivate
			restarted.handleAdminCallback(t.Context(), nil, outsider)
			assertAdminRole(t, store.users, requester, false, false)

			pressAdminTestButton(t, restarted, firstAdmin, first.MessageID, button)
			pressAdminTestButton(t, restarted, secondAdmin, second.MessageID, adminTestButton(t, second.ReplyMarkup, "Approve"))
			pressAdminTestButton(t, restarted, requester, menu.MessageID, request)
			assertAdminRole(t, store.users, requester, action == "Approve", false)
			edits := after.EditedMessages()
			if len(edits) != 2 {
				t.Fatalf("got %d edits, want both copies of the decision", len(edits))
			}
			for _, edit := range edits {
				if !strings.Contains(edit.Text, actionToOutcome(action)+" by first @first") ||
					len(edit.ReplyMarkup.(*models.InlineKeyboardMarkup).InlineKeyboard) != 0 {
					t.Fatalf("unexpected decision edit: %+v", edit)
				}
			}
			sentAdminMessage(t, after, requester, "Your Dash14 admin access")
			if action == "Deny" {
				restarted.handleAdmin(t.Context(), nil, makeTextUpdate(requester, requester, "/admin"))
				sentAdminMessage(t, after, requester, "Your previous admin access request was denied.")
			}
			stale := makeCallbackUpdateForMessage(secondAdmin, secondAdmin, second.MessageID, "stale", adminTestButton(t, second.ReplyMarkup, "Approve"))
			stale.CallbackQuery.Message.Message.Chat.Type = models.ChatTypePrivate
			stale.CallbackQuery.Message.Message.Text = second.Text
			restarted.handleAdminCallback(t.Context(), nil, stale)
			repaired := lastAdminEdit(t, after)
			if repaired.MessageID != second.MessageID || repaired.Text != edits[1].Text {
				t.Fatalf("stale card did not recover the persisted decision: %+v", repaired)
			}
			if _, err := store.users.GetPendingAdminRequest(requester); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("pending request after decision and stale taps: %v", err)
			}
		})
	}
}

func TestAdminCanReviewPendingRequestWithoutNotification(t *testing.T) {
	store := openTestStore(t)
	const requester, admin = 201, 202
	store.createAdminUser(t, admin, "admin")
	if err := store.users.UpsertTelegramContact(requester, "", "New Operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.users.RequestAdminAccess(requester, 0); err != nil {
		t.Fatal(err)
	}

	r, fb := newRouterWithStore(t, store)
	r.handleAdmin(t.Context(), nil, makeTextUpdate(admin, admin, "/admin"))
	menu := sentAdminMessage(t, fb, admin, "Dash14 admins.")
	pressAdminTestButton(t, r, admin, menu.MessageID, adminTestButton(t, menu.ReplyMarkup, "Pending requests"))
	list := lastAdminEdit(t, fb)
	pressAdminTestButton(t, r, admin, list.MessageID, adminTestButton(t, list.ReplyMarkup, "New Operator"))
	notice := sentAdminMessage(t, fb, admin, "Admin access requested by New Operator")
	pressAdminTestButton(t, r, admin, notice.MessageID, adminTestButton(t, notice.ReplyMarkup, "Approve"))
	assertAdminRole(t, store.users, requester, true, false)
	sentAdminMessage(t, fb, requester, "Your Dash14 admin access was approved.")
}

func TestAdminRevocationPreservesMatchAndStopsOldControls(t *testing.T) {
	store := openPlanTestStore(t)
	r, fb, _ := newPlanRouter(t, store)
	const operator, remaining = 301, 302
	store.createAdminUser(t, operator, "operator")
	store.createAdminUser(t, remaining, "remaining")
	match := createCurrentPlannedGame(t, store, operator)

	// Leave a replacement wizard and a score panel open under the old role.
	r.handlePlan(t.Context(), nil, makeTextUpdate(operator, operator, "/plan"))
	plan := sentAdminMessage(t, fb, operator, "A game is already planned:")
	current := match
	current.Status = storage.GameStatusInProgress
	if err := store.games.SaveGame(current); err != nil {
		t.Fatal(err)
	}
	set := &storage.GameSet{GameID: match.ID, SetNumber: 1, HomeScore: 12, GuestScore: 9}
	if err := store.games.CreateSet(set); err != nil {
		t.Fatal(err)
	}
	r.handleGame(t.Context(), nil, makeGameMessageUpdate(operator, operator, "/game"))
	current, err := store.games.GetGameByID(match.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldPoint := makeCurrentGameCallbackUpdate(t, store.games, operator, operator, current.ControlMessageID, "old-point", scoreHomeUp)
	mux := http.NewServeMux()
	r.RegisterScoringPanel(mux, testScoringToken, testScoringURL)
	auth := signedScoringLaunch(testScoringToken, operator, time.Now())
	before := scoringRequest(t, mux, auth, nil, http.StatusOK)

	r.handleAdmin(t.Context(), nil, makeTextUpdate(remaining, remaining, "/admin"))
	menu := sentAdminMessage(t, fb, remaining, "Dash14 admins.")
	selectButton := adminTestButton(t, menu.ReplyMarkup, "operator")
	pressAdminTestButton(t, r, remaining, menu.MessageID, selectButton)
	confirmation := lastAdminEdit(t, fb)
	cancel := adminTestButton(t, confirmation.ReplyMarkup, "Cancel")
	revoke := adminTestButton(t, confirmation.ReplyMarkup, "Revoke access")
	pressAdminTestButton(t, r, remaining, menu.MessageID, cancel)
	pressAdminTestButton(t, r, remaining, menu.MessageID, revoke)
	assertAdminRole(t, store.users, operator, true, true)

	pressAdminTestButton(t, r, remaining, menu.MessageID, selectButton)
	confirmation = lastAdminEdit(t, fb)
	revoke = adminTestButton(t, confirmation.ReplyMarkup, "Revoke access")
	pressAdminTestButton(t, r, remaining, menu.MessageID, revoke)
	assertAdminRole(t, store.users, operator, false, true)
	sentAdminMessage(t, fb, operator, "Your Dash14 admin access was revoked")
	sentAdminMessage(t, fb, remaining, "The current match lost its operator")
	current, err = store.games.GetGameByID(match.ID)
	if err != nil || current.CurrentAdminUserID != 0 || current.ControlMessageID != 0 {
		t.Fatalf("released match = %+v, %v", current, err)
	}

	r.handleGameCallback(t.Context(), nil, oldPoint)
	r.handlePlanCallback(t.Context(), nil, makeCallbackUpdateForMessage(operator, operator, plan.MessageID, "old-plan", "plan:replace:start"))
	if _, exists := r.plans.Load(int64(operator)); exists {
		t.Fatal("revoked operator's planning session is still available")
	}
	scoringRequest(t, mux, auth, &scoringCommand{
		GameID: match.ID, SetID: set.ID, RequestID: uuid.New(), Action: scoreHomeUp,
	}, http.StatusForbidden)

	r.handleTakeover(t.Context(), nil, makeTextUpdate(remaining, remaining, "/takeover"))
	newAuth := signedScoringLaunch(testScoringToken, remaining, time.Now())
	if after := scoringRequest(t, mux, newAuth, nil, http.StatusOK); after != before {
		t.Fatalf("match changed across revocation and takeover: before %+v, after %+v", before, after)
	}
}

func TestAdminRevocationInvalidatesUnfinishedPlanAfterNewGrant(t *testing.T) {
	store := openPlanTestStore(t)
	r, _, _ := newPlanRouter(t, store)
	const planner, otherAdmin = 401, 402
	store.createAdminUser(t, planner, "planner")
	store.createAdminUser(t, otherAdmin, "other")
	insertTeam(t, store.teams, "home", "Home team", "H")
	insertTeam(t, store.teams, "guest", "Guest team", "G")
	r.handlePlan(t.Context(), nil, makeTextUpdate(planner, planner, "/plan"))
	r.handlePlanText(t.Context(), nil, makeTextUpdate(planner, planner, "Home team"))

	if _, err := store.users.RevokeAdminAccess(otherAdmin, 0, planner, 0); err != nil {
		t.Fatal(err)
	}
	request, err := store.users.RequestAdminAccess(planner, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.users.DecideAdminRequest(request.ID, otherAdmin, 0, true); err != nil {
		t.Fatal(err)
	}
	r.handlePlanText(t.Context(), nil, makeTextUpdate(planner, planner, "Guest team"))
	if match, err := store.games.GetCurrentGame(); err != nil || match != nil {
		t.Fatalf("unfinished plan produced match %+v, %v", match, err)
	}
}

func actionToOutcome(action string) string {
	if action == "Approve" {
		return "approved"
	}
	return "denied"
}

func pressAdminTestButton(t *testing.T, router *Router, userID int64, messageID int, data string) {
	t.Helper()
	update := makeCallbackUpdateForMessage(userID, userID, messageID, fmt.Sprint(messageID), data)
	update.CallbackQuery.Message.Message.Chat.Type = models.ChatTypePrivate
	router.handleAdminCallback(t.Context(), nil, update)
}

func sentAdminMessage(t *testing.T, fb *FakeBot, userID int64, prefix string) sentMessage {
	t.Helper()
	for _, message := range fb.SentMessages() {
		if message.ChatID == userID && strings.HasPrefix(message.Text, prefix) {
			return message
		}
	}
	t.Fatalf("no message to %d starting with %q", userID, prefix)
	return sentMessage{}
}

func lastAdminEdit(t *testing.T, fb *FakeBot) editedMessage {
	t.Helper()
	edits := fb.EditedMessages()
	if len(edits) == 0 {
		t.Fatal("expected an edited message")
	}
	return edits[len(edits)-1]
}

func adminTestButton(t *testing.T, markup any, labelPrefix string) string {
	t.Helper()
	keyboard, ok := markup.(*models.InlineKeyboardMarkup)
	if ok {
		for _, row := range keyboard.InlineKeyboard {
			for _, button := range row {
				if strings.HasPrefix(button.Text, labelPrefix) {
					return button.CallbackData
				}
			}
		}
	}
	t.Fatalf("button %q missing from %+v", labelPrefix, markup)
	return ""
}

func assertAdminRole(t *testing.T, users *storage.UserRepository, id int64, admin, subscribed bool) {
	t.Helper()
	user, err := users.GetUserByTelegramID(id)
	if err != nil || user.IsAdmin != admin || user.Subscribed != subscribed {
		t.Fatalf("user %d = %+v, %v, want admin=%t subscribed=%t", id, user, err, admin, subscribed)
	}
}
