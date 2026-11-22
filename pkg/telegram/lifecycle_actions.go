package telegram

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/smith3v/dash14/pkg/game"
	"github.com/smith3v/dash14/pkg/storage"
)

const (
	startMatch   = "game:start"
	startNextSet = "game:set:start_next"
	finishSet    = "game:set:finish"
	finishMatch  = "game:finish"
)

var errLifecycleActionUnavailable = errors.New("That match action is no longer available. Check the current match state.")

func isLifecycleAction(action string) bool {
	switch action {
	case startMatch, startNextSet, finishSet, finishMatch:
		return true
	default:
		return false
	}
}

func nextLifecycleAction(match *storage.Game, set *storage.GameSet) string {
	phase := match.EffectivePhase(set != nil && !set.IsFinished)
	switch phase {
	case storage.GamePhasePlanned:
		return startMatch
	case storage.GamePhaseSetInProgress:
		if game.IsSetFinishable(currentSetScore(match, set)) {
			return finishSet
		}
	case storage.GamePhaseBetweenSets:
		if game.IsGameFinishEligible(toGameState(match, phase)) {
			return finishMatch
		}
		return startNextSet
	}
	return ""
}

func decodeLifecycleCallback(data string) (action string, setNumber int) {
	if i := strings.LastIndexByte(data, ':'); i > 0 && isLifecycleAction(data[:i]) {
		if number, err := strconv.Atoi(data[i+1:]); err == nil && number > 0 {
			return data[:i], number
		}
	}
	return data, 0
}

func activeSetID(set *storage.GameSet) uint {
	if set == nil || set.IsFinished {
		return 0
	}
	return set.ID
}

// The set number identifies intermissions, which have no active set ID.
func lifecycleRevision(match *storage.Game, set *storage.GameSet) string {
	return fmt.Sprintf("%d/%d/%d/%d", match.CurrentSetNumber, activeSetID(set), match.HomeTeamID, match.GuestTeamID)
}

func lifecycleActionLabel(action string) string {
	switch action {
	case startMatch:
		return "Start the game"
	case startNextSet:
		return "Start next set"
	case finishSet:
		return "Finish the set"
	case finishMatch:
		return "Finish the game"
	default:
		return ""
	}
}

// saveLifecycleAction applies the same phase and scoring rules in both control
// surfaces. The caller verifies ownership and the expected set in the transaction.
func saveLifecycleAction(repo *storage.GameRepository, match *storage.Game, set *storage.GameSet, action string) (*storage.GameSet, error) {
	if action != nextLifecycleAction(match, set) {
		return nil, errLifecycleActionUnavailable
	}
	phase := match.EffectivePhase(set != nil && !set.IsFinished)
	state := toGameState(match, phase)
	var next game.GameState
	var newSet game.SetState
	var err error
	switch action {
	case startMatch:
		next, newSet, err = game.StartPlannedGame(state)
	case startNextSet:
		next, newSet, err = game.StartNextSet(state)
	case finishSet:
		result, finishErr := game.ConfirmSetFinished(state, game.SetState{
			SetScore:   currentSetScore(match, set),
			IsFinished: set.IsFinished,
		})
		if finishErr != nil {
			return nil, finishErr
		}
		set.IsFinished = true
		if err := repo.SaveSet(set); err != nil {
			return nil, err
		}
		next = result.Game
	case finishMatch:
		result, finishErr := game.ConfirmGameFinished(state)
		next, err = result.Game, finishErr
	default:
		return nil, errUnhandledGameCallback
	}
	if err != nil {
		return nil, err
	}
	applyGameState(match, next)
	if err := repo.SaveGame(match); err != nil {
		return nil, err
	}
	if newSet.SetNumber == 0 {
		return set, nil
	}
	set = &storage.GameSet{
		GameID: match.ID, SetNumber: newSet.SetNumber,
		HomeScore: newSet.HomeScore, GuestScore: newSet.GuestScore,
	}
	if err := repo.CreateSet(set); err != nil {
		return nil, err
	}
	return set, nil
}
