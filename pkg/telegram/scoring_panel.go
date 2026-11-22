package telegram

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/smith3v/dash14/pkg/game"
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
	errScoringOwner = errors.New("Only the current match operator can use the scoring panel.")
	errScoringSet   = errors.New("That set is no longer active. Check the current score before tapping again.")
	errScoringKey   = errors.New("That request ID was already used for another action.")
)

type scoringPanel struct {
	router      *Router
	token       string
	controlEdit chan struct{}
}

type scoringTeam struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

type scoringState struct {
	GameID        uint              `json:"gameId"`
	SetID         uint              `json:"setId"`
	SetNumber     int               `json:"setNumber"`
	SetFinishable bool              `json:"setFinishable"`
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
}

// RegisterScoringPanel adds the Mini App and its API to the existing server.
// The returned worker refreshes the Telegram controls after Mini App writes.
func (r *Router) RegisterScoringPanel(mux *http.ServeMux, token, panelURL string) func(context.Context) {
	r.scoringPanelURL = panelURL
	panel := &scoringPanel{router: r, token: token, controlEdit: make(chan struct{}, 1)}
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
	p.respond(w, r, userID, nil)
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
		cmd.GameID == 0 || cmd.SetID == 0 || cmd.RequestID == uuid.Nil || !isScoringAction(cmd.Action) {
		scoringJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid scoring action."})
		return
	}
	p.respond(w, r, userID, &cmd)
}

func (p *scoringPanel) respond(w http.ResponseWriter, r *http.Request, userID int64, cmd *scoringCommand) {
	var state scoringState
	var broadcast string
	changed := false
	err := p.router.games.WithinTx(func(repo *storage.GameRepository) error {
		match, err := repo.GetCurrentGame()
		if err != nil {
			return err
		}
		if match == nil {
			return errNoCurrentGame
		}
		if match.CurrentAdminUserID != userID {
			return errScoringOwner
		}
		set, err := findActiveSet(repo, match.ID)
		if err != nil {
			return err
		}
		phase := match.EffectivePhase(set != nil)
		if cmd != nil && (match.ID != cmd.GameID || set == nil || set.ID != cmd.SetID ||
			phase != storage.GamePhaseSetInProgress) {
			return errScoringSet
		}
		home, guest, err := repo.GetGameTeams(match)
		if err != nil {
			return err
		}
		if cmd != nil {
			previous, err := repo.RecordScoringRequest(set.ID, cmd.RequestID.String(), cmd.Action)
			if err != nil {
				return err
			}
			switch previous {
			case "":
				if err := saveScoringAction(repo, match, set, cmd.Action); err != nil {
					return err
				}
				changed = true
				broadcast = scoringBroadcast(cmd.Action, match, home, guest, set)
			case cmd.Action:
				// An earlier attempt committed. Return the current score.
			default:
				return errScoringKey
			}
		}
		state = scoringState{
			GameID: match.ID, SetNumber: match.CurrentSetNumber, Phase: phase, HomeSide: match.HomeTeamSide,
			Home: scoringTeam{Name: home.Name}, Guest: scoringTeam{Name: guest.Name},
		}
		if phase == storage.GamePhaseSetInProgress {
			state.SetID, state.SetNumber = set.ID, set.SetNumber
			state.Home.Score, state.Guest.Score = set.HomeScore, set.GuestScore
			state.SetFinishable = game.IsSetFinishable(currentSetScore(match, set))
		}
		return nil
	})
	if err != nil {
		p.writeError(w, r, err)
		return
	}

	if changed {
		if p.router.gameChanged != nil {
			p.router.gameChanged()
		}
		p.router.BroadcastExcept(r.Context(), broadcast, userID)
		select {
		case p.controlEdit <- struct{}{}:
		default:
		}
	}
	scoringJSON(w, http.StatusOK, state)
}

func (p *scoringPanel) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusConflict
	var message string
	switch {
	case errors.Is(err, errScoringOwner):
		status, message = http.StatusForbidden, errScoringOwner.Error()
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
		case <-p.controlEdit:
			match, err := p.router.games.GetCurrentGame()
			if err != nil {
				p.router.logger.ErrorContext(ctx, "load current game for scoring panel control edit", "err", err)
			} else if match != nil {
				p.router.editGameControl(ctx, match.ID, 0, false)
			}
		}
	}
}
