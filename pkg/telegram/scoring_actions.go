package telegram

import (
	"errors"

	"github.com/smith3v/dash14/pkg/game"
	"github.com/smith3v/dash14/pkg/storage"
)

const (
	scoreHomeUp    = "game:home:+1"
	scoreHomeDown  = "game:home:-1"
	scoreGuestUp   = "game:guest:+1"
	scoreGuestDown = "game:guest:-1"
	switchSides    = "game:reverse"
)

var errSetAwaitingConfirmation = errors.New("The set is ready to finish. Confirm the result or correct the score.")

func isScoringAction(action string) bool {
	switch action {
	case scoreHomeUp, scoreHomeDown, scoreGuestUp, scoreGuestDown, switchSides:
		return true
	default:
		return false
	}
}

// saveScoringAction is shared by the inline controls and the scoring panel.
// Callers check authorization and lifecycle in the enclosing transaction.
func saveScoringAction(repo *storage.GameRepository, match *storage.Game, set *storage.GameSet, action string) error {
	if action == switchSides {
		phase := match.EffectivePhase(set != nil)
		applyGameState(match, game.ReverseOverlaySides(toGameState(match, phase)).Game)
		return repo.SaveGame(match)
	}

	score := currentSetScore(match, set)
	if (action == scoreHomeUp || action == scoreGuestUp) && game.IsSetFinishable(score) {
		return errSetAwaitingConfirmation
	}
	var result game.ScoreResult
	switch action {
	case scoreHomeUp:
		result = game.IncrementHome(score)
	case scoreHomeDown:
		result = game.DecrementHome(score)
	case scoreGuestUp:
		result = game.IncrementGuest(score)
	case scoreGuestDown:
		result = game.DecrementGuest(score)
	default:
		return errUnhandledGameCallback
	}

	set.HomeScore = result.Set.HomeScore
	set.GuestScore = result.Set.GuestScore
	if err := repo.SaveSet(set); err != nil {
		return err
	}
	if result.SideSwitch {
		match.HomeTeamSide, match.GuestTeamSide = match.GuestTeamSide, match.HomeTeamSide
	}
	match.SideSwitchedInSet5 = result.Set.SideSwitchedInSet5
	return repo.SaveGame(match)
}

func currentSetScore(match *storage.Game, set *storage.GameSet) game.SetScore {
	return game.SetScore{
		HomeScore:          set.HomeScore,
		GuestScore:         set.GuestScore,
		SetNumber:          set.SetNumber,
		SideSwitchedInSet5: match.SideSwitchedInSet5,
	}
}
