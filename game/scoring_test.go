package game

import "testing"

// TestScoringIncrementHome verifies that IncrementHome adds one point to the
// home score and that the Finishable flag is set correctly.
func TestScoringIncrementHome(t *testing.T) {
	tests := []struct {
		name       string
		input      SetScore
		wantHome   int
		wantGuest  int
		wantFinish bool
	}{
		{
			name:       "from zero",
			input:      SetScore{HomeScore: 0, GuestScore: 0, SetNumber: 1},
			wantHome:   1,
			wantGuest:  0,
			wantFinish: false,
		},
		{
			name:       "reaches 25 with lead of 2",
			input:      SetScore{HomeScore: 24, GuestScore: 22, SetNumber: 1},
			wantHome:   25,
			wantGuest:  22,
			wantFinish: true,
		},
		{
			name:       "reaches 25 but not 2 ahead",
			input:      SetScore{HomeScore: 24, GuestScore: 24, SetNumber: 1},
			wantHome:   25,
			wantGuest:  24,
			wantFinish: false,
		},
		{
			name:       "deuce resolved at 26-24",
			input:      SetScore{HomeScore: 25, GuestScore: 24, SetNumber: 2},
			wantHome:   26,
			wantGuest:  24,
			wantFinish: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IncrementHome(tc.input)
			if got.Set.HomeScore != tc.wantHome {
				t.Errorf("HomeScore: got %d, want %d", got.Set.HomeScore, tc.wantHome)
			}
			if got.Set.GuestScore != tc.wantGuest {
				t.Errorf("GuestScore: got %d, want %d", got.Set.GuestScore, tc.wantGuest)
			}
			if got.Finishable != tc.wantFinish {
				t.Errorf("Finishable: got %v, want %v", got.Finishable, tc.wantFinish)
			}
			if got.SideSwitch != false {
				t.Errorf("SideSwitch: got %v, want false (sets 1-4 never switch)", got.SideSwitch)
			}
		})
	}
}

// TestScoringIncrementGuest verifies that IncrementGuest adds one point to
// the guest score and that the Finishable flag is set correctly.
func TestScoringIncrementGuest(t *testing.T) {
	tests := []struct {
		name       string
		input      SetScore
		wantHome   int
		wantGuest  int
		wantFinish bool
	}{
		{
			name:       "from zero",
			input:      SetScore{HomeScore: 0, GuestScore: 0, SetNumber: 1},
			wantHome:   0,
			wantGuest:  1,
			wantFinish: false,
		},
		{
			name:       "guest reaches 25 with lead of 2",
			input:      SetScore{HomeScore: 22, GuestScore: 24, SetNumber: 3},
			wantHome:   22,
			wantGuest:  25,
			wantFinish: true,
		},
		{
			name:       "guest reaches 25 but tied",
			input:      SetScore{HomeScore: 24, GuestScore: 24, SetNumber: 4},
			wantHome:   24,
			wantGuest:  25,
			wantFinish: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IncrementGuest(tc.input)
			if got.Set.HomeScore != tc.wantHome {
				t.Errorf("HomeScore: got %d, want %d", got.Set.HomeScore, tc.wantHome)
			}
			if got.Set.GuestScore != tc.wantGuest {
				t.Errorf("GuestScore: got %d, want %d", got.Set.GuestScore, tc.wantGuest)
			}
			if got.Finishable != tc.wantFinish {
				t.Errorf("Finishable: got %v, want %v", got.Finishable, tc.wantFinish)
			}
		})
	}
}

// TestScoringDecrementHome verifies that DecrementHome subtracts one point
// and never goes below zero.
func TestScoringDecrementHome(t *testing.T) {
	tests := []struct {
		name       string
		input      SetScore
		wantHome   int
		wantFinish bool
	}{
		{
			name:       "normal decrement",
			input:      SetScore{HomeScore: 5, GuestScore: 3, SetNumber: 1},
			wantHome:   4,
			wantFinish: false,
		},
		{
			name:       "decrement from 1 goes to 0",
			input:      SetScore{HomeScore: 1, GuestScore: 0, SetNumber: 1},
			wantHome:   0,
			wantFinish: false,
		},
		{
			name:       "decrement from 0 stays at 0",
			input:      SetScore{HomeScore: 0, GuestScore: 0, SetNumber: 1},
			wantHome:   0,
			wantFinish: false,
		},
		{
			name:       "decrement removes finishable state",
			input:      SetScore{HomeScore: 26, GuestScore: 24, SetNumber: 2},
			wantHome:   25,
			wantFinish: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecrementHome(tc.input)
			if got.Set.HomeScore != tc.wantHome {
				t.Errorf("HomeScore: got %d, want %d", got.Set.HomeScore, tc.wantHome)
			}
			if got.Finishable != tc.wantFinish {
				t.Errorf("Finishable: got %v, want %v", got.Finishable, tc.wantFinish)
			}
		})
	}
}

// TestScoringDecrementGuest verifies that DecrementGuest subtracts one point
// and never goes below zero.
func TestScoringDecrementGuest(t *testing.T) {
	tests := []struct {
		name       string
		input      SetScore
		wantGuest  int
		wantFinish bool
	}{
		{
			name:       "normal decrement",
			input:      SetScore{HomeScore: 3, GuestScore: 5, SetNumber: 1},
			wantGuest:  4,
			wantFinish: false,
		},
		{
			name:       "decrement from 1 goes to 0",
			input:      SetScore{HomeScore: 0, GuestScore: 1, SetNumber: 1},
			wantGuest:  0,
			wantFinish: false,
		},
		{
			name:       "decrement from 0 stays at 0",
			input:      SetScore{HomeScore: 0, GuestScore: 0, SetNumber: 1},
			wantGuest:  0,
			wantFinish: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecrementGuest(tc.input)
			if got.Set.GuestScore != tc.wantGuest {
				t.Errorf("GuestScore: got %d, want %d", got.Set.GuestScore, tc.wantGuest)
			}
			if got.Finishable != tc.wantFinish {
				t.Errorf("Finishable: got %v, want %v", got.Finishable, tc.wantFinish)
			}
		})
	}
}

// TestScoringFinishable exercises the isFinishable helper across a broad
// range of score combinations for sets 1–4 (25-point rule).
func TestScoringFinishable(t *testing.T) {
	tests := []struct {
		name       string
		set        SetScore
		wantFinish bool
	}{
		// Not yet at winning threshold
		{name: "0-0 not finishable", set: SetScore{HomeScore: 0, GuestScore: 0, SetNumber: 1}, wantFinish: false},
		{name: "24-22 not finishable", set: SetScore{HomeScore: 24, GuestScore: 22, SetNumber: 1}, wantFinish: false},
		{name: "24-24 deuce not finishable", set: SetScore{HomeScore: 24, GuestScore: 24, SetNumber: 1}, wantFinish: false},

		// Exactly 25 for home with sufficient lead
		{name: "25-0 finishable", set: SetScore{HomeScore: 25, GuestScore: 0, SetNumber: 1}, wantFinish: true},
		{name: "25-22 finishable", set: SetScore{HomeScore: 25, GuestScore: 22, SetNumber: 1}, wantFinish: true},
		{name: "25-23 finishable", set: SetScore{HomeScore: 25, GuestScore: 23, SetNumber: 2}, wantFinish: true},

		// Deuce scenarios at 25
		{name: "25-24 not finishable", set: SetScore{HomeScore: 25, GuestScore: 24, SetNumber: 1}, wantFinish: false},
		{name: "25-25 not finishable", set: SetScore{HomeScore: 25, GuestScore: 25, SetNumber: 3}, wantFinish: false},

		// Extended deuce resolved
		{name: "26-24 finishable", set: SetScore{HomeScore: 26, GuestScore: 24, SetNumber: 1}, wantFinish: true},
		{name: "27-25 finishable", set: SetScore{HomeScore: 27, GuestScore: 25, SetNumber: 4}, wantFinish: true},
		{name: "26-25 not finishable", set: SetScore{HomeScore: 26, GuestScore: 25, SetNumber: 2}, wantFinish: false},

		// Exactly 25 for guest with sufficient lead
		{name: "0-25 finishable", set: SetScore{HomeScore: 0, GuestScore: 25, SetNumber: 2}, wantFinish: true},
		{name: "23-25 finishable", set: SetScore{HomeScore: 23, GuestScore: 25, SetNumber: 3}, wantFinish: true},
		{name: "24-25 not finishable", set: SetScore{HomeScore: 24, GuestScore: 25, SetNumber: 4}, wantFinish: false},
		{name: "24-26 finishable (guest)", set: SetScore{HomeScore: 24, GuestScore: 26, SetNumber: 1}, wantFinish: true},

		// Set 5 uses 15-point threshold (basic coverage; Task 12 will expand)
		{name: "set5 14-12 not finishable", set: SetScore{HomeScore: 14, GuestScore: 12, SetNumber: 5}, wantFinish: false},
		{name: "set5 15-13 finishable", set: SetScore{HomeScore: 15, GuestScore: 13, SetNumber: 5}, wantFinish: true},
		{name: "set5 15-14 not finishable", set: SetScore{HomeScore: 15, GuestScore: 14, SetNumber: 5}, wantFinish: false},
		{name: "set5 16-14 finishable", set: SetScore{HomeScore: 16, GuestScore: 14, SetNumber: 5}, wantFinish: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isFinishable(tc.set)
			if got != tc.wantFinish {
				t.Errorf("isFinishable(%+v) = %v, want %v", tc.set, got, tc.wantFinish)
			}
		})
	}
}

// TestScoringSet5SideSwitch covers the automatic side-switch behaviour in set
// 5: exact trigger at 8, protection against a second switch, deuce after 8,
// and the 15-point finishable rule.
func TestScoringSet5SideSwitch(t *testing.T) {
	tests := []struct {
		name             string
		input            SetScore
		op               func(SetScore) ScoreResult
		wantHome         int
		wantGuest        int
		wantSideSwitch   bool
		wantSwitchedInS5 bool
		wantFinish       bool
	}{
		{
			// Home team score goes from 7 to 8 — side switch must fire.
			name:             "home reaches 8 triggers switch",
			input:            SetScore{HomeScore: 7, GuestScore: 3, SetNumber: 5, SideSwitchedInSet5: false},
			op:               IncrementHome,
			wantHome:         8,
			wantGuest:        3,
			wantSideSwitch:   true,
			wantSwitchedInS5: true,
			wantFinish:       false,
		},
		{
			// Guest team score goes from 7 to 8 — side switch must fire.
			name:             "guest reaches 8 triggers switch",
			input:            SetScore{HomeScore: 5, GuestScore: 7, SetNumber: 5, SideSwitchedInSet5: false},
			op:               IncrementGuest,
			wantHome:         5,
			wantGuest:        8,
			wantSideSwitch:   true,
			wantSwitchedInS5: true,
			wantFinish:       false,
		},
		{
			// Switch already fired — no second emission even as scores rise.
			name:             "already switched no second switch",
			input:            SetScore{HomeScore: 9, GuestScore: 8, SetNumber: 5, SideSwitchedInSet5: true},
			op:               IncrementHome,
			wantHome:         10,
			wantGuest:        8,
			wantSideSwitch:   false,
			wantSwitchedInS5: true,
			wantFinish:       false,
		},
		{
			// Both teams reach 8 (deuce scenario). Switch was triggered when the
			// first team hit 8; this call brings the second team to 8 with
			// SideSwitchedInSet5 already true → no second switch.
			name:             "deuce at 8-8 no second switch",
			input:            SetScore{HomeScore: 8, GuestScore: 7, SetNumber: 5, SideSwitchedInSet5: true},
			op:               IncrementGuest,
			wantHome:         8,
			wantGuest:        8,
			wantSideSwitch:   false,
			wantSwitchedInS5: true,
			wantFinish:       false,
		},
		{
			// Set 5 is finishable at 15-13 (win by ≥ 2, ≥ 15 points).
			name:             "set5 finishable at 15-13",
			input:            SetScore{HomeScore: 14, GuestScore: 13, SetNumber: 5, SideSwitchedInSet5: true},
			op:               IncrementHome,
			wantHome:         15,
			wantGuest:        13,
			wantSideSwitch:   false,
			wantSwitchedInS5: true,
			wantFinish:       true,
		},
		{
			// Set 5 is NOT finishable at 15-14 (lead is only 1).
			name:             "set5 not finishable at 15-14",
			input:            SetScore{HomeScore: 14, GuestScore: 14, SetNumber: 5, SideSwitchedInSet5: true},
			op:               IncrementHome,
			wantHome:         15,
			wantGuest:        14,
			wantSideSwitch:   false,
			wantSwitchedInS5: true,
			wantFinish:       false,
		},
		{
			// Score is still below 8 in set 5 — no switch yet.
			name:             "set5 below 8 no switch",
			input:            SetScore{HomeScore: 6, GuestScore: 5, SetNumber: 5, SideSwitchedInSet5: false},
			op:               IncrementHome,
			wantHome:         7,
			wantGuest:        5,
			wantSideSwitch:   false,
			wantSwitchedInS5: false,
			wantFinish:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.op(tc.input)
			if got.Set.HomeScore != tc.wantHome {
				t.Errorf("HomeScore: got %d, want %d", got.Set.HomeScore, tc.wantHome)
			}
			if got.Set.GuestScore != tc.wantGuest {
				t.Errorf("GuestScore: got %d, want %d", got.Set.GuestScore, tc.wantGuest)
			}
			if got.SideSwitch != tc.wantSideSwitch {
				t.Errorf("SideSwitch: got %v, want %v", got.SideSwitch, tc.wantSideSwitch)
			}
			if got.Set.SideSwitchedInSet5 != tc.wantSwitchedInS5 {
				t.Errorf("SideSwitchedInSet5: got %v, want %v", got.Set.SideSwitchedInSet5, tc.wantSwitchedInS5)
			}
			if got.Finishable != tc.wantFinish {
				t.Errorf("Finishable: got %v, want %v", got.Finishable, tc.wantFinish)
			}
		})
	}
}

// TestScoringDeuceSequence simulates a deuce sequence to make sure the
// Finishable flag transitions correctly as points are added.
func TestScoringDeuceSequence(t *testing.T) {
	s := SetScore{SetNumber: 1}

	// Bring both teams to 24.
	for i := 0; i < 24; i++ {
		s = IncrementHome(s).Set
		s = IncrementGuest(s).Set
	}
	if s.HomeScore != 24 || s.GuestScore != 24 {
		t.Fatalf("expected 24-24, got %d-%d", s.HomeScore, s.GuestScore)
	}

	// 25-24: not finishable.
	r := IncrementHome(s)
	if r.Finishable {
		t.Errorf("25-24 should not be finishable")
	}
	s = r.Set

	// 25-25: not finishable.
	r = IncrementGuest(s)
	if r.Finishable {
		t.Errorf("25-25 should not be finishable")
	}
	s = r.Set

	// 26-25: not finishable.
	r = IncrementHome(s)
	if r.Finishable {
		t.Errorf("26-25 should not be finishable")
	}
	s = r.Set

	// 26-26: not finishable.
	r = IncrementGuest(s)
	if r.Finishable {
		t.Errorf("26-26 should not be finishable")
	}
	s = r.Set

	// 27-26: not finishable.
	r = IncrementHome(s)
	if r.Finishable {
		t.Errorf("27-26 should not be finishable")
	}
	s = r.Set

	// 27-25 via decrement guest to 25 — wait, let's advance home to 27 and
	// guest to 25 the clean way.
	// Current state is 27-26. Guest decrement to 25 → 27-25: finishable.
	r = DecrementGuest(s)
	if !r.Finishable {
		t.Errorf("27-25 should be finishable after decrement, got Finishable=%v", r.Finishable)
	}
}
