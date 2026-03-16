package game

// SetScore represents the score state of a single set.
type SetScore struct {
	HomeScore          int
	GuestScore         int
	SetNumber          int  // 1-5
	SideSwitchedInSet5 bool // true once the automatic side switch fires in set 5
}

// ScoreResult is returned by scoring functions and captures the updated set
// state together with derived flags that the caller may act on.
type ScoreResult struct {
	Set        SetScore
	Finishable bool // can an admin confirm this set is done?
	SideSwitch bool // used in set 5 when either team reaches 8 points
}

// GameState represents the mutable state of a game (pure, no DB references).
type GameState struct {
	HomeTeamID         uint
	GuestTeamID        uint
	HomeSide           string // "left" or "right"
	GuestSide          string // "left" or "right"
	HomeSetsWon        int
	GuestSetsWon       int
	CurrentSetNumber   int
	Status             string // "planned", "in_progress", "finished"
	SideSwitchedInSet5 bool   // propagated to SetScore when set 5 starts
}

// SetState represents the mutable state of the active set (pure).
type SetState struct {
	SetScore   // embedded
	IsFinished bool
}

// ConfirmSetResult is returned by ConfirmSetFinished.
type ConfirmSetResult struct {
	Game         GameState
	NextSet      *SetState // nil if match is over (game needs to be finished)
	PromptFinish bool      // true if match became finish-eligible, admin must confirm finish
}

// ConfirmGameResult is returned by ConfirmGameFinished.
type ConfirmGameResult struct {
	Game GameState
}

// ReverseResult is returned by ReverseOverlaySides.
type ReverseResult struct {
	Game GameState
}
