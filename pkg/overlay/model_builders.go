package overlay

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
