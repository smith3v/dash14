package overlay

import "github.com/smith3v/dash14/pkg/storage"

// BuildLiveViewModel converts game state into the live overlay view model.
func BuildLiveViewModel(
	home TeamIdentity,
	guest TeamIdentity,
	homeTeamSide string,
	homeScore int,
	guestScore int,
	homeSetsWon int,
	guestSetsWon int,
	currentSetNumber int,
) LiveViewModel {
	leftName := home.Name
	leftLabel := "Home Team"
	rightName := guest.Name
	rightLabel := "Guest Team"
	leftScore := homeScore
	rightScore := guestScore
	leftSets := homeSetsWon
	rightSets := guestSetsWon
	if homeTeamSide == "right" {
		leftName = guest.Name
		leftLabel = "Guest Team"
		rightName = home.Name
		rightLabel = "Home Team"
		leftScore = guestScore
		rightScore = homeScore
		leftSets = guestSetsWon
		rightSets = homeSetsWon
	}

	return LiveViewModel{
		HomeTeamName:       home.Name,
		HomeTeamShortName:  home.ShortName,
		HomeTeamLogoPath:   home.LogoPath,
		GuestTeamName:      guest.Name,
		GuestTeamShortName: guest.ShortName,
		GuestTeamLogoPath:  guest.LogoPath,
		HomeScore:          homeScore,
		GuestScore:         guestScore,
		HomeSetsWon:        homeSetsWon,
		GuestSetsWon:       guestSetsWon,
		CurrentSetNumber:   currentSetNumber,
		LeftTeamName:       leftName,
		LeftTeamLabel:      leftLabel,
		RightTeamName:      rightName,
		RightTeamLabel:     rightLabel,
		LeftScore:          leftScore,
		RightScore:         rightScore,
		LeftSetsWon:        leftSets,
		RightSetsWon:       rightSets,
	}
}

// BuildIntermissionViewModel converts game state and set history into the
// intermission overlay view model.
func BuildIntermissionViewModel(
	home TeamIdentity,
	guest TeamIdentity,
	homeSetsWon int,
	guestSetsWon int,
	sets []SetScoreViewModel,
) IntermissionViewModel {
	return IntermissionViewModel{
		HomeTeamName:       home.Name,
		HomeTeamShortName:  home.ShortName,
		HomeTeamLogoPath:   home.LogoPath,
		GuestTeamName:      guest.Name,
		GuestTeamShortName: guest.ShortName,
		GuestTeamLogoPath:  guest.LogoPath,
		HomeSetsWon:        homeSetsWon,
		GuestSetsWon:       guestSetsWon,
		SetScores:          sets,
	}
}

// BuildSetScoreHistory converts persisted sets into the intermission-visible
// score history. Unstarted placeholder sets are excluded.
func BuildSetScoreHistory(sets []storage.GameSet) []SetScoreViewModel {
	history := make([]SetScoreViewModel, 0, len(sets))
	for _, set := range sets {
		if !set.IsFinished && set.HomeScore == 0 && set.GuestScore == 0 {
			continue
		}
		history = append(history, SetScoreViewModel{
			SetNumber:  set.SetNumber,
			HomeScore:  set.HomeScore,
			GuestScore: set.GuestScore,
		})
	}
	return history
}
