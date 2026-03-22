package game

import "testing"

func TestStartPlannedGame(t *testing.T) {
	tests := []struct {
		name      string
		input     GameState
		wantPhase string
		wantErr   bool
	}{
		{
			name: "planned game starts correctly",
			input: GameState{
				HomeTeamID:  1,
				GuestTeamID: 2,
				HomeSide:    "left",
				GuestSide:   "right",
				Status:      "planned",
				Phase:       PhasePlanned,
			},
			wantPhase: PhaseSetInProgress,
		},
		{
			name: "legacy planned status without explicit phase still starts",
			input: GameState{
				Status: "planned",
			},
			wantPhase: PhaseSetInProgress,
		},
		{
			name: "already live returns error",
			input: GameState{
				Status: "in_progress",
				Phase:  PhaseSetInProgress,
			},
			wantErr: true,
		},
		{
			name: "finished returns error",
			input: GameState{
				Status: "finished",
				Phase:  PhaseFinished,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, s, err := StartPlannedGame(tc.input)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if g.Status != "in_progress" {
				t.Fatalf("Status: got %q, want %q", g.Status, "in_progress")
			}
			if g.Phase != tc.wantPhase {
				t.Fatalf("Phase: got %q, want %q", g.Phase, tc.wantPhase)
			}
			if g.CurrentSetNumber != 1 {
				t.Fatalf("CurrentSetNumber: got %d, want 1", g.CurrentSetNumber)
			}
			if s.SetNumber != 1 || s.HomeScore != 0 || s.GuestScore != 0 || s.IsFinished {
				t.Fatalf("unexpected initial set state: %+v", s)
			}
		})
	}
}

func TestConfirmSetFinished(t *testing.T) {
	tests := []struct {
		name             string
		game             GameState
		set              SetState
		wantPhase        string
		wantHomeSetsWon  int
		wantGuestSetsWon int
		wantPromptFinish bool
		wantCurrentSet   int
		wantErr          bool
	}{
		{
			name: "home wins and game enters between_sets",
			game: GameState{
				Status:           "in_progress",
				Phase:            PhaseSetInProgress,
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 3,
				HomeSetsWon:      1,
				GuestSetsWon:     1,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 20,
					SetNumber:  3,
				},
			},
			wantPhase:        PhaseBetweenSets,
			wantHomeSetsWon:  2,
			wantGuestSetsWon: 1,
			wantCurrentSet:   3,
		},
		{
			name: "finish-eligible set still stays between_sets and prompts finish",
			game: GameState{
				Status:           "in_progress",
				Phase:            PhaseSetInProgress,
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 4,
				HomeSetsWon:      2,
				GuestSetsWon:     1,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 18,
					SetNumber:  4,
				},
			},
			wantPhase:        PhaseBetweenSets,
			wantHomeSetsWon:  3,
			wantGuestSetsWon: 1,
			wantPromptFinish: true,
			wantCurrentSet:   4,
		},
		{
			name: "legacy in_progress status without explicit phase still works",
			game: GameState{
				Status:           "in_progress",
				CurrentSetNumber: 1,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 22,
					SetNumber:  1,
				},
			},
			wantPhase:        PhaseBetweenSets,
			wantHomeSetsWon:  1,
			wantGuestSetsWon: 0,
			wantCurrentSet:   1,
		},
		{
			name: "not finishable score returns error",
			game: GameState{
				Status: "in_progress",
				Phase:  PhaseSetInProgress,
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
			name: "wrong phase returns error",
			game: GameState{
				Status: "in_progress",
				Phase:  PhaseBetweenSets,
			},
			set: SetState{
				SetScore: SetScore{
					HomeScore:  25,
					GuestScore: 20,
					SetNumber:  1,
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConfirmSetFinished(tc.game, tc.set)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Game.Phase != tc.wantPhase {
				t.Fatalf("Game.Phase: got %q, want %q", result.Game.Phase, tc.wantPhase)
			}
			if result.Game.CurrentSetNumber != tc.wantCurrentSet {
				t.Fatalf("CurrentSetNumber: got %d, want %d", result.Game.CurrentSetNumber, tc.wantCurrentSet)
			}
			if result.Game.HomeSetsWon != tc.wantHomeSetsWon {
				t.Fatalf("HomeSetsWon: got %d, want %d", result.Game.HomeSetsWon, tc.wantHomeSetsWon)
			}
			if result.Game.GuestSetsWon != tc.wantGuestSetsWon {
				t.Fatalf("GuestSetsWon: got %d, want %d", result.Game.GuestSetsWon, tc.wantGuestSetsWon)
			}
			if result.PromptFinish != tc.wantPromptFinish {
				t.Fatalf("PromptFinish: got %v, want %v", result.PromptFinish, tc.wantPromptFinish)
			}
			if result.NextSet != nil {
				t.Fatalf("NextSet: got %+v, want nil", result.NextSet)
			}
		})
	}
}

func TestStartNextSet(t *testing.T) {
	tests := []struct {
		name          string
		game          GameState
		wantPhase     string
		wantSetNumber int
		wantHomeSide  string
		wantGuestSide string
		wantCarrySide bool
		wantErr       bool
	}{
		{
			name: "between sets starts next set and swaps sides",
			game: GameState{
				Status:           "in_progress",
				Phase:            PhaseBetweenSets,
				HomeSide:         "left",
				GuestSide:        "right",
				CurrentSetNumber: 2,
				HomeSetsWon:      1,
				GuestSetsWon:     1,
			},
			wantPhase:     PhaseSetInProgress,
			wantSetNumber: 3,
			wantHomeSide:  "right",
			wantGuestSide: "left",
		},
		{
			name: "starting set 5 carries side-switched flag",
			game: GameState{
				Status:             "in_progress",
				Phase:              PhaseBetweenSets,
				HomeSide:           "left",
				GuestSide:          "right",
				CurrentSetNumber:   4,
				HomeSetsWon:        2,
				GuestSetsWon:       2,
				SideSwitchedInSet5: true,
			},
			wantPhase:     PhaseSetInProgress,
			wantSetNumber: 5,
			wantHomeSide:  "right",
			wantGuestSide: "left",
			wantCarrySide: true,
		},
		{
			name: "legacy in_progress status without explicit phase is rejected",
			game: GameState{
				Status:           "in_progress",
				CurrentSetNumber: 3,
			},
			wantErr: true,
		},
		{
			name: "finish-eligible game cannot start another set",
			game: GameState{
				Status:       "in_progress",
				Phase:        PhaseBetweenSets,
				HomeSetsWon:  3,
				GuestSetsWon: 1,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, s, err := StartNextSet(tc.game)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if g.Phase != tc.wantPhase {
				t.Fatalf("Game.Phase: got %q, want %q", g.Phase, tc.wantPhase)
			}
			if g.CurrentSetNumber != tc.wantSetNumber {
				t.Fatalf("CurrentSetNumber: got %d, want %d", g.CurrentSetNumber, tc.wantSetNumber)
			}
			if g.HomeSide != tc.wantHomeSide || g.GuestSide != tc.wantGuestSide {
				t.Fatalf("unexpected side swap: home=%q guest=%q", g.HomeSide, g.GuestSide)
			}
			if s.SetNumber != tc.wantSetNumber || s.HomeScore != 0 || s.GuestScore != 0 || s.IsFinished {
				t.Fatalf("unexpected next set state: %+v", s)
			}
			if s.SideSwitchedInSet5 != tc.wantCarrySide {
				t.Fatalf("SideSwitchedInSet5: got %v, want %v", s.SideSwitchedInSet5, tc.wantCarrySide)
			}
		})
	}
}

func TestConfirmGameFinished(t *testing.T) {
	tests := []struct {
		name       string
		game       GameState
		wantStatus string
		wantPhase  string
		wantErr    bool
	}{
		{
			name: "between sets and finish eligible succeeds",
			game: GameState{
				Status:       "in_progress",
				Phase:        PhaseBetweenSets,
				HomeSetsWon:  3,
				GuestSetsWon: 1,
			},
			wantStatus: "finished",
			wantPhase:  PhaseFinished,
		},
		{
			name: "legacy finish eligible without explicit phase is rejected",
			game: GameState{
				Status:       "in_progress",
				HomeSetsWon:  3,
				GuestSetsWon: 1,
			},
			wantErr: true,
		},
		{
			name: "live phase cannot be finished directly",
			game: GameState{
				Status:       "in_progress",
				Phase:        PhaseSetInProgress,
				HomeSetsWon:  3,
				GuestSetsWon: 1,
			},
			wantErr: true,
		},
		{
			name: "not finish eligible returns error",
			game: GameState{
				Status:       "in_progress",
				Phase:        PhaseBetweenSets,
				HomeSetsWon:  2,
				GuestSetsWon: 2,
			},
			wantErr: true,
		},
		{
			name: "already finished returns error",
			game: GameState{
				Status:       "finished",
				Phase:        PhaseFinished,
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
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Game.Status != tc.wantStatus {
				t.Fatalf("Status: got %q, want %q", result.Game.Status, tc.wantStatus)
			}
			if result.Game.Phase != tc.wantPhase {
				t.Fatalf("Phase: got %q, want %q", result.Game.Phase, tc.wantPhase)
			}
		})
	}
}

func TestReverseOverlaySides(t *testing.T) {
	result := ReverseOverlaySides(GameState{
		HomeSide:         "left",
		GuestSide:        "right",
		CurrentSetNumber: 4,
		Status:           "in_progress",
		Phase:            PhaseSetInProgress,
	})

	if result.Game.HomeSide != "right" || result.Game.GuestSide != "left" {
		t.Fatalf("unexpected side reversal: %+v", result.Game)
	}
	if result.Game.CurrentSetNumber != 4 || result.Game.Phase != PhaseSetInProgress {
		t.Fatalf("unexpected mutation of other fields: %+v", result.Game)
	}
}

func TestIsSetFinishable(t *testing.T) {
	tests := []struct {
		name string
		set  SetScore
		want bool
	}{
		{name: "set 1: 25-23 finishable", set: SetScore{HomeScore: 25, GuestScore: 23, SetNumber: 1}, want: true},
		{name: "set 1: 25-24 not finishable", set: SetScore{HomeScore: 25, GuestScore: 24, SetNumber: 1}, want: false},
		{name: "set 5: 15-13 finishable", set: SetScore{HomeScore: 15, GuestScore: 13, SetNumber: 5}, want: true},
		{name: "set 5: 15-14 not finishable", set: SetScore{HomeScore: 15, GuestScore: 14, SetNumber: 5}, want: false},
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

func TestIsGameFinishEligible(t *testing.T) {
	tests := []struct {
		name string
		game GameState
		want bool
	}{
		{name: "3-0 after three sets is not eligible", game: GameState{HomeSetsWon: 3, GuestSetsWon: 0}, want: false},
		{name: "3-1 after four sets is eligible", game: GameState{HomeSetsWon: 3, GuestSetsWon: 1}, want: true},
		{name: "2-2 after four sets is not eligible", game: GameState{HomeSetsWon: 2, GuestSetsWon: 2}, want: false},
		{name: "3-2 after five sets is eligible", game: GameState{HomeSetsWon: 3, GuestSetsWon: 2}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsGameFinishEligible(tc.game)
			if got != tc.want {
				t.Errorf("IsGameFinishEligible(%+v) = %v, want %v", tc.game, got, tc.want)
			}
		})
	}
}
