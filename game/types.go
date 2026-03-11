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
