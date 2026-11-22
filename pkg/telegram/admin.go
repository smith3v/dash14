package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

const adminPageSize = 8

type adminConfirmation struct {
	token         string
	messageID     int
	actorVersion  int64
	targetID      int64
	targetVersion int64
}

func (r *Router) handleAdmin(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.From.ID <= 0 {
		return
	}
	if msg.Chat.Type != models.ChatTypePrivate || msg.Chat.ID != msg.From.ID {
		r.sendAdminMessage(ctx, msg.Chat.ID, "Open a private chat with the bot and send /admin.", nil)
		return
	}

	id := msg.From.ID
	if err := r.users.UpsertTelegramContact(id, msg.From.Username, telegramDisplayName(msg.From)); err != nil {
		r.sendAdminMessage(ctx, id, r.adminError(ctx, err), nil)
		return
	}
	user, err := r.users.GetUserByTelegramID(id)
	if err != nil {
		r.sendAdminMessage(ctx, id, r.adminError(ctx, err), nil)
		return
	}

	r.adminConfirmations.Delete(id)
	if user.IsAdmin {
		text, keys, err := r.adminList(*user, 0)
		if err != nil {
			text = r.adminError(ctx, err)
		}
		r.sendAdminMessage(ctx, id, text, keys)
		return
	}
	request, err := r.users.GetLatestAdminRequest(id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.sendAdminMessage(ctx, id, r.adminError(ctx, err), nil)
		return
	}
	if request.Pending() {
		r.sendAdminMessage(ctx, id, "Your admin access request is pending.", nil)
		return
	}
	text := "Ask the current Dash14 admins for access."
	if request.ID != 0 && !request.Approved() {
		text = "Your previous admin access request was denied.\n\n" + text
	}
	r.sendAdminMessage(ctx, id, text, adminKeyboard(
		[]models.InlineKeyboardButton{adminButton("Request admin access", "request", user.AdminVersion, "")},
	))
}

func (r *Router) handleAdminCallback(ctx context.Context, _ *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	if cb == nil {
		return
	}
	answered := false
	answer := func(text string) {
		answered = true
		if _, err := r.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID, Text: text,
		}); err != nil {
			r.logger.WarnContext(ctx, "answer admin callback", "err", err)
		}
	}
	result := r.adminCallback(ctx, cb, answer)
	if !answered {
		answer(result)
	}
}

func (r *Router) adminCallback(ctx context.Context, cb *models.CallbackQuery, answer func(string)) string {
	msg := cb.Message.Message
	if msg == nil || msg.Chat.Type != models.ChatTypePrivate || msg.Chat.ID != cb.From.ID || cb.From.ID <= 0 {
		return "Use /admin in a private chat with the bot."
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "admin" {
		return "This button has expired. Send /admin."
	}
	action := parts[1]
	version, err := strconv.ParseInt(parts[2], 36, 64)
	if err != nil || version < 0 {
		return "This button has expired. Send /admin."
	}
	arg := ""
	if len(parts) == 4 {
		arg = parts[3]
	}
	actorID := cb.From.ID
	if action == "request" {
		return r.submitAdminRequest(ctx, actorID, version, msg.ID, answer)
	}

	actor, err := r.users.GetUserByTelegramID(actorID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.adminError(ctx, storage.ErrAdminUnauthorized)
	}
	if err != nil {
		return r.adminError(ctx, err)
	}
	if !actor.IsAdmin || actor.AdminVersion != version {
		return r.adminError(ctx, storage.ErrAdminUnauthorized)
	}

	if action == "revoke" {
		return r.revokeAdmin(ctx, *actor, msg.ID, arg, answer)
	}

	id, err := strconv.ParseInt(arg, 36, 64)
	if err != nil || id < 0 {
		return r.adminError(ctx, storage.ErrAdminActionStale)
	}
	switch action {
	case "approve", "deny":
		return r.decideAdminRequest(ctx, *actor, msg, id, action == "approve", answer)
	case "view":
		return r.viewAdminRequest(ctx, *actor, id)
	case "select":
		return r.confirmAdminRevocation(ctx, *actor, msg.ID, id)
	case "admins", "pending":
		r.cancelAdminConfirmation(actorID, msg.ID)
		var text string
		var keys *models.InlineKeyboardMarkup
		if action == "admins" {
			text, keys, err = r.adminList(*actor, id)
		} else {
			text, keys, err = r.pendingAdminList(*actor, id)
		}
		if err != nil {
			return r.adminError(ctx, err)
		}
		r.editAdminMessage(ctx, actorID, msg.ID, text, keys)
		return ""
	default:
		return r.adminError(ctx, storage.ErrAdminActionStale)
	}
}

func (r *Router) submitAdminRequest(ctx context.Context, userID, version int64, messageID int, answer func(string)) string {
	request, err := r.users.RequestAdminAccess(userID, version)
	if err != nil {
		return r.adminError(ctx, err)
	}
	answer("Admin access requested.")
	r.editAdminMessage(ctx, userID, messageID, "Admin access requested. The bot will tell you when an admin decides.", nil)
	admins, err := r.users.ListAdmins()
	if err != nil {
		r.logger.ErrorContext(ctx, "list admins for access request", "request_id", request.ID, "err", err)
		return ""
	}
	requester, err := r.users.GetUserByTelegramID(userID)
	if err != nil {
		r.logger.ErrorContext(ctx, "load access requester", "request_id", request.ID, "err", err)
		return ""
	}
	for _, admin := range admins {
		r.sendAdminRequestNotice(ctx, admin, *requester, request)
	}
	return ""
}

func (r *Router) viewAdminRequest(ctx context.Context, actor storage.User, requestID int64) string {
	request, err := r.users.GetAdminRequest(requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && !request.Pending() {
		return r.adminError(ctx, storage.ErrAdminActionStale)
	}
	if err != nil {
		return r.adminError(ctx, err)
	}
	requester, err := r.users.GetUserByTelegramID(request.RequesterID)
	if err != nil {
		return r.adminError(ctx, err)
	}
	r.sendAdminRequestNotice(ctx, actor, *requester, request)
	return ""
}

func (r *Router) sendAdminRequestNotice(ctx context.Context, admin, requester storage.User, request storage.AdminRequest) {
	reqID := adminNumber(request.ID)
	message := "Admin access requested by " + adminUserLabel(requester)
	keys := adminKeyboard([]models.InlineKeyboardButton{
		adminButton("Approve", "approve", admin.AdminVersion, reqID),
		adminButton("Deny", "deny", admin.AdminVersion, reqID),
	})
	msg := r.sendAdminMessage(ctx, admin.TelegramUserID, message, keys)
	if msg == nil {
		return
	}
	notice := storage.AdminRequestNotice{RequestID: request.ID, ChatID: admin.TelegramUserID, MessageID: msg.ID}
	if err := r.users.RecordAdminRequestNotice(notice); err != nil {
		r.logger.ErrorContext(ctx, "record admin request notice", "request_id", request.ID, "err", err)
	}
	// A decision can commit while a notice is in flight. If its updater
	// missed this copy, finish updating it here.
	current, err := r.users.GetAdminRequest(request.ID)
	if err != nil {
		r.logger.ErrorContext(ctx, "read admin request after notice", "request_id", request.ID, "err", err)
		return
	}
	if !current.Pending() {
		r.editAdminMessage(ctx, notice.ChatID, notice.MessageID, r.adminDecisionText(ctx, current), nil)
	}
}

func (r *Router) decideAdminRequest(ctx context.Context, actor storage.User, message *models.Message, requestID int64, approve bool, answer func(string)) string {
	request, err := r.users.DecideAdminRequest(requestID, actor.TelegramUserID, actor.AdminVersion, approve)
	if errors.Is(err, storage.ErrAdminActionStale) && strings.HasPrefix(message.Text, "Admin access requested by ") {
		request, readErr := r.users.GetAdminRequest(requestID)
		if readErr == nil && !request.Pending() {
			answer("This request has already been decided.")
			r.editAdminMessage(ctx, actor.TelegramUserID, message.ID, r.adminDecisionText(ctx, request), nil)
			return ""
		}
	}
	if err != nil {
		return r.adminError(ctx, err)
	}
	answer("Decision saved.")
	result := r.adminDecisionText(ctx, request)
	r.editAdminMessage(ctx, actor.TelegramUserID, message.ID, result, nil)

	notices, err := r.users.ListAdminRequestNotices(requestID)
	if err != nil {
		r.logger.ErrorContext(ctx, "list admin request notices", "request_id", requestID, "err", err)
	}
	for _, notice := range notices {
		if notice.ChatID != actor.TelegramUserID || notice.MessageID != message.ID {
			r.editAdminMessage(ctx, notice.ChatID, notice.MessageID, result, nil)
		}
	}
	feedback := "Your Dash14 admin access request was denied."
	if approve {
		feedback = "Your Dash14 admin access was approved. Send /admin to manage access or /plan to plan a match."
	}
	r.sendAdminMessage(ctx, request.RequesterID, feedback, nil)
	return ""
}

func (r *Router) adminDecisionText(ctx context.Context, request storage.AdminRequest) string {
	requester := r.adminDecisionLabel(ctx, request.RequesterID)
	decider := r.adminDecisionLabel(ctx, request.DecidedBy)
	outcome := "denied"
	if request.Approved() {
		outcome = "approved"
	}
	return fmt.Sprintf("Admin access request from %s was %s by %s.", requester, outcome, decider)
}

func (r *Router) adminDecisionLabel(ctx context.Context, id int64) string {
	user, err := r.users.GetUserByTelegramID(id)
	if err != nil {
		r.logger.WarnContext(ctx, "load user for admin decision", "user_id", id, "err", err)
		return fmt.Sprintf("Telegram user %d", id)
	}
	return adminUserLabel(*user)
}

func (r *Router) confirmAdminRevocation(ctx context.Context, actor storage.User, messageID int, targetID int64) string {
	target, err := r.users.GetUserByTelegramID(targetID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && !target.IsAdmin {
		return r.adminError(ctx, storage.ErrAdminActionStale)
	}
	if err != nil {
		return r.adminError(ctx, err)
	}

	confirmation := adminConfirmation{
		token: uuid.NewString(), messageID: messageID, actorVersion: actor.AdminVersion,
		targetID: targetID, targetVersion: target.AdminVersion,
	}
	r.adminConfirmations.Store(actor.TelegramUserID, confirmation)
	r.editAdminMessage(ctx, actor.TelegramUserID, messageID, "Revoke admin access for "+adminUserLabel(*target)+"?", adminKeyboard(
		[]models.InlineKeyboardButton{
			adminButton("Revoke access", "revoke", actor.AdminVersion, confirmation.token),
			adminButton("Cancel", "admins", actor.AdminVersion, "0"),
		},
	))
	return ""
}

func (r *Router) cancelAdminConfirmation(actorID int64, messageID int) {
	value, ok := r.adminConfirmations.Load(actorID)
	if ok && value.(adminConfirmation).messageID == messageID {
		r.adminConfirmations.CompareAndDelete(actorID, value)
	}
}

func (r *Router) revokeAdmin(ctx context.Context, actor storage.User, messageID int, token string, answer func(string)) string {
	id := actor.TelegramUserID
	value, ok := r.adminConfirmations.Load(id)
	if !ok {
		return r.adminError(ctx, storage.ErrAdminActionStale)
	}
	confirmation := value.(adminConfirmation)
	if confirmation.token != token || confirmation.messageID != messageID || confirmation.actorVersion != actor.AdminVersion ||
		!r.adminConfirmations.CompareAndDelete(id, confirmation) {
		return r.adminError(ctx, storage.ErrAdminActionStale)
	}

	released, err := r.users.RevokeAdminAccess(id, actor.AdminVersion, confirmation.targetID, confirmation.targetVersion)
	if err != nil {
		return r.adminError(ctx, err)
	}
	answer("Admin access revoked.")
	r.sendAdminMessage(ctx, confirmation.targetID, "Your Dash14 admin access was revoked.", nil)
	if id == confirmation.targetID {
		r.editAdminMessage(ctx, id, messageID, "Your admin access was revoked.", nil)
	} else {
		text, keys, err := r.adminList(actor, 0)
		if err != nil {
			r.logger.ErrorContext(ctx, "refresh admin list after revocation", "err", err)
			text = "Admin access revoked. Send /admin to refresh the list."
		} else {
			text = "Admin access revoked.\n\n" + text
		}
		r.editAdminMessage(ctx, id, messageID, text, keys)
	}
	if released {
		r.notifyAdminTakeover(ctx)
	}
	return ""
}

func (r *Router) notifyAdminTakeover(ctx context.Context) {
	admins, err := r.users.ListAdmins()
	if err != nil {
		r.logger.ErrorContext(ctx, "list admins after releasing match control", "err", err)
		return
	}
	for _, admin := range admins {
		r.sendAdminMessage(ctx, admin.TelegramUserID,
			"The current match lost its operator. If needed, run /takeover, then /game to resume control.", nil)
	}
}

func (r *Router) adminError(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, storage.ErrAdminUnauthorized):
		return "This button requires current admin access. Send /admin."
	case errors.Is(err, storage.ErrAdminActionStale):
		return "This button is no longer active. Send /admin."
	case errors.Is(err, storage.ErrAdminRequestPending):
		return "Your admin access request is already pending."
	case errors.Is(err, storage.ErrNoAdmins):
		return "No Dash14 admin is available yet. Ask the bot operator to set up the first admin."
	case errors.Is(err, storage.ErrLastAdmin):
		return "The last Dash14 admin must keep access."
	default:
		r.logger.ErrorContext(ctx, "manage Telegram admin access", "err", err)
		return "Unable to manage admin access. Send /admin and try again."
	}
}
