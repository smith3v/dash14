package overlay

import "testing"

func TestBuildIntermissionViewModelIncludesHometown(t *testing.T) {
	home := TeamIdentity{
		Name:      "Home Team",
		ShortName: "HOME",
		Hometown:  "Hoofddorp",
		LogoPath:  "home.png",
	}
	guest := TeamIdentity{
		Name:      "Guest Team",
		ShortName: "GUEST",
		Hometown:  "Haarlem",
		LogoPath:  "guest.png",
	}

	vm := BuildIntermissionViewModel(home, guest, 2, 1, nil)

	if vm.HomeTeamHometown != "Hoofddorp" {
		t.Fatalf("HomeTeamHometown = %q, want %q", vm.HomeTeamHometown, "Hoofddorp")
	}
	if vm.GuestTeamHometown != "Haarlem" {
		t.Fatalf("GuestTeamHometown = %q, want %q", vm.GuestTeamHometown, "Haarlem")
	}
}

func TestBuildFinishedViewModelIncludesHometown(t *testing.T) {
	home := TeamIdentity{
		Name:      "Home Team",
		ShortName: "HOME",
		Hometown:  "Hoofddorp",
		LogoPath:  "home.png",
	}
	guest := TeamIdentity{
		Name:      "Guest Team",
		ShortName: "GUEST",
		Hometown:  "Haarlem",
		LogoPath:  "guest.png",
	}

	vm := BuildFinishedViewModel(home, guest, 3, 1, nil)

	if vm.HomeTeamHometown != "Hoofddorp" {
		t.Fatalf("HomeTeamHometown = %q, want %q", vm.HomeTeamHometown, "Hoofddorp")
	}
	if vm.GuestTeamHometown != "Haarlem" {
		t.Fatalf("GuestTeamHometown = %q, want %q", vm.GuestTeamHometown, "Haarlem")
	}
	if vm.HomeSetsWon != 3 || vm.GuestSetsWon != 1 {
		t.Fatalf("unexpected final sets: home=%d guest=%d", vm.HomeSetsWon, vm.GuestSetsWon)
	}
}
