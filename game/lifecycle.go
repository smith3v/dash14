package game

import "fmt"

const (
	minSetsBeforeFinish = 4
)

// IsSetFinishable returns whether the given SetScore can be confirmed as
// finished. It is the exported equivalent of the unexported isFinishable
// helper used by the scoring layer.
func IsSetFinishable(s SetScore) bool {
	return isFinishable(s)
}

// StartPlannedGame transitions a game from planned to in_progress.
// Returns the updated GameState and the initial SetState (set 1, 0-0).
// Returns an error if the game is not in "planned" status.
func StartPlannedGame(g GameState) (GameState, SetState, error) {
	if g.Status != "planned" {
		return GameState{}, SetState{}, fmt.Errorf(
			"game: StartPlannedGame requires status %q, got %q",
			"planned", g.Status,
		)
	}

	g.Status = "in_progress"
	g.CurrentSetNumber = 1
	g.HomeSetsWon = 0
	g.GuestSetsWon = 0
	g.SideSwitchedInSet5 = false

	s := SetState{
		SetScore: SetScore{
			SetNumber: 1,
		},
	}

	return g, s, nil
}

// ConfirmSetFinished marks the current set as done, updates sets won,
// swaps overlay sides for the next set, and creates the next set if needed.
// Returns an error if the set is not finishable (isFinishable returns false)
// or if the set is already finished.
func ConfirmSetFinished(g GameState, s SetState) (ConfirmSetResult, error) {
	if s.IsFinished {
		return ConfirmSetResult{}, fmt.Errorf(
			"game: ConfirmSetFinished: set %d is already finished",
			s.SetNumber,
		)
	}
	if !isFinishable(s.SetScore) {
		return ConfirmSetResult{}, fmt.Errorf(
			"game: ConfirmSetFinished: set %d score %d-%d is not finishable",
			s.SetNumber, s.HomeScore, s.GuestScore,
		)
	}

	// Mark the set finished.
	s.IsFinished = true

	// Increment the winning team's sets won counter.
	if s.HomeScore > s.GuestScore {
		g.HomeSetsWon++
	} else {
		g.GuestSetsWon++
	}

	// We always play at least four sets. A fifth set is played only if the
	// set score is 2-2 after four completed sets.
	if IsGameFinishEligible(g) {
		return ConfirmSetResult{
			Game:         g,
			NextSet:      nil,
			PromptFinish: true,
		}, nil
	}

	// Swap overlay sides for the next set.
	g.HomeSide, g.GuestSide = g.GuestSide, g.HomeSide

	// Advance to the next set.
	g.CurrentSetNumber++

	// Build the new SetState.
	nextSet := &SetState{
		SetScore: SetScore{
			SetNumber: g.CurrentSetNumber,
		},
	}
	// For set 5, carry over SideSwitchedInSet5 from the game state.
	if g.CurrentSetNumber == 5 {
		nextSet.SetScore.SideSwitchedInSet5 = g.SideSwitchedInSet5
	}

	return ConfirmSetResult{
		Game:         g,
		NextSet:      nextSet,
		PromptFinish: false,
	}, nil
}

// IsGameFinishEligible reports whether the match can be confirmed finished
// under the competition rule:
//   - at least four completed sets are required
//   - if four sets are completed at 2-2, set 5 must be played
func IsGameFinishEligible(g GameState) bool {
	completedSets := g.HomeSetsWon + g.GuestSetsWon
	if completedSets < minSetsBeforeFinish {
		return false
	}
	if completedSets == minSetsBeforeFinish {
		return !(g.HomeSetsWon == 2 && g.GuestSetsWon == 2)
	}
	return true
}

// ConfirmGameFinished marks the game as finished.
// Returns an error if the game has not met finish eligibility yet, or if the
// game is already finished.
func ConfirmGameFinished(g GameState) (ConfirmGameResult, error) {
	if g.Status == "finished" {
		return ConfirmGameResult{}, fmt.Errorf(
			"game: ConfirmGameFinished: game is already finished",
		)
	}

	if !IsGameFinishEligible(g) {
		return ConfirmGameResult{}, fmt.Errorf(
			"game: ConfirmGameFinished: not eligible yet (home=%d, guest=%d, completed=%d)",
			g.HomeSetsWon, g.GuestSetsWon, g.HomeSetsWon+g.GuestSetsWon,
		)
	}

	g.Status = "finished"
	return ConfirmGameResult{Game: g}, nil
}

// ReverseOverlaySides swaps HomeSide and GuestSide in the GameState.
// Can be called at any time.
func ReverseOverlaySides(g GameState) ReverseResult {
	g.HomeSide, g.GuestSide = g.GuestSide, g.HomeSide
	return ReverseResult{Game: g}
}
