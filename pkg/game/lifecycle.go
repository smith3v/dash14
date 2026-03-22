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
	if phaseOrStatus(g) != PhasePlanned {
		return GameState{}, SetState{}, fmt.Errorf(
			"game: StartPlannedGame requires phase %q, got %q",
			PhasePlanned, phaseOrStatus(g),
		)
	}

	g.Status = "in_progress"
	g.Phase = PhaseSetInProgress
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

// ConfirmSetFinished marks the current set as done and updates sets won.
// If the match continues, the game moves into the between-sets phase and the
// next set is not created until StartNextSet is called explicitly.
// Returns an error if the set is not finishable (isFinishable returns false)
// or if the set is already finished.
func ConfirmSetFinished(g GameState, s SetState) (ConfirmSetResult, error) {
	if phaseOrStatus(g) != PhaseSetInProgress {
		return ConfirmSetResult{}, fmt.Errorf(
			"game: ConfirmSetFinished requires phase %q, got %q",
			PhaseSetInProgress, phaseOrStatus(g),
		)
	}
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
	g.Phase = PhaseBetweenSets
	if IsGameFinishEligible(g) {
		return ConfirmSetResult{
			Game:         g,
			NextSet:      nil,
			PromptFinish: true,
		}, nil
	}

	return ConfirmSetResult{
		Game:         g,
		NextSet:      nil,
		PromptFinish: false,
	}, nil
}

// StartNextSet transitions a between-sets game into active play again.
// It increments the current set number, swaps sides for the new set, and
// returns the fresh next SetState.
func StartNextSet(g GameState) (GameState, SetState, error) {
	if phaseOrStatus(g) != PhaseBetweenSets {
		return GameState{}, SetState{}, fmt.Errorf(
			"game: StartNextSet requires phase %q, got %q",
			PhaseBetweenSets, phaseOrStatus(g),
		)
	}
	if IsGameFinishEligible(g) {
		return GameState{}, SetState{}, fmt.Errorf(
			"game: StartNextSet: game is already finish-eligible",
		)
	}

	g.HomeSide, g.GuestSide = g.GuestSide, g.HomeSide
	g.CurrentSetNumber++
	g.Phase = PhaseSetInProgress

	s := SetState{
		SetScore: SetScore{
			SetNumber: g.CurrentSetNumber,
		},
	}
	if g.CurrentSetNumber == 5 {
		s.SideSwitchedInSet5 = g.SideSwitchedInSet5
	}

	return g, s, nil
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
	if phaseOrStatus(g) == PhaseFinished || g.Status == "finished" {
		return ConfirmGameResult{}, fmt.Errorf(
			"game: ConfirmGameFinished: game is already finished",
		)
	}
	if phaseOrStatus(g) != PhaseBetweenSets {
		return ConfirmGameResult{}, fmt.Errorf(
			"game: ConfirmGameFinished requires phase %q, got %q",
			PhaseBetweenSets, phaseOrStatus(g),
		)
	}

	if !IsGameFinishEligible(g) {
		return ConfirmGameResult{}, fmt.Errorf(
			"game: ConfirmGameFinished: not eligible yet (home=%d, guest=%d, completed=%d)",
			g.HomeSetsWon, g.GuestSetsWon, g.HomeSetsWon+g.GuestSetsWon,
		)
	}

	g.Status = "finished"
	g.Phase = PhaseFinished
	return ConfirmGameResult{Game: g}, nil
}

// ReverseOverlaySides swaps HomeSide and GuestSide in the GameState.
// Can be called at any time.
func ReverseOverlaySides(g GameState) ReverseResult {
	g.HomeSide, g.GuestSide = g.GuestSide, g.HomeSide
	return ReverseResult{Game: g}
}

func phaseOrStatus(g GameState) string {
	if g.Phase != "" {
		return g.Phase
	}
	switch g.Status {
	case "planned":
		return PhasePlanned
	case "finished":
		return PhaseFinished
	case "in_progress":
		return PhaseSetInProgress
	default:
		return g.Status
	}
}
