package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/smith3v/dash14/storage"
	"gorm.io/gorm"
)

func (r *Router) handleUnknownCommand(ctx context.Context, update *models.Update) {
	if update.Message == nil {
		return
	}

	userID := senderID(update)
	chatID := update.Message.Chat.ID

	text, err := r.buildDefaultHelpMessage(userID)
	if err != nil {
		r.logger.ErrorContext(ctx, "handleUnknownCommand: build help message failed",
			"user_id", userID, "err", err)
		_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Something went wrong. Please try again.",
		})
		return
	}

	_, _ = r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
}

func (r *Router) buildDefaultHelpMessage(userID int64) (string, error) {
	isAdmin, err := r.lookupIsAdmin(userID)
	if err != nil {
		return "", err
	}

	plannedGameText, err := r.currentPlannedGameText()
	if err != nil {
		return "", err
	}

	if !isAdmin {
		lines := []string{
			"The bot will show the score updates during the game.",
		}
		if plannedGameText != "" {
			lines = append(lines, plannedGameText)
		}
		lines = append(lines,
			"You can unsubscribe from updates with /stop and subscribe back with /start.",
		)
		return strings.Join(lines, "\n"), nil
	}

	lines := []string{"You are an admin."}
	if plannedGameText != "" {
		lines = append(lines, plannedGameText)
	} else {
		lines = append(lines, "There is no game planned yet.")
	}
	lines = append(lines,
		"",
		"Use the following commands:",
		"/plan to plan the next game",
		"/game to manage the game status and the score",
		"/takeover to takeover the current game (then use /game)",
	)
	return strings.Join(lines, "\n"), nil
}

func (r *Router) lookupIsAdmin(userID int64) (bool, error) {
	if r.users == nil || userID == 0 {
		return false, nil
	}

	user, err := r.users.GetUserByTelegramID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("lookup admin user %d: %w", userID, err)
	}

	return user.IsAdmin, nil
}

func (r *Router) currentPlannedGameText() (string, error) {
	if r.games == nil || r.teams == nil {
		return "", nil
	}

	game, err := r.games.GetCurrentGame()
	if err != nil {
		return "", fmt.Errorf("get current game: %w", err)
	}
	if game == nil || game.Status != storage.GameStatusPlanned {
		return "", nil
	}

	home, err := r.teams.GetTeamByID(game.HomeTeamID)
	if err != nil {
		return "", fmt.Errorf("get planned home team: %w", err)
	}
	guest, err := r.teams.GetTeamByID(game.GuestTeamID)
	if err != nil {
		return "", fmt.Errorf("get planned guest team: %w", err)
	}

	return fmt.Sprintf("The next game is planned between %s and %s.", home.Name, guest.Name), nil
}
