package telegram

import (
	"context"

	"github.com/go-telegram/bot"
)

type broadcastJob struct {
	text           string
	excludeUserIDs []int64
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
	r.ensureBroadcastWorker()

	job := broadcastJob{
		text:           text,
		excludeUserIDs: append([]int64(nil), excludeUserIDs...),
	}
	select {
	case r.broadcastJobs <- job:
	default:
		r.logger.WarnContext(ctx, "Broadcast: queue full, dropping message",
			"text", text, "excluded_users", len(excludeUserIDs))
	}
}

func (r *Router) ensureBroadcastWorker() {
	r.broadcastWorkerOnce.Do(func() {
		if r.broadcastQueueSize <= 0 {
			r.broadcastQueueSize = defaultBroadcastQueueSize
		}
		r.broadcastJobs = make(chan broadcastJob, r.broadcastQueueSize)
		go r.broadcastLoop()
	})
}

func (r *Router) broadcastLoop() {
	for job := range r.broadcastJobs {
		r.performBroadcast(job)
	}
}

func (r *Router) performBroadcast(job broadcastJob) {
	users, err := r.users.ListSubscribedUsers()
	if err != nil {
		r.logger.Error("Broadcast: list subscribed users failed", "err", err)
		return
	}

	excluded := make(map[int64]struct{}, len(job.excludeUserIDs))
	for _, id := range job.excludeUserIDs {
		if id == 0 {
			continue
		}
		excluded[id] = struct{}{}
	}

	for _, u := range users {
		if _, skip := excluded[u.TelegramUserID]; skip {
			continue
		}
		_, err := r.client.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID:    u.TelegramUserID,
			Text:      job.text,
			ParseMode: "HTML",
		})
		if err != nil {
			r.logger.Warn("Broadcast: send to user failed",
				"user_id", u.TelegramUserID, "err", err)
		}
	}
}
