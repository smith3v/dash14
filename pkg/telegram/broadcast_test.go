package telegram

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestScoreboardWorkerTracksSubscribersAcrossGamesAndRestart(t *testing.T) {
	store := openTestStore(t)
	fb := &FakeBot{}
	r := NewRouter(nil, discardLogger(), fb, store.users, nil)
	ctx := context.Background()
	const viewer, lateViewer, admin int64 = 8001, 8002, 8003
	subscribe := func(id int64) {
		t.Helper()
		if err := store.users.UpsertTelegramUser(id, "", "Viewer"); err != nil {
			t.Fatal(err)
		}
	}
	subscribe(viewer)
	subscribe(admin)

	publish := func(gameID uint, text string) {
		r.enqueueBroadcast(ctx, broadcastJob{gameID: gameID, text: text, excludeUserIDs: []int64{admin}})
	}
	sent := func(want int) sentMessage {
		t.Helper()
		waitForSentCount(t, func() int { return len(fb.SentMessages()) }, want)
		return fb.SentMessages()[want-1]
	}

	publish(1, "0-0")
	first := sent(1)
	if first.ChatID != viewer || first.Text != "0-0" {
		t.Fatalf("unexpected initial scoreboard: %+v", first)
	}

	subscribe(lateViewer)
	publish(1, "0-0")
	late := sent(2)
	if late.ChatID != lateViewer || late.Text != "0-0" {
		t.Fatalf("unexpected scoreboard for new subscriber: %+v", late)
	}
	if got := fb.EditedMessages(); len(got) != 0 {
		t.Fatalf("unchanged scoreboard triggered an edit: %+v", got)
	}

	if err := store.users.SetSubscription(viewer, false); err != nil {
		t.Fatal(err)
	}
	r.BroadcastExcept(ctx, "announcement", admin)
	publish(1, "Final result")
	publish(2, "0-0")
	next := sent(4)
	announcement := fb.SentMessages()[2]
	if announcement.ChatID != lateViewer || announcement.Text != "announcement" {
		t.Fatalf("unexpected announcement: %+v", announcement)
	}
	if next.ChatID != lateViewer || next.MessageID == late.MessageID || next.Text != "0-0" {
		t.Fatalf("expected a fresh scoreboard for game 2, got %+v", next)
	}
	edits := fb.EditedMessages()
	if len(edits) != 1 || edits[0].ChatID != lateViewer ||
		edits[0].MessageID != late.MessageID || edits[0].Text != "Final result" {
		t.Fatalf("expected just the still-subscribed viewer's final edit, got %+v", edits)
	}

	r = NewRouter(nil, discardLogger(), fb, store.users, nil)
	publish(2, "1-0")
	restarted := sent(5)
	if restarted.ChatID != lateViewer || restarted.MessageID == next.MessageID || restarted.Text != "1-0" {
		t.Fatalf("expected a fresh scoreboard after restart, got %+v", restarted)
	}
	publish(2, "2-0")
	waitForScoreboardEdit(t, fb, restarted, "2-0")
}

func TestScoreboardWorkerKeepsFinalResultWhenQueueIsBusy(t *testing.T) {
	store := openTestStore(t)
	const subscriber int64 = 8004
	if err := store.users.UpsertTelegramUser(subscriber, "", "Viewer"); err != nil {
		t.Fatal(err)
	}
	fb := &blockingSubscriberBot{
		subscriberID: subscriber,
		started:      make(chan int, 1),
		release:      make(chan struct{}),
	}
	release := sync.OnceFunc(func() { close(fb.release) })
	t.Cleanup(release)
	r := NewRouter(nil, discardLogger(), fb, store.users, nil)
	r.broadcastQueueSize = 1
	ctx := context.Background()

	r.enqueueBroadcast(ctx, broadcastJob{gameID: 1, text: "0-0"})
	select {
	case <-fb.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial scoreboard send")
	}
	r.enqueueBroadcast(ctx, broadcastJob{gameID: 1, text: "1-0", scoreChange: true})
	r.enqueueBroadcast(ctx, broadcastJob{gameID: 1, text: "2-0", scoreChange: true})
	r.enqueueBroadcast(ctx, broadcastJob{gameID: 1, text: "Final result"})
	r.enqueueBroadcast(ctx, broadcastJob{gameID: 2, text: "0-0"})

	release()
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)
	messages, edits := fb.SentMessages(), fb.EditedMessages()
	if len(edits) != 1 || edits[0].MessageID != messages[0].MessageID || edits[0].Text != "Final result" {
		t.Fatalf("expected queued point updates to give way to the final result, got %+v", edits)
	}
}

type scoreboardFailureBot struct {
	FakeBot
	editErr      error
	sendErr      error
	sendAttempts int
}

func (b *scoreboardFailureBot) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	b.sendAttempts++
	if b.sendErr != nil {
		return nil, b.sendErr
	}
	return b.FakeBot.SendMessage(ctx, params)
}

func (b *scoreboardFailureBot) EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	msg, err := b.FakeBot.EditMessageText(ctx, params)
	if b.editErr != nil {
		return nil, b.editErr
	}
	return msg, err
}

func TestScoreboardEditFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		replacement bool
		retryEdit   bool
	}{
		{name: "timeout", err: context.DeadlineExceeded, retryEdit: true},
		{name: "rate limit", err: &bot.TooManyRequestsError{RetryAfter: 1}, retryEdit: true},
		{name: "other bad request", err: fmt.Errorf("%w, Bad Request: can't parse entities", bot.ErrorBadRequest), retryEdit: true},
		{name: "already applied", err: fmt.Errorf("%w, Bad Request: message is not modified: same text", bot.ErrorBadRequest)},
		{name: "deleted", err: fmt.Errorf("%w, Bad Request: message to edit not found", bot.ErrorBadRequest), replacement: true},
		{name: "uneditable", err: fmt.Errorf("%w, Bad Request: message can't be edited", bot.ErrorBadRequest), replacement: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fb := &scoreboardFailureBot{}
			r := NewRouter(nil, discardLogger(), fb, nil, nil)
			scoreboards := make(map[int64]scoreboardMessage)
			ctx := context.Background()
			const id int64 = 8005

			r.updateScoreboard(ctx, id, "0-0", scoreboards)
			original := fb.SentMessages()[0].MessageID
			fb.editErr = tc.err
			r.updateScoreboard(ctx, id, "1-0", scoreboards)

			fb.editErr = nil
			r.updateScoreboard(ctx, id, "1-0", scoreboards)
			messages, edits := fb.SentMessages(), fb.EditedMessages()
			wantSends, wantEdits := 1, 1
			if tc.replacement {
				wantSends++
			}
			if tc.retryEdit {
				wantEdits++
			}
			if len(messages) != wantSends || len(edits) != wantEdits {
				t.Fatalf("got %d sends and %d edits, want %d sends and %d edits",
					len(messages), len(edits), wantSends, wantEdits)
			}
			for _, edit := range edits {
				if edit.MessageID != original {
					t.Fatalf("unexpected edit target: %+v", edit)
				}
			}

			r.updateScoreboard(ctx, id, "2-0", scoreboards)
			edits = fb.EditedMessages()
			if len(edits) != wantEdits+1 || edits[wantEdits].MessageID != messages[wantSends-1].MessageID {
				t.Fatalf("expected the next change to edit the last sent scoreboard, got %+v", edits)
			}
		})
	}
}

func TestScoreboardRetriesFailedSendsOnNextUpdate(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "initial"
		if replace {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
			fb := &scoreboardFailureBot{}
			r := NewRouter(nil, discardLogger(), fb, nil, nil)
			scoreboards := make(map[int64]scoreboardMessage)
			ctx := context.Background()
			const id int64 = 8006

			wantEdits, wantSent := 0, 0
			if replace {
				r.updateScoreboard(ctx, id, "0-0", scoreboards)
				fb.editErr = fmt.Errorf("%w, Bad Request: message to edit not found", bot.ErrorBadRequest)
				wantEdits, wantSent = 1, 1
			}
			fb.sendErr = &bot.TooManyRequestsError{RetryAfter: 1}
			r.updateScoreboard(ctx, id, "1-0", scoreboards)
			if got := fb.SentMessages(); len(got) != wantSent {
				t.Fatalf("unexpected messages after send failure: %+v", got)
			}
			fb.sendErr = nil
			r.updateScoreboard(ctx, id, "2-0", scoreboards)
			got := fb.SentMessages()
			if len(got) != wantSent+1 || got[wantSent].Text != "2-0" || fb.sendAttempts != wantSent+2 {
				t.Fatalf("expected one successful retry, got %d attempts and %+v", fb.sendAttempts, got)
			}
			if edits := fb.EditedMessages(); len(edits) != wantEdits {
				t.Fatalf("unexpected edits while retrying a send: %+v", edits)
			}
		})
	}
}
