package intuithub

import (
	"reflect"
	"testing"
)

func TestBuildIntuitHubUser(t *testing.T) {
	row := CollaboratorRow{
		CollaboratorID: 42,
		FullName:       "Pablo Pelardas",
		Email:          " pablo@x.com ",
		Role:           "Editor",
		Teams: []CollaboratorTeam{
			{TeamID: 1, TeamCode: "BACK", Role: "Lead"},
			{TeamID: 2, TeamCode: "DEVOPS", Role: "Member"},
			{TeamID: 3, TeamCode: "", Role: "Member"}, // should be skipped (empty code)
		},
	}
	teamHasFullVis := map[string]bool{
		"DEVOPS": true,
	}
	appTeamRoles := map[string]map[string]string{
		"engram":  {"BACK": "Maintainer"},
		"infra":   {"DEVOPS": "Maintainer"},
		"shared":  {"BACK": "Contributor", "DEVOPS": "Maintainer"},
	}

	u := buildIntuitHubUser(row, teamHasFullVis, appTeamRoles, "deadbeef")

	if u.IntuitCollaboratorID != 42 {
		t.Fatalf("expected collab id 42, got %d", u.IntuitCollaboratorID)
	}
	if u.Email != "pablo@x.com" {
		t.Fatalf("expected trimmed email, got %q", u.Email)
	}
	if !u.HasFullVisibility {
		t.Fatalf("expected HasFullVisibility=true (DEVOPS team has it)")
	}
	if !reflect.DeepEqual(u.TeamCodes, []string{"BACK", "DEVOPS"}) {
		t.Fatalf("unexpected team codes: %v", u.TeamCodes)
	}
	if u.APIKeyHash != "deadbeef" {
		t.Fatalf("api key hash not propagated: %q", u.APIKeyHash)
	}
	if len(u.Memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(u.Memberships))
	}

	// Memberships are sorted by team_code: BACK first, DEVOPS second.
	back := u.Memberships[0]
	if back.TeamCode != "BACK" || back.RoleInTeam != "Lead" {
		t.Fatalf("unexpected BACK membership: %+v", back)
	}
	if len(back.Apps) != 2 {
		t.Fatalf("expected BACK to own 2 apps, got %d (%+v)", len(back.Apps), back.Apps)
	}
	// Apps within a team are sorted by app name: engram, shared.
	if back.Apps[0].ApplicationName != "engram" || back.Apps[0].RoleInApp != "Maintainer" {
		t.Fatalf("unexpected BACK app[0]: %+v", back.Apps[0])
	}
	if back.Apps[1].ApplicationName != "shared" || back.Apps[1].RoleInApp != "Contributor" {
		t.Fatalf("unexpected BACK app[1]: %+v", back.Apps[1])
	}

	devops := u.Memberships[1]
	if devops.TeamCode != "DEVOPS" || devops.RoleInTeam != "Member" {
		t.Fatalf("unexpected DEVOPS membership: %+v", devops)
	}
	if len(devops.Apps) != 2 {
		t.Fatalf("expected DEVOPS to own 2 apps, got %d (%+v)", len(devops.Apps), devops.Apps)
	}
}

func TestBuildIntuitHubUser_NoFullVisibility(t *testing.T) {
	row := CollaboratorRow{
		CollaboratorID: 7,
		Email:          "x@x",
		Teams:          []CollaboratorTeam{{TeamCode: "BACK", Role: "Member"}},
	}
	u := buildIntuitHubUser(row, nil, nil, "")
	if u.HasFullVisibility {
		t.Fatal("expected HasFullVisibility=false")
	}
	if len(u.Memberships) != 1 || len(u.Memberships[0].Apps) != 0 {
		t.Fatalf("expected 1 membership with 0 apps, got %+v", u.Memberships)
	}
	if u.APIKeyHash != "" {
		t.Fatalf("expected empty APIKeyHash, got %q", u.APIKeyHash)
	}
}

func TestHashAPIKey(t *testing.T) {
	if hashAPIKey("") != "" {
		t.Fatal("empty input should produce empty hash")
	}
	if hashAPIKey("   ") != "" {
		t.Fatal("whitespace input should produce empty hash")
	}
	a := hashAPIKey("secret")
	b := hashAPIKey("secret")
	if a != b {
		t.Fatal("hash should be deterministic")
	}
	if a == hashAPIKey("other") {
		t.Fatal("different inputs should produce different hashes")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %d (%q)", len(a), a)
	}
}
