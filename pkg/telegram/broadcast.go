package telegram

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type broadcastJob struct {
	text           string
	excludeUserIDs []int64
	gameID         uint // zero for an ordinary announcement
	scoreChange    bool // a pending point update can be replaced by a newer scoreboard
}

type scoreboardMessage struct {
	id       int
	lastText string
}

// Broadcast sends text to all subscribed users. Per-user failures are logged
// at warn level but do not abort the broadcast; all subscribed users are
// attempted regardless of earlier failures.
func (r *Router) Broadcast(ctx context.Context, text string) {
	r.BroadcastExcept(ctx, text)
}

// BroadcastExcept sends text to all subscribed users except explicitly
// excluded Telegram user IDs.
func (r *Router) BroadcastExcept(ctx context.Context, text string, excludeUserIDs ...int64) {
	r.enqueueBroadcast(ctx, broadcastJob{
		text:           text,
		excludeUserIDs: slices.Clone(excludeUserIDs),
	})
}

func (r *Router) enqueueBroadcast(ctx context.Context, job broadcastJob) {
	r.broadcastWorkerOnce.Do(func() {
		if r.broadcastQueueSize <= 0 {
			r.broadcastQueueSize = defaultBroadcastQueueSize
		}
		r.broadcastReady = sync.NewCond(&r.broadcastMu)
		go r.broadcastLoop()
	})

	r.broadcastMu.Lock()
	if n := len(r.broadcastJobs); n >= r.broadcastQueueSize {
		if job.gameID == 0 {
			r.broadcastMu.Unlock()
			r.logger.WarnContext(ctx, "Broadcast: queue full, dropping message",
				"text", job.text, "excluded_users", len(job.excludeUserIDs))
			return
		}
		// A newer scoreboard supersedes a queued point update for this game.
		// Preserve announcements and lifecycle updates already in the queue.
		last := &r.broadcastJobs[n-1]
		if last.gameID == job.gameID && last.scoreChange {
			*last = job
			r.broadcastMu.Unlock()
			return
		}
	}
	r.broadcastJobs = append(r.broadcastJobs, job)
	r.broadcastReady.Signal()
	r.broadcastMu.Unlock()
}

func (r *Router) broadcastLoop() {
	var gameID uint
	var scoreboards map[int64]scoreboardMessage
	for {
		r.broadcastMu.Lock()
		for len(r.broadcastJobs) == 0 {
			r.broadcastReady.Wait()
		}
		job := r.broadcastJobs[0]
		r.broadcastJobs[0] = broadcastJob{}
		r.broadcastJobs = r.broadcastJobs[1:]
		r.broadcastMu.Unlock()

		if job.gameID != 0 && job.gameID != gameID {
			gameID = job.gameID
			scoreboards = make(map[int64]scoreboardMessage)
		}
		r.performBroadcast(job, scoreboards)
	}
}

func (r *Router) performBroadcast(job broadcastJob, scoreboards map[int64]scoreboardMessage) {
	users, err := r.users.ListSubscribedUsers()
	if err != nil {
		r.logger.Error("Broadcast: list subscribed users failed", "err", err)
		return
	}

	ctx := context.Background()
	for _, u := range users {
		id := u.TelegramUserID
		if id != 0 && slices.Contains(job.excludeUserIDs, id) {
			continue
		}

		if job.gameID != 0 {
			r.updateScoreboard(ctx, id, job.text, scoreboards)
			continue
		}

		_, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    id,
			Text:      job.text,
			ParseMode: models.ParseModeHTML,
		})
		if err != nil {
			r.logger.Warn("Broadcast: send to user failed", "user_id", id, "err", err)
		}
	}
}

func (r *Router) updateScoreboard(ctx context.Context, userID int64, text string, scoreboards map[int64]scoreboardMessage) {
	if previous, ok := scoreboards[userID]; ok {
		if previous.lastText == text {
			return
		}

		_, err := r.client.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    userID,
			MessageID: previous.id,
			Text:      text,
			ParseMode: models.ParseModeHTML,
		})
		switch {
		case err == nil || scoreboardEditErrorIs(err, "message is not modified"):
			scoreboards[userID] = scoreboardMessage{id: previous.id, lastText: text}
			return
		case scoreboardEditErrorIs(err, "message to edit not found", "message can't be edited"):
			delete(scoreboards, userID)
		default:
			// A failed request can leave the displayed text unknown. Keep the ID
			// so the next update can reconcile the existing message.
			scoreboards[userID] = scoreboardMessage{id: previous.id}
			r.logger.Warn("Scoreboard: edit failed", "user_id", userID, "message_id", previous.id, "err", err)
			return
		}
	}

	msg, err := r.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    userID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		r.logger.Warn("Scoreboard: send failed", "user_id", userID, "err", err)
		return
	}
	if msg != nil && msg.ID != 0 {
		scoreboards[userID] = scoreboardMessage{id: msg.ID, lastText: text}
	}
}

func scoreboardEditErrorIs(err error, reasons ...string) bool {
	if !errors.Is(err, bot.ErrorBadRequest) {
		return false
	}
	description := strings.ToLower(err.Error())
	for _, reason := range reasons {
		if strings.Contains(description, "bad request: "+reason) {
			return true
		}
	}
	return false
}
