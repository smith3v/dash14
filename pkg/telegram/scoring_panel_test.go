package telegram

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
	"github.com/smith3v/dash14/pkg/storage"
)

const (
	testScoringToken = "12345:test-token"
	testScoringURL   = "https://dash14.example/scoring"
)

func signedScoringLaunch(token string, userID int64, date time.Time) string {
	user := fmt.Sprintf(`{"id":%d,"first_name":"A & B"}`, userID)
	fields := url.Values{
		"auth_date": {strconv.FormatInt(date.Unix(), 10)},
		"query_id":  {"query-id"},
		"signature": {"signature-from-telegram"},
		"user":      {user},
	}
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte(token))
	sig := hmac.New(sha256.New, key.Sum(nil))
	_, _ = fmt.Fprintf(sig, "auth_date=%s\nquery_id=query-id\nsignature=signature-from-telegram\nuser=%s", fields.Get("auth_date"), user)
	fields.Set("hash", hex.EncodeToString(sig.Sum(nil)))
	return fields.Encode()
}

func scoringRequest(t *testing.T, handler http.Handler, initData string, cmd *scoringCommand, want int) scoringState {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/scoring/state", nil)
	if cmd != nil {
		payload, err := json.Marshal(cmd)
		if err != nil {
			t.Fatal(err)
		}
		req = httptest.NewRequest(http.MethodPost, "/scoring/actions", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
	}
	if initData != "" {
		req.Header.Set("Authorization", "tma "+initData)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != want || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d %q, want %d and no-store", res.Code, res.Body.String(), want)
	}
	var state scoringState
	if want == http.StatusOK {
		if err := json.Unmarshal(res.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

func TestScoringPanelLaunchIsPrivate(t *testing.T) {
	store := openPlanTestStore(t)
	fb := &FakeBot{}
	r := NewRouter(nil, discardLogger(), fb, store.users, store.teams)
	r.SetGameServices(store.games, nil)
	r.RegisterScoringPanel(http.NewServeMux(), testScoringToken, testScoringURL)

	const owner int64 = 112
	store.createAdminUser(t, owner, "owner")
	match := createCurrentPlannedGame(t, store, owner)
	for _, chatType := range []models.ChatType{models.ChatTypePrivate, models.ChatTypeGroup} {
		update := makeGameMessageUpdate(owner, owner, "/game")
		update.Message.Chat.Type = chatType
		r.handleGame(t.Context(), nil, update)
		msgs := fb.SentMessages()
		markup := msgs[len(msgs)-1].ReplyMarkup.(*models.InlineKeyboardMarkup)
		var panelURL string
		for _, row := range markup.InlineKeyboard {
			for _, button := range row {
				if button.WebApp != nil {
					panelURL = button.WebApp.URL
				}
			}
		}
		if chatType == models.ChatTypePrivate && panelURL != fmt.Sprintf("%s?gameId=%d", testScoringURL, match.ID) ||
			chatType == models.ChatTypeGroup && panelURL != "" {
			t.Fatalf("%s chat got scoring URL %q", chatType, panelURL)
		}
	}
}

func TestScoringPanelAuthenticatesTelegramLaunch(t *testing.T) {
	store := openPlanTestStore(t)
	r := NewRouter(nil, discardLogger(), &FakeBot{}, store.users, store.teams)
	r.SetGameServices(store.games, nil)
	mux := http.NewServeMux()
	r.RegisterScoringPanel(mux, testScoringToken, testScoringURL)
	const owner = 30100100001
	store.createAdminUser(t, owner, "owner")
	createCurrentPlannedGame(t, store, owner)

	now := time.Now()
	valid := signedScoringLaunch(testScoringToken, owner, now)
	for _, test := range []struct {
		name string
		data string
		want int
	}{
		{"valid", valid, http.StatusOK},
		{"missing", "", http.StatusUnauthorized},
		{"expired", signedScoringLaunch(testScoringToken, owner, now.Add(-13*time.Hour)), http.StatusUnauthorized},
		{"future", signedScoringLaunch(testScoringToken, owner, now.Add(time.Hour)), http.StatusUnauthorized},
		{"another bot", signedScoringLaunch("98765:other-token", owner, now), http.StatusUnauthorized},
		{"tampered", strings.Replace(valid, "query-id", "different-query", 1), http.StatusUnauthorized},
		{"duplicate field", valid + "&query_id=query-id", http.StatusUnauthorized},
		{"signed viewer", signedScoringLaunch(testScoringToken, owner+1, now), http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			scoringRequest(t, mux, test.data, nil, test.want)
		})
	}
}

func TestScoringPanelSavesOnceAndKeepsTeamIdentityThroughSwitches(t *testing.T) {
	store := openPlanTestStore(t)
	fb := &FakeBot{}
	changes := make(chan struct{}, 10)
	r := NewRouter(nil, discardLogger(), fb, store.users, store.teams)
	r.SetGameServices(store.games, func() { changes <- struct{}{} })
	mux := http.NewServeMux()
	runPanel := r.RegisterScoringPanel(mux, testScoringToken, testScoringURL)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPanel(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	const owner, subscriber = 201, 202
	store.createAdminUser(t, owner, "owner")
	if err := store.users.UpsertTelegramUser(subscriber, "subscriber", "Subscriber"); err != nil {
		t.Fatal(err)
	}
	match := createCurrentPlannedGame(t, store, owner)
	match.Status, match.CurrentSetNumber = storage.GameStatusInProgress, 5
	if err := store.games.SaveGame(match); err != nil {
		t.Fatal(err)
	}
	set := &storage.GameSet{GameID: match.ID, SetNumber: 5, HomeScore: 7, GuestScore: 7}
	if err := store.games.CreateSet(set); err != nil {
		t.Fatal(err)
	}
	open := makeGameMessageUpdate(owner, owner, "/game")
	open.Message.Chat.Type = models.ChatTypePrivate
	r.handleGame(ctx, nil, open)
	auth := signedScoringLaunch(testScoringToken, owner, time.Now())

	cmd := &scoringCommand{GameID: match.ID, SetID: set.ID, RequestID: uuid.New(), Action: scoreHomeUp}
	if err := store.db.Exec(`
		CREATE TRIGGER fail_score_update BEFORE UPDATE ON game_sets
		BEGIN SELECT RAISE(ABORT, 'test write failure'); END
	`).Error; err != nil {
		t.Fatal(err)
	}
	scoringRequest(t, mux, auth, cmd, http.StatusServiceUnavailable)
	if before := scoringRequest(t, mux, auth, nil, http.StatusOK); before.Home.Score != 7 || len(changes) != 0 {
		t.Fatalf("failed write changed or published the score: %+v", before)
	}
	if err := store.db.Exec("DROP TRIGGER fail_score_update").Error; err != nil {
		t.Fatal(err)
	}
	state := scoringRequest(t, mux, auth, cmd, http.StatusOK)
	if state.SetNumber != 5 || state.HomeSide != "right" ||
		state.Home != (scoringTeam{Name: "Control Home", Score: 8}) || state.Guest.Score != 7 {
		t.Fatalf("unexpected state after fifth-set switch: %+v", state)
	}
	waitForSentCount(t, func() int { return len(fb.SentMessages()) }, 2)
	if got := fb.SentMessages()[1]; got.ChatID != int64(subscriber) || !strings.Contains(got.Text, "<b>8-7</b>") {
		t.Fatalf("unexpected subscriber score: %+v", got)
	}

	waitForCondition(t, "bot control edit", func() bool { return len(fb.EditedMessages()) > 0 })
	if got := fb.EditedMessages()[0].Text; !strings.Contains(got, "Score 8-7") {
		t.Fatalf("unexpected bot score: %q", got)
	}

	// Rebuild the handler to ensure retries use persisted state.
	restarted := NewRouter(nil, discardLogger(), fb, store.users, store.teams)
	restarted.SetGameServices(store.games, func() { changes <- struct{}{} })
	restartedMux := http.NewServeMux()
	restarted.RegisterScoringPanel(restartedMux, testScoringToken, testScoringURL)
	if retry := scoringRequest(t, restartedMux, auth, cmd, http.StatusOK); retry != state || len(changes) != 1 {
		t.Fatalf("retry changed state or republished: %+v, notifications %d", retry, len(changes))
	}
	cmd.Action = scoreGuestUp
	scoringRequest(t, mux, auth, cmd, http.StatusConflict)

	cmd.RequestID, cmd.Action = uuid.New(), switchSides
	state = scoringRequest(t, mux, auth, cmd, http.StatusOK)
	if state.HomeSide != "left" || state.Home.Score != 8 || state.Guest.Score != 7 {
		t.Fatalf("manual switch did not preserve scores: %+v", state)
	}
	cmd.RequestID, cmd.Action = uuid.New(), scoreHomeDown
	state = scoringRequest(t, mux, auth, cmd, http.StatusOK)
	if state.HomeSide != "left" || state.Home.Score != 7 || state.Guest.Score != 7 {
		t.Fatalf("correction targeted the wrong team: %+v", state)
	}
}

func TestScoringPanelRejectsOldSetAndPreviousOperator(t *testing.T) {
	store := openPlanTestStore(t)
	r := NewRouter(nil, discardLogger(), &FakeBot{}, store.users, store.teams)
	r.SetGameServices(store.games, nil)
	mux := http.NewServeMux()
	r.RegisterScoringPanel(mux, testScoringToken, testScoringURL)

	const owner, nextOwner = 311, 312
	store.createAdminUser(t, owner, "owner")
	store.createAdminUser(t, nextOwner, "next")
	match := createCurrentPlannedGame(t, store, owner)
	match.Status = storage.GameStatusInProgress
	if err := store.games.SaveGame(match); err != nil {
		t.Fatal(err)
	}
	set := &storage.GameSet{GameID: match.ID, SetNumber: 1, HomeScore: 24}
	if err := store.games.CreateSet(set); err != nil {
		t.Fatal(err)
	}
	open := makeGameMessageUpdate(owner, owner, "/game")
	open.Message.Chat.Type = models.ChatTypePrivate
	r.handleGame(t.Context(), nil, open)
	current, err := store.games.GetGameByID(match.ID)
	if err != nil {
		t.Fatal(err)
	}

	auth := signedScoringLaunch(testScoringToken, owner, time.Now())
	nextAuth := signedScoringLaunch(testScoringToken, nextOwner, time.Now())
	cmd := &scoringCommand{GameID: match.ID, SetID: set.ID, RequestID: uuid.New(), Action: scoreHomeUp}
	scoringRequest(t, mux, nextAuth, cmd, http.StatusForbidden)
	scoringRequest(t, mux, auth, cmd, http.StatusOK)

	for _, action := range []string{"game:set:finish", "game:set:start_next"} {
		update := makeGameCallbackUpdate(owner, owner, current.ControlMessageID, action, action)
		update.CallbackQuery.Message.Message.Chat.Type = models.ChatTypePrivate
		r.handleGameCallback(t.Context(), nil, update)
	}
	state := scoringRequest(t, mux, auth, nil, http.StatusOK)
	if state.SetNumber != 2 || state.SetID == set.ID {
		t.Fatalf("bot did not start the next set: %+v", state)
	}
	scoringRequest(t, mux, auth, cmd, http.StatusConflict)
	cmd.RequestID = uuid.New()
	scoringRequest(t, mux, auth, cmd, http.StatusConflict)

	r.handleTakeover(t.Context(), nil, makeTextUpdate(nextOwner, nextOwner, "/takeover"))
	cmd.SetID = state.SetID
	scoringRequest(t, mux, auth, cmd, http.StatusForbidden)
	if next := scoringRequest(t, mux, nextAuth, nil, http.StatusOK); next != state {
		t.Fatalf("stale writes or takeover changed scores: before %+v, after %+v", state, next)
	}
	scoringRequest(t, mux, nextAuth, cmd, http.StatusOK)
}

func TestScoringControlsStopAddingAtWinningScore(t *testing.T) {
	for _, test := range []struct {
		name       string
		setNumber  int
		deuceScore int
	}{
		{"regular set", 1, 24},
		{"fifth set", 5, 14},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openPlanTestStore(t)
			fb := &FakeBot{}
			r := NewRouter(nil, discardLogger(), fb, store.users, store.teams)
			r.SetGameServices(store.games, nil)
			mux := http.NewServeMux()
			r.RegisterScoringPanel(mux, testScoringToken, testScoringURL)

			const owner = 413
			store.createAdminUser(t, owner, "owner")
			match := createCurrentPlannedGame(t, store, owner)
			match.Status, match.CurrentSetNumber = storage.GameStatusInProgress, test.setNumber
			match.SideSwitchedInSet5 = test.setNumber == 5
			if err := store.games.SaveGame(match); err != nil {
				t.Fatal(err)
			}
			set := &storage.GameSet{
				GameID: match.ID, SetNumber: test.setNumber,
				HomeScore: test.deuceScore, GuestScore: test.deuceScore,
			}
			if err := store.games.CreateSet(set); err != nil {
				t.Fatal(err)
			}
			r.handleGame(t.Context(), nil, makeGameMessageUpdate(owner, owner, "/game"))
			control, err := store.games.GetGameByID(match.ID)
			if err != nil {
				t.Fatal(err)
			}
			auth := signedScoringLaunch(testScoringToken, owner, time.Now())

			cmd := scoringCommand{GameID: match.ID, SetID: set.ID, RequestID: uuid.New(), Action: scoreHomeUp}
			if lead := scoringRequest(t, mux, auth, &cmd, http.StatusOK); lead.SetFinishable || lead.Home.Score != test.deuceScore+1 {
				t.Fatalf("a one-point lead should remain playable: %+v", lead)
			}
			cmd.RequestID = uuid.New()
			winning := scoringRequest(t, mux, auth, &cmd, http.StatusOK)
			if !winning.SetFinishable || winning.Home.Score != test.deuceScore+2 {
				t.Fatalf("winning point should make the set finishable: %+v", winning)
			}

			for _, action := range []string{scoreHomeUp, scoreGuestUp} {
				extra := scoringCommand{GameID: match.ID, SetID: set.ID, RequestID: uuid.New(), Action: action}
				scoringRequest(t, mux, auth, &extra, http.StatusConflict)
				r.handleGameCallback(t.Context(), nil, makeGameCallbackUpdate(
					owner, owner, control.ControlMessageID, action, action,
				))
			}
			if retry := scoringRequest(t, mux, auth, &cmd, http.StatusOK); retry != winning {
				t.Fatalf("extra points or retry changed the winning score: %+v", retry)
			}

			r.handleGame(t.Context(), nil, makeGameMessageUpdate(owner, owner, "/game"))
			messages := fb.SentMessages()
			buttons := keyboardCallbackData(t, messages[len(messages)-1].ReplyMarkup)
			requireNoCallback(t, buttons, scoreHomeUp)
			requireNoCallback(t, buttons, scoreGuestUp)
			requireHasCallback(t, buttons, scoreHomeDown)
			requireHasCallback(t, buttons, scoreGuestDown)
			requireHasCallback(t, buttons, "game:set:finish")

			cmd.RequestID, cmd.Action = uuid.New(), scoreHomeDown
			corrected := scoringRequest(t, mux, auth, &cmd, http.StatusOK)
			if corrected.SetFinishable || corrected.Home.Score != test.deuceScore+1 {
				t.Fatalf("correcting the winning score should reopen scoring: %+v", corrected)
			}
			cmd.RequestID, cmd.Action = uuid.New(), scoreGuestUp
			tied := scoringRequest(t, mux, auth, &cmd, http.StatusOK)
			if tied.SetFinishable || tied.Home.Score != tied.Guest.Score {
				t.Fatalf("scoring should resume at deuce: %+v", tied)
			}
		})
	}
}

func TestScoringPanelPlaysMatchThroughTakeoverAndFinish(t *testing.T) {
	store := openPlanTestStore(t)
	fb := &FakeBot{}
	r := NewRouter(nil, discardLogger(), fb, store.users, store.teams)
	r.SetGameServices(store.games, nil)
	mux := http.NewServeMux()
	r.RegisterScoringPanel(mux, testScoringToken, testScoringURL)

	const firstOperator, nextOperator = 520, 521
	store.createAdminUser(t, firstOperator, "first")
	store.createAdminUser(t, nextOperator, "next")
	match := createCurrentPlannedGame(t, store, firstOperator)
	firstAuth := signedScoringLaunch(testScoringToken, firstOperator, time.Now())
	nextAuth := signedScoringLaunch(testScoringToken, nextOperator, time.Now())
	auth := firstAuth

	boundState := func(auth string, want int) scoringState {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/scoring/state?gameId=%d", match.ID), nil)
		req.Header.Set("Authorization", "tma "+auth)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("bound state response: %d %s", res.Code, res.Body.String())
		}
		var state scoringState
		if want == http.StatusOK {
			if err := json.Unmarshal(res.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
		} else if want == http.StatusForbidden && !strings.Contains(res.Body.String(), "no longer control") {
			t.Fatalf("expected inactive panel to explain takeover: %s", res.Body.String())
		}
		return state
	}

	command := func(state scoringState, action string) scoringCommand {
		t.Helper()
		if state.Transition.Action != action || state.Transition.Revision == "" || state.Transition.Label == "" {
			t.Fatalf("expected %s to be available: %+v", action, state)
		}
		return scoringCommand{
			GameID: state.GameID, SetID: state.SetID, Action: action,
			RequestID: uuid.New(), Revision: state.Transition.Revision,
		}
	}

	start := command(boundState(auth, http.StatusOK), startMatch)
	state := scoringRequest(t, mux, auth, &start, http.StatusOK)
	if state.SetID == 0 || state.SetNumber != 1 || state.Transition.Action != "" {
		t.Fatalf("expected a playable first set: %+v", state)
	}
	if retry := scoringRequest(t, mux, auth, &start, http.StatusOK); retry != state {
		t.Fatalf("retry changed first set: %+v", retry)
	}
	start.RequestID = uuid.New()
	scoringRequest(t, mux, auth, &start, http.StatusConflict)

	var oldIntermission scoringCommand
	for number := 1; number <= 5; number++ {
		set, err := store.games.GetActiveSet(match.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case number == 5:
			set.HomeScore, set.GuestScore = 15, 13
		case number%2 == 0:
			set.HomeScore, set.GuestScore = 23, 25
		default:
			set.HomeScore, set.GuestScore = 25, 23
		}
		if err := store.games.SaveSet(set); err != nil {
			t.Fatal(err)
		}
		if number == 2 {
			r.handleGame(t.Context(), nil, makeGameMessageUpdate(nextOperator, nextOperator, "/game"))
			current, err := store.games.GetGameByID(match.ID)
			if err != nil {
				t.Fatal(err)
			}
			r.handleGameCallback(t.Context(), nil, makeGameCallbackUpdate(nextOperator, nextOperator,
				current.ControlMessageID, "stale-set", finishSet+":1"))
		}
		endSet := command(boundState(auth, http.StatusOK), finishSet)
		state = scoringRequest(t, mux, auth, &endSet, http.StatusOK)
		if state.SetID != 0 || state.SetNumber != number || state.Phase != storage.GamePhaseBetweenSets {
			t.Fatalf("set %d did not finish: %+v", number, state)
		}
		if retry := scoringRequest(t, mux, auth, &endSet, http.StatusOK); retry != state {
			t.Fatalf("finish retry changed set %d: %+v", number, retry)
		}
		if number == 5 {
			break
		}

		nextSet := command(state, startNextSet)
		if number == 1 {
			oldIntermission = nextSet
			r.handleTakeover(t.Context(), nil, makeTextUpdate(nextOperator, nextOperator, "/takeover"))
			boundState(firstAuth, http.StatusForbidden)
			scoringRequest(t, mux, firstAuth, &nextSet, http.StatusForbidden)
			scoringRequest(t, mux, firstAuth, &endSet, http.StatusForbidden)
			auth = nextAuth
			if after := boundState(auth, http.StatusOK); after != state {
				t.Fatalf("takeover changed the match: before %+v, after %+v", state, after)
			}
		}
		if number == 2 {
			stale := oldIntermission
			stale.RequestID = uuid.New()
			scoringRequest(t, mux, auth, &stale, http.StatusConflict)
		}
		next := scoringRequest(t, mux, auth, &nextSet, http.StatusOK)
		if next.SetID == 0 || next.SetNumber != number+1 || next.Home.Score != 0 || next.Guest.Score != 0 ||
			next.HomeSide == state.HomeSide {
			t.Fatalf("expected next set and switched sides: %+v", next)
		}
	}

	finish := command(state, finishMatch)
	final := scoringRequest(t, mux, auth, &finish, http.StatusOK)
	if final.Phase != storage.GamePhaseFinished || final.Transition.Action != "" ||
		final.Home.SetsWon != 3 || final.Guest.SetsWon != 2 {
		t.Fatalf("unexpected final result: %+v", final)
	}
	scoringRequest(t, mux, auth, nil, http.StatusConflict)
	if got := boundState(auth, http.StatusOK); got != final {
		t.Fatalf("panel lost the final result: %+v", got)
	}
	if err := store.games.CreateGame(&storage.Game{
		HomeTeamID: match.HomeTeamID, GuestTeamID: match.GuestTeamID, CurrentAdminUserID: firstOperator,
	}); err != nil {
		t.Fatal(err)
	}
	if retry := scoringRequest(t, mux, auth, &finish, http.StatusOK); retry != final {
		t.Fatalf("finish retry picked up a new match: %+v", retry)
	}
}
