package game

import "testing"

// TestStartPlannedGame verifies that StartPlannedGame transitions the game to
// in_progress and returns a fresh set 1 with score 0-0.
func TestStartPlannedGame(t *testing.T) {
	tests := []struct {
		name       string
		input      GameState
		wantStatus string
		wantSetNum int
		wantHome   int
		wantGuest  int
		wantErr    bool
	}{
		{
			name: "planned game starts correctly",
			input: GameState{
				HomeTeamID:  1,
				GuestTeamID: 2,
				HomeSide:    "left",
				GuestSide:   "right",
				Status:      "planned",
			},
			wantStatus: "in_progress",
			wantSetNum: 1,
			wantHome:   0,
			wantGuest:  0,
			wantErr:    false,
		},
		{
			name: "in_progress game returns error",
			input: GameState{
				Status: "in_progress",
			},
			wantErr: true,
		},
		{
			name: "finished game returns error",
			input: GameState{
				Status: "finished",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, s, err := StartPlannedGame(tc.input)

			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if g.Status != tc.wantStatus {
				t.Errorf("Status: got %q, want %q", g.Status, tc.wantStatus)
			}
			if g.CurrentSetNumber != tc.wantSetNum {
				t.Errorf("CurrentSetNumber: got %d, want %d", g.CurrentSetNumber, tc.wantSetNum)
			}
			if s.SetNumber != tc.wantSetNum {
				t.Errorf("SetState.SetNumber: got %d, want %d", s.SetNumber, tc.wantSetNum)
			}
			if s.HomeScore != tc.wantHome {
				t.Errorf("SetState.HomeScore: got %d, want %d", s.HomeScore, tc.wantHome)
			}
			if s.GuestScore != tc.wantGuest {
				t.Errorf("SetState.GuestScore: got %d, want %d", s.GuestScore, tc.wantGuest)
			}
			if s.IsFinished {
				t.Errorf("SetState.IsFinished: got true, want false")
			}
			if g.HomeSetsWon != 0 || g.GuestSetsWon != 0 {
				t.Errorf("sets won should be 0-0, got home=%d guest=%d",
					g.HomeSetsWon, g.GuestSetsWon)
			}
		})
	}
}

// TestConfirmSetFinished_NextSetCreated verifies that after a finishable set
// is confirmed, the next set is created with an incremented set number and
// the overlay sides are swapped.
func TestConfirmSetFinished_NextSetCreated(t *testing.T) {
	tests := []struct {
		name             string
		game             GameState
		set              SetState
		wantNextSetNum   int
		wantHomeSide     string
		wantGuestSide    string
		wantHomeSetsWon  int
		wantGuestSetsWon int
		wantPromptFinish bool
		wantErr          bool
	}{
		{
			name: "home wins set 1, sides swap, set 2 created",
			game: GameState{
				Status:           "in_progress",
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 1,
				HomeSetsWon:      0,
				GuestSetsWon:     0,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 20,
					SetNumber:  1,
				},
			},
			wantNextSetNum:   2,
			wantHomeSide:     "right",
			wantGuestSide:    "left",
			wantHomeSetsWon:  1,
			wantGuestSetsWon: 0,
			wantPromptFinish: false,
			wantErr:          false,
		},
		{
			name: "guest wins set 2, sides swap back, set 3 created",
			game: GameState{
				Status:           "in_progress",
				HomeSide:         "right",
				GuestSide:        "left",
				CurrentSetNumber: 2,
				HomeSetsWon:      1,
				GuestSetsWon:     0,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  20,
					GuestScore: 25,
					SetNumber:  2,
				},
			},
			wantNextSetNum:   3,
			wantHomeSide:     "left",
			wantGuestSide:    "right",
			wantHomeSetsWon:  1,
			wantGuestSetsWon: 1,
			wantPromptFinish: false,
			wantErr:          false,
		},
		{
			name: "not finishable score returns error",
			game: GameState{
				Status:           "in_progress",
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 1,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  20,
					GuestScore: 20,
					SetNumber:  1,
				},
			},
			wantErr: true,
		},
		{
			name: "already finished set returns error",
			game: GameState{
				Status:           "in_progress",
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 1,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 20,
					SetNumber:  1,
				},
				IsFinished: true,
			},
			wantErr: true,
		},
		{
			name: "set 4 creates set 5 with SideSwitchedInSet5 from game",
			game: GameState{
				Status:             "in_progress",
				HomeSide:           "left",
				GuestSide:          "right",
				CurrentSetNumber:   4,
				HomeSetsWon:        2,
				GuestSetsWon:       1,
				SideSwitchedInSet5: false,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 22,
					SetNumber:  4,
				},
			},
			wantNextSetNum:   5,
			wantHomeSide:     "right",
			wantGuestSide:    "left",
			wantHomeSetsWon:  3,
			wantGuestSetsWon: 1,
			wantPromptFinish: true, // 3 sets won
			wantErr:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConfirmSetFinished(tc.game, tc.set)

			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.PromptFinish != tc.wantPromptFinish {
				t.Errorf("PromptFinish: got %v, want %v", result.PromptFinish, tc.wantPromptFinish)
			}

			if result.Game.HomeSetsWon != tc.wantHomeSetsWon {
				t.Errorf("HomeSetsWon: got %d, want %d", result.Game.HomeSetsWon, tc.wantHomeSetsWon)
			}
			if result.Game.GuestSetsWon != tc.wantGuestSetsWon {
				t.Errorf("GuestSetsWon: got %d, want %d", result.Game.GuestSetsWon, tc.wantGuestSetsWon)
			}

			if tc.wantPromptFinish {
				if result.NextSet != nil {
					t.Errorf("NextSet: got non-nil, want nil when PromptFinish=true")
				}
				return
			}

			if result.NextSet == nil {
				t.Fatalf("NextSet: got nil, want non-nil")
			}
			if result.NextSet.SetNumber != tc.wantNextSetNum {
				t.Errorf("NextSet.SetNumber: got %d, want %d",
					result.NextSet.SetNumber, tc.wantNextSetNum)
			}
			if result.NextSet.IsFinished {
				t.Errorf("NextSet.IsFinished: got true, want false")
			}
			if result.NextSet.HomeScore != 0 || result.NextSet.GuestScore != 0 {
				t.Errorf("NextSet score: got %d-%d, want 0-0",
					result.NextSet.HomeScore, result.NextSet.GuestScore)
			}
			if result.Game.HomeSide != tc.wantHomeSide {
				t.Errorf("HomeSide: got %q, want %q", result.Game.HomeSide, tc.wantHomeSide)
			}
			if result.Game.GuestSide != tc.wantGuestSide {
				t.Errorf("GuestSide: got %q, want %q", result.Game.GuestSide, tc.wantGuestSide)
			}
			if result.Game.CurrentSetNumber != tc.wantNextSetNum {
				t.Errorf("CurrentSetNumber: got %d, want %d",
					result.Game.CurrentSetNumber, tc.wantNextSetNum)
			}
		})
	}
}

// TestConfirmSetFinished_PromptFinish verifies that when a team wins their
// 3rd set, PromptFinish is true and NextSet is nil — the app does not
// auto-finish the match.
func TestConfirmSetFinished_PromptFinish(t *testing.T) {
	tests := []struct {
		name             string
		game             GameState
		set              SetState
		wantHomeSetsWon  int
		wantGuestSetsWon int
	}{
		{
			name: "home team wins 3rd set",
			game: GameState{
				Status:           "in_progress",
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 3,
				HomeSetsWon:      2,
				GuestSetsWon:     0,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 18,
					SetNumber:  3,
				},
			},
			wantHomeSetsWon:  3,
			wantGuestSetsWon: 0,
		},
		{
			name: "guest team wins 3rd set",
			game: GameState{
				Status:           "in_progress",
				HomeSide:         "right",
				GuestSide:        "left",
				CurrentSetNumber: 5,
				HomeSetsWon:      2,
				GuestSetsWon:     2,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  13,
					GuestScore: 15,
					SetNumber:  5,
				},
			},
			wantHomeSetsWon:  2,
			wantGuestSetsWon: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConfirmSetFinished(tc.game, tc.set)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !result.PromptFinish {
				t.Errorf("PromptFinish: got false, want true")
			}
			if result.NextSet != nil {
				t.Errorf("NextSet: got non-nil, want nil — app must not auto-finish")
			}
			if result.Game.Status == "finished" {
				t.Errorf("Game.Status: got %q — app must not auto-finish", result.Game.Status)
			}
			if result.Game.HomeSetsWon != tc.wantHomeSetsWon {
				t.Errorf("HomeSetsWon: got %d, want %d",
					result.Game.HomeSetsWon, tc.wantHomeSetsWon)
			}
			if result.Game.GuestSetsWon != tc.wantGuestSetsWon {
				t.Errorf("GuestSetsWon: got %d, want %d",
					result.Game.GuestSetsWon, tc.wantGuestSetsWon)
			}
		})
	}
}

// TestConfirmGameFinished verifies that ConfirmGameFinished sets status to
// "finished" when a team has 3 sets won, and returns an error otherwise.
func TestConfirmGameFinished(t *testing.T) {
	tests := []struct {
		name       string
		game       GameState
		wantStatus string
		wantErr    bool
	}{
		{
			name: "home team has 3 sets — finishes successfully",
			game: GameState{
				Status:       "in_progress",
				HomeSetsWon:  3,
				GuestSetsWon: 1,
			},
			wantStatus: "finished",
			wantErr:    false,
		},
		{
			name: "guest team has 3 sets — finishes successfully",
			game: GameState{
				Status:       "in_progress",
				HomeSetsWon:  2,
				GuestSetsWon: 3,
			},
			wantStatus: "finished",
			wantErr:    false,
		},
		{
			name: "no team has 3 sets — error returned",
			game: GameState{
				Status:       "in_progress",
				HomeSetsWon:  2,
				GuestSetsWon: 2,
			},
			wantErr: true,
		},
		{
			name: "already finished — error returned",
			game: GameState{
				Status:       "finished",
				HomeSetsWon:  3,
				GuestSetsWon: 1,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConfirmGameFinished(tc.game)

			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Game.Status != tc.wantStatus {
				t.Errorf("Status: got %q, want %q", result.Game.Status, tc.wantStatus)
			}
		})
	}
}

// TestReverseOverlaySides verifies that ReverseOverlaySides swaps HomeSide
// and GuestSide correctly.
func TestReverseOverlaySides(t *testing.T) {
	tests := []struct {
		name          string
		game          GameState
		wantHomeSide  string
		wantGuestSide string
	}{
		{
			name: "left-right becomes right-left",
			game: GameState{
				HomeSide:  "left",
				GuestSide: "right",
			},
			wantHomeSide:  "right",
			wantGuestSide: "left",
		},
		{
			name: "right-left becomes left-right",
			game: GameState{
				HomeSide:  "right",
				GuestSide: "left",
			},
			wantHomeSide:  "left",
			wantGuestSide: "right",
		},
		{
			name: "other fields preserved after reverse",
			game: GameState{
				HomeTeamID:       1,
				GuestTeamID:      2,
				HomeSide:         "left",
				GuestSide:        "right",
				HomeSetsWon:      1,
				GuestSetsWon:     2,
				CurrentSetNumber: 4,
				Status:           "in_progress",
			},
			wantHomeSide:  "right",
			wantGuestSide: "left",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ReverseOverlaySides(tc.game)

			if result.Game.HomeSide != tc.wantHomeSide {
				t.Errorf("HomeSide: got %q, want %q", result.Game.HomeSide, tc.wantHomeSide)
			}
			if result.Game.GuestSide != tc.wantGuestSide {
				t.Errorf("GuestSide: got %q, want %q", result.Game.GuestSide, tc.wantGuestSide)
			}

			// Verify the original is not mutated (value semantics).
			if tc.game.HomeSide == result.Game.HomeSide && tc.game.HomeSide != tc.game.GuestSide {
				t.Errorf("original GameState appears mutated (HomeSide unchanged)")
			}
		})
	}
}

// TestIsSetFinishable verifies the exported helper behaves identically to
// the internal isFinishable function.
func TestIsSetFinishable(t *testing.T) {
	tests := []struct {
		name string
		set  SetScore
		want bool
	}{
		{
			name: "set 1: 25-23 finishable",
			set:  SetScore{HomeScore: 25, GuestScore: 23, SetNumber: 1},
			want: true,
		},
		{
			name: "set 1: 25-24 not finishable",
			set:  SetScore{HomeScore: 25, GuestScore: 24, SetNumber: 1},
			want: false,
		},
		{
			name: "set 5: 15-13 finishable",
			set:  SetScore{HomeScore: 15, GuestScore: 13, SetNumber: 5},
			want: true,
		},
		{
			name: "set 5: 15-14 not finishable",
			set:  SetScore{HomeScore: 15, GuestScore: 14, SetNumber: 5},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSetFinishable(tc.set)
			if got != tc.want {
				t.Errorf("IsSetFinishable(%+v) = %v, want %v", tc.set, got, tc.want)
			}
		})
	}
}

// TestSet5SideSwitchedCarryOver verifies that when set 5 is created via
// ConfirmSetFinished, the SideSwitchedInSet5 flag from the game state is
// carried into the new SetState.
func TestSet5SideSwitchedCarryOver(t *testing.T) {
	game := GameState{
		Status:             "in_progress",
		HomeSide:           "left",
		GuestSide:          "right",
		CurrentSetNumber:   4,
		HomeSetsWon:        1,
		GuestSetsWon:       2,
		SideSwitchedInSet5: false,
	}
	set := SetState{
		SetScore: SetScore{
			HomeScore:  25,
			GuestScore: 22,
			SetNumber:  4,
		},
	}

	result, err := ConfirmSetFinished(game, set)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NextSet == nil {
		t.Fatalf("NextSet is nil, expected set 5 to be created")
	}
	if result.NextSet.SetNumber != 5 {
		t.Errorf("NextSet.SetNumber: got %d, want 5", result.NextSet.SetNumber)
	}
	// SideSwitchedInSet5 should come from game.SideSwitchedInSet5 (false here).
	if result.NextSet.SideSwitchedInSet5 != game.SideSwitchedInSet5 {
		t.Errorf("NextSet.SideSwitchedInSet5: got %v, want %v",
			result.NextSet.SideSwitchedInSet5, game.SideSwitchedInSet5)
	}
}
