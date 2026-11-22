package telegram

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/smith3v/dash14/pkg/storage"
	"gorm.io/gorm"
)

//go:embed scoring_panel/index.html
var scoringPanelHTML []byte

//go:embed scoring_panel/panel.css
var scoringPanelCSS []byte

//go:embed scoring_panel/panel.js
var scoringPanelJS []byte

var (
	errScoringControlLost = errors.New("You no longer control this match. Run /game in the bot to see who has control.")
	errScoringOwner       = errors.New("Only the current match operator can use the scoring panel.")
	errScoringSet         = errors.New("That set is no longer active. Check the current score before tapping again.")
	errScoringKey         = errors.New("That request ID was already used for another action.")
)

type scoringPanel struct {
	router      *Router
	token       string
	controlEdit chan uint
}

type scoringTeam struct {
	Name    string `json:"name"`
	Score   int    `json:"score"`
	SetsWon int    `json:"setsWon"`
}

type scoringTransition struct {
	Action   string `json:"action"`
	Label    string `json:"label"`
	Revision string `json:"revision"`
}

type scoringState struct {
	GameID        uint              `json:"gameId"`
	SetID         uint              `json:"setId"`
	SetNumber     int               `json:"setNumber"`
	SetFinishable bool              `json:"setFinishable"`
	Transition    scoringTransition `json:"transition,omitzero"`
	Phase         storage.GamePhase `json:"phase"`
	HomeSide      string            `json:"homeSide"`
	Home          scoringTeam       `json:"home"`
	Guest         scoringTeam       `json:"guest"`
}

type scoringCommand struct {
	GameID    uint      `json:"gameId"`
	SetID     uint      `json:"setId"`
	RequestID uuid.UUID `json:"requestId"`
	Action    string    `json:"action"`
	Revision  string    `json:"revision,omitempty"`
}

// RegisterScoringPanel adds the Mini App and its API to the existing server.
// The returned worker refreshes the Telegram controls after Mini App writes.
func (r *Router) RegisterScoringPanel(mux *http.ServeMux, token, panelURL string) func(context.Context) {
	r.scoringPanelURL = panelURL
	panel := &scoringPanel{router: r, token: token, controlEdit: make(chan uint, 32)}
	mux.HandleFunc("GET /scoring", scoringAsset("text/html; charset=utf-8", scoringPanelHTML))
	mux.HandleFunc("GET /scoring/panel.css", scoringAsset("text/css; charset=utf-8", scoringPanelCSS))
	mux.HandleFunc("GET /scoring/panel.js", scoringAsset("text/javascript; charset=utf-8", scoringPanelJS))
	mux.HandleFunc("GET /scoring/state", panel.serveState)
	mux.HandleFunc("POST /scoring/actions", panel.serveAction)
	return panel.runControlEdits
}

func scoringAsset(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
}

func (p *scoringPanel) operator(w http.ResponseWriter, r *http.Request) (int64, bool) {
	auth, ok := strings.CutPrefix(r.Header.Get("Authorization"), "tma ")
	if ok {
		if userID, valid := telegramMiniAppUser(p.token, auth, time.Now()); valid {
			user, err := p.router.users.GetUserByTelegramID(userID)
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound), err == nil && !user.IsAdmin:
				p.writeError(w, r, errScoringOwner)
			case err != nil:
				p.writeError(w, r, err)
			default:
				return userID, true
			}
			return 0, false
		}
	}
	scoringJSON(w, http.StatusUnauthorized, map[string]string{"error": "Reopen the scoring panel from /game in Telegram."})
	return 0, false
}

func (p *scoringPanel) serveState(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.operator(w, r)
	if !ok {
		return
	}
	var gameID uint
	if raw := r.URL.Query().Get("gameId"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, strconv.IntSize)
		if err != nil || id == 0 {
			scoringJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid match ID."})
			return
		}
		gameID = uint(id)
	}
	p.respond(w, r, userID, gameID, nil)
}

func (p *scoringPanel) serveAction(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.operator(w, r)
	if !ok {
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		scoringJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Scoring actions require JSON."})
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	var cmd scoringCommand
	if err := decoder.Decode(&cmd); err != nil || decoder.Decode(new(any)) != io.EOF ||
		cmd.GameID == 0 || cmd.RequestID == uuid.Nil || !cmd.validAction() {
		scoringJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid scoring action."})
		return
	}
	p.router.gameActionMu.Lock()
	defer p.router.gameActionMu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	p.respond(w, r, userID, cmd.GameID, &cmd)
}

func (cmd scoringCommand) validAction() bool {
	if isLifecycleAction(cmd.Action) {
		return cmd.Revision != ""
	}
	return isScoringAction(cmd.Action) && cmd.SetID != 0 && cmd.Revision == ""
}

func (p *scoringPanel) respond(w http.ResponseWriter, r *http.Request, userID int64, gameID uint, cmd *scoringCommand) {
	var state scoringState
	var committedGame *storage.Game
	var committedSet *storage.GameSet
	var home, guest *storage.Team
	err := p.router.games.WithinTxContext(r.Context(), func(repo *storage.GameRepository) error {
		var match *storage.Game
		var err error
		if gameID == 0 {
			match, err = repo.GetCurrentGame()
		} else {
			match, err = repo.GetGameByID(gameID)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errNoCurrentGame
		}
		if err != nil {
			return err
		}
		if match == nil {
			return errNoCurrentGame
		}
		if match.CurrentAdminUserID != userID {
			if gameID != 0 {
				return errScoringControlLost
			}
			return errScoringOwner
		}
		set, err := findActiveSet(repo, match.ID)
		if err != nil {
			return err
		}
		home, guest, err = repo.GetGameTeams(match)
		if err != nil {
			return err
		}
		if cmd != nil {
			var changed bool
			set, changed, err = cmd.apply(repo, match, set)
			if err != nil {
				return err
			}
			if changed {
				committedGame, committedSet = match, set
			}
		}
		state = panelGameState(match, set, home, guest)
		return nil
	})
	if err != nil {
		if r.Context().Err() == nil {
			p.writeError(w, r, err)
		}
		return
	}

	if committedGame != nil {
		if p.router.gameChanged != nil {
			p.router.gameChanged()
		}
		p.router.enqueueGameBroadcast(r.Context(), cmd.Action, committedGame, home, guest, committedSet, nil)
		select {
		case p.controlEdit <- state.GameID:
		default:
		}
	}
	scoringJSON(w, http.StatusOK, state)
}

func (cmd scoringCommand) apply(repo *storage.GameRepository, match *storage.Game, set *storage.GameSet) (*storage.GameSet, bool, error) {
	lifecycle := isLifecycleAction(cmd.Action)
	receiptSetID, receiptValue := cmd.SetID, cmd.Action
	if lifecycle {
		// A lifecycle retry can arrive after its set has ended. Scope these
		// receipts to the request UUID and bind them to the displayed state.
		receiptSetID = 0
		receiptValue = fmt.Sprintf("%d/%d/%s/%s", cmd.GameID, cmd.SetID, cmd.Action, cmd.Revision)
	} else if match.EffectivePhase(set != nil) != storage.GamePhaseSetInProgress ||
		activeSetID(set) != cmd.SetID {
		return nil, false, errScoringSet
	}
	previous, err := repo.RecordScoringRequest(receiptSetID, cmd.RequestID.String(), receiptValue)
	if err != nil {
		return nil, false, err
	}
	if previous == receiptValue {
		return set, false, nil
	}
	if previous != "" {
		return nil, false, errScoringKey
	}

	if lifecycle {
		if cmd.SetID != activeSetID(set) || cmd.Revision != lifecycleRevision(match, set) {
			return nil, false, errLifecycleActionUnavailable
		}
		set, err = saveLifecycleAction(repo, match, set, cmd.Action)
	} else {
		err = saveScoringAction(repo, match, set, cmd.Action)
	}
	return set, err == nil, err
}

func panelGameState(match *storage.Game, set *storage.GameSet, home, guest *storage.Team) scoringState {
	phase := match.EffectivePhase(set != nil && !set.IsFinished)
	state := scoringState{
		GameID: match.ID, SetNumber: match.CurrentSetNumber, Phase: phase, HomeSide: match.HomeTeamSide,
		Home:  scoringTeam{Name: home.Name, SetsWon: match.HomeSetsWon},
		Guest: scoringTeam{Name: guest.Name, SetsWon: match.GuestSetsWon},
	}
	if phase == storage.GamePhaseSetInProgress {
		state.SetID, state.SetNumber = set.ID, set.SetNumber
		state.Home.Score, state.Guest.Score = set.HomeScore, set.GuestScore
	}
	if action := nextLifecycleAction(match, set); action != "" {
		state.SetFinishable = action == finishSet
		state.Transition = scoringTransition{
			Action: action, Label: lifecycleActionLabel(action), Revision: lifecycleRevision(match, set),
		}
	}
	return state
}

func (p *scoringPanel) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusConflict
	var message string
	switch {
	case errors.Is(err, errScoringOwner):
		status, message = http.StatusForbidden, errScoringOwner.Error()
	case errors.Is(err, errScoringControlLost):
		status, message = http.StatusForbidden, errScoringControlLost.Error()
	case errors.Is(err, errLifecycleActionUnavailable):
		message = errLifecycleActionUnavailable.Error()
	case errors.Is(err, errScoringSet):
		message = errScoringSet.Error()
	case errors.Is(err, errSetAwaitingConfirmation):
		message = errSetAwaitingConfirmation.Error()
	case errors.Is(err, errScoringKey):
		message = errScoringKey.Error()
	case errors.Is(err, errNoCurrentGame):
		message = "There is no planned or active match."
	default:
		p.router.logger.ErrorContext(r.Context(), "scoring panel request failed", "err", err)
		status, message = http.StatusServiceUnavailable, "Unable to confirm the score. Try again."
	}
	scoringJSON(w, status, map[string]string{"error": message})
}

func scoringJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (p *scoringPanel) runControlEdits(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case gameID := <-p.controlEdit:
			p.router.editGameControl(ctx, gameID, 0, false)
		}
	}
}
