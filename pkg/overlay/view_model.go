// Package overlay provides template rendering for the OBS browser source overlay.
package overlay

// TeamIdentity holds the team fields reused across overlay view models.
type TeamIdentity struct {
	Name      string
	ShortName string
	LogoPath  string
}

// SetScoreViewModel holds the score line for a single set.
type SetScoreViewModel struct {
	SetNumber  int
	HomeScore  int
	GuestScore int
}

// PlannedViewModel holds the presentation data for a game in the planned state.
// It is passed to the planned HTML template for rendering.
type PlannedViewModel struct {
	// HomeTeamName is the full name of the home team.
	HomeTeamName string

	// HomeTeamShortName is the abbreviated name of the home team.
	HomeTeamShortName string

	// HomeTeamLogoPath is the filesystem path to the home team logo image.
	// Empty string means no logo is available.
	HomeTeamLogoPath string

	// GuestTeamName is the full name of the guest team.
	GuestTeamName string

	// GuestTeamShortName is the abbreviated name of the guest team.
	GuestTeamShortName string

	// GuestTeamLogoPath is the filesystem path to the guest team logo image.
	// Empty string means no logo is available.
	GuestTeamLogoPath string
}

// LiveViewModel holds the presentation data for a game in progress.
// It extends PlannedViewModel with score and set information, and exposes
// left/right side fields so templates do not need to apply side-swap logic.
type LiveViewModel struct {
	// HomeTeamName is the full name of the home team.
	HomeTeamName string

	// HomeTeamShortName is the abbreviated name of the home team.
	HomeTeamShortName string

	// HomeTeamLogoPath is the filesystem path to the home team logo image.
	// Empty string means no logo is available.
	HomeTeamLogoPath string

	// GuestTeamName is the full name of the guest team.
	GuestTeamName string

	// GuestTeamShortName is the abbreviated name of the guest team.
	GuestTeamShortName string

	// GuestTeamLogoPath is the filesystem path to the guest team logo image.
	// Empty string means no logo is available.
	GuestTeamLogoPath string

	// HomeScore is the current set point tally for the home team.
	HomeScore int

	// GuestScore is the current set point tally for the guest team.
	GuestScore int

	// HomeSetsWon is the number of sets won by the home team.
	HomeSetsWon int

	// GuestSetsWon is the number of sets won by the guest team.
	GuestSetsWon int

	// CurrentSetNumber is the 1-based index of the active set (1–5).
	CurrentSetNumber int

	// LeftTeamName is the name of the team displayed on the left side of the
	// overlay. This may be either the home or guest team depending on the
	// side assignment for the current game.
	LeftTeamName string

	// LeftTeamLabel identifies whether the left-side team is the home or guest
	// team for the current game.
	LeftTeamLabel string

	// RightTeamName is the name of the team displayed on the right side of
	// the overlay.
	RightTeamName string

	// RightTeamLabel identifies whether the right-side team is the home or
	// guest team for the current game.
	RightTeamLabel string

	// LeftScore is the current set point tally for the left-side team.
	LeftScore int

	// RightScore is the current set point tally for the right-side team.
	RightScore int

	// LeftSetsWon is the number of sets won by the left-side team.
	LeftSetsWon int

	// RightSetsWon is the number of sets won by the right-side team.
	RightSetsWon int
}

// IntermissionViewModel holds the presentation data for the scoreboard shown
// before the game and between sets.
type IntermissionViewModel struct {
	// HomeTeamName is the full name of the home team.
	HomeTeamName string

	// HomeTeamShortName is the abbreviated name of the home team.
	HomeTeamShortName string

	// HomeTeamLogoPath is the filesystem path to the home team logo image.
	// Empty string means no logo is available.
	HomeTeamLogoPath string

	// GuestTeamName is the full name of the guest team.
	GuestTeamName string

	// GuestTeamShortName is the abbreviated name of the guest team.
	GuestTeamShortName string

	// GuestTeamLogoPath is the filesystem path to the guest team logo image.
	// Empty string means no logo is available.
	GuestTeamLogoPath string

	// HomeSetsWon is the total number of sets won by the home team.
	HomeSetsWon int

	// GuestSetsWon is the total number of sets won by the guest team.
	GuestSetsWon int

	// SetScores contains all played sets, including the active unfinished set.
	SetScores []SetScoreViewModel
}
