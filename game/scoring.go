package game

// IncrementHome adds one point to the home team score and returns the updated
// ScoreResult.
func IncrementHome(s SetScore) ScoreResult {
	s.HomeScore++
	return buildResult(s)
}

// DecrementHome subtracts one point from the home team score. The score will
// not go below zero.
func DecrementHome(s SetScore) ScoreResult {
	if s.HomeScore > 0 {
		s.HomeScore--
	}
	return buildResult(s)
}

// IncrementGuest adds one point to the guest team score and returns the
// updated ScoreResult.
func IncrementGuest(s SetScore) ScoreResult {
	s.GuestScore++
	return buildResult(s)
}

// DecrementGuest subtracts one point from the guest team score. The score
// will not go below zero.
func DecrementGuest(s SetScore) ScoreResult {
	if s.GuestScore > 0 {
		s.GuestScore--
	}
	return buildResult(s)
}

// buildResult assembles a ScoreResult from the current SetScore.
//
// For set 5, SideSwitch is set to true the first time either team reaches
// 8 points. Once s.SideSwitchedInSet5 is already true, SideSwitch will not
// fire again. When a side switch is emitted the returned Set carries
// SideSwitchedInSet5=true so the caller can persist the updated state.
func buildResult(s SetScore) ScoreResult {
	sideSwitch := false
	if s.SetNumber == 5 && !s.SideSwitchedInSet5 && (s.HomeScore >= 8 || s.GuestScore >= 8) {
		sideSwitch = true
		s.SideSwitchedInSet5 = true
	}
	return ScoreResult{
		Set:        s,
		Finishable: isFinishable(s),
		SideSwitch: sideSwitch,
	}
}

// isFinishable reports whether the current score qualifies an admin to confirm
// the set as finished.
//
// Sets 1–4: first to 25, win by ≥ 2.
// Set 5:    first to 15, win by ≥ 2 (side-switch logic is handled separately
//
//	in Task 12 and does not affect finishability).
func isFinishable(s SetScore) bool {
	threshold := 25
	if s.SetNumber == 5 {
		threshold = 15
	}

	home, guest := s.HomeScore, s.GuestScore
	diff := home - guest
	if diff < 0 {
		diff = -diff
	}

	if diff < 2 {
		return false
	}
	if home >= threshold || guest >= threshold {
		return true
	}
	return false
}
