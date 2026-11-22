package telegram

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/pkg/storage"
)

func (r *Router) adminList(actor storage.User, page int64) (string, *models.InlineKeyboardMarkup, error) {
	admins, err := r.users.ListAdmins()
	if err != nil {
		return "", nil, err
	}
	start, end := adminPage(len(admins), page)
	keys := adminKeyboard()
	for _, admin := range admins[start:end] {
		keys.InlineKeyboard = append(keys.InlineKeyboard, []models.InlineKeyboardButton{
			adminButton(adminUserLabel(admin), "select", actor.AdminVersion, adminNumber(admin.TelegramUserID)),
		})
	}
	addAdminPagination(keys, "admins", actor.AdminVersion, start, end, len(admins))
	keys.InlineKeyboard = append(keys.InlineKeyboard, []models.InlineKeyboardButton{
		adminButton("Pending requests", "pending", actor.AdminVersion, "0"),
	})
	text := "Dash14 admins. Select a person to manage their access."
	if r.games != nil {
		match, err := r.games.GetCurrentGame()
		if err != nil {
			r.logger.Warn("load match for admin list", "err", err)
		} else if match != nil && match.CurrentAdminUserID == 0 {
			text += "\n\nThe current match needs an operator. Run /takeover, then /game to resume control."
		}
	}
	return text, keys, nil
}

func (r *Router) pendingAdminList(actor storage.User, page int64) (string, *models.InlineKeyboardMarkup, error) {
	requests, err := r.users.ListPendingAdminRequests()
	if err != nil {
		return "", nil, err
	}
	start, end := adminPage(len(requests), page)
	keys := adminKeyboard()
	for _, request := range requests[start:end] {
		user, err := r.users.GetUserByTelegramID(request.RequesterID)
		if err != nil {
			return "", nil, err
		}
		keys.InlineKeyboard = append(keys.InlineKeyboard, []models.InlineKeyboardButton{
			adminButton(adminUserLabel(*user), "view", actor.AdminVersion, adminNumber(request.ID)),
		})
	}
	addAdminPagination(keys, "pending", actor.AdminVersion, start, end, len(requests))
	keys.InlineKeyboard = append(keys.InlineKeyboard, []models.InlineKeyboardButton{
		adminButton("Back to admins", "admins", actor.AdminVersion, "0"),
	})
	text := "Pending admin access requests. Select a request to review it."
	if len(requests) == 0 {
		text = "There are no pending admin access requests."
	}
	return text, keys, nil
}

func adminPage(total int, page int64) (start, end int) {
	page = min(page, int64(max(total-1, 0)/adminPageSize))
	start = int(page) * adminPageSize
	return start, min(start+adminPageSize, total)
}

func addAdminPagination(keys *models.InlineKeyboardMarkup, action string, version int64, start, end, total int) {
	var nav []models.InlineKeyboardButton
	page := int64(start / adminPageSize)
	if start > 0 {
		nav = append(nav, adminButton("Previous", action, version, adminNumber(page-1)))
	}
	if end < total {
		nav = append(nav, adminButton("Next", action, version, adminNumber(page+1)))
	}
	if len(nav) > 0 {
		keys.InlineKeyboard = append(keys.InlineKeyboard, nav)
	}
}

func adminNumber(n int64) string { return strconv.FormatInt(n, 36) }

func adminButton(label, action string, version int64, arg string) models.InlineKeyboardButton {
	data := "admin:" + action + ":" + adminNumber(version)
	if arg != "" {
		data += ":" + arg
	}
	return models.InlineKeyboardButton{Text: label, CallbackData: data}
}

func adminKeyboard(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: append([][]models.InlineKeyboardButton{}, rows...)}
}

func adminUserLabel(user storage.User) string {
	name := user.DisplayName
	if user.Username != "" {
		if name != "" {
			name += " "
		}
		name += "@" + user.Username
	}
	if name == "" {
		return fmt.Sprintf("Telegram user %d", user.TelegramUserID)
	}
	return fmt.Sprintf("%s (%d)", name, user.TelegramUserID)
}

func (r *Router) sendAdminMessage(ctx context.Context, chatID int64, text string, keys *models.InlineKeyboardMarkup) *models.Message {
	params := &bot.SendMessageParams{ChatID: chatID, Text: text}
	if keys != nil {
		params.ReplyMarkup = keys
	}
	message, err := r.client.SendMessage(ctx, params)
	if err != nil {
		r.logger.WarnContext(ctx, "send Telegram admin message", "chat_id", chatID, "err", err)
		return nil
	}
	return message
}

func (r *Router) editAdminMessage(ctx context.Context, chatID int64, messageID int, text string, keys *models.InlineKeyboardMarkup) {
	if keys == nil {
		keys = adminKeyboard()
	}
	if _, err := r.client.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: keys,
	}); err != nil {
		r.logger.WarnContext(ctx, "edit Telegram admin message", "chat_id", chatID, "message_id", messageID, "err", err)
	}
}
