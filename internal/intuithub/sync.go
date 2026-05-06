package intuithub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Gentleman-Programming/engram/internal/cloud/cloudstore"
)

// Syncer pulls organizational data from IntuitHub and hydrates the local
// cloudstore mirror tables. Construct with NewSyncer.
type Syncer struct {
	client   *Client
	store    *cloudstore.CloudStore
	adminKey string
}

// NewSyncer wires a syncer against an IntuitHub client + a cloudstore.
// adminKey must belong to a collaborator that is a member of a team with
// HasFullVisibility = true (otherwise IntuitHub's visibility filter returns
// only the bot's own teams).
func NewSyncer(client *Client, store *cloudstore.CloudStore, adminKey string) *Syncer {
	return &Syncer{client: client, store: store, adminKey: adminKey}
}

// SyncResult summarizes what a Run produced. Returned by Run for telemetry
// and persisted in intuithub_sync_state.
type SyncResult struct {
	TeamsSynced         int
	CollaboratorsSynced int
	OwnershipsSynced    int
	StartedAt           time.Time
	FinishedAt          time.Time
	Err                 error // nil when Status == "success"
}

// Status returns "success", "failure", or "partial".
func (r SyncResult) Status() string {
	if r.Err != nil {
		return "failure"
	}
	return "success"
}

// Run executes a single full sync: teams, applications (for name lookup),
// teamapplications, collaborators. Persists results to intuithub_sync_state.
func (s *Syncer) Run(ctx context.Context) (res SyncResult) {
	res.StartedAt = time.Now().UTC()
	if s.client == nil || s.store == nil {
		res.Err = errors.New("intuithub: syncer not configured (missing client or store)")
		res.FinishedAt = time.Now().UTC()
		return res
	}
	if strings.TrimSpace(s.adminKey) == "" {
		res.Err = errors.New("intuithub: admin API key not configured (set INTUIT_ENGRAM_INTUITHUB_ADMIN_KEY)")
		res.FinishedAt = time.Now().UTC()
		return res
	}

	// Named return value (res) lets this defer mutate the value the function
	// returns. A deferred closure on a non-named return modifies a stale copy.
	defer func() {
		res.FinishedAt = time.Now().UTC()
		// Best-effort: record run regardless of error so doctor can surface stale state.
		errMsg := ""
		if res.Err != nil {
			errMsg = res.Err.Error()
		}
		_ = s.store.RecordIntuitHubSyncRun(ctx, cloudstore.IntuitHubSyncStateUpdate{
			Status:              res.Status(),
			Error:               errMsg,
			TeamsSynced:         res.TeamsSynced,
			CollaboratorsSynced: res.CollaboratorsSynced,
			OwnershipsSynced:    res.OwnershipsSynced,
		})
	}()

	// ── 1. Teams ─────────────────────────────────────────────────────────
	teams, err := s.client.ListTeams(ctx, s.adminKey)
	if err != nil {
		res.Err = fmt.Errorf("list teams: %w", err)
		return res
	}
	res.TeamsSynced = len(teams)
	teamCodeByID := make(map[int]string, len(teams))
	teamHasFullVis := make(map[string]bool, len(teams))
	for _, t := range teams {
		code := strings.TrimSpace(t.TeamCode)
		if code == "" {
			continue
		}
		teamCodeByID[t.TeamID] = code
		teamHasFullVis[code] = t.HasFullVisibility
	}

	// ── 2. Applications (id → name) ──────────────────────────────────────
	apps, err := s.client.ListApplications(ctx, s.adminKey)
	if err != nil {
		res.Err = fmt.Errorf("list applications: %w", err)
		return res
	}
	appNameByID := make(map[int]string, len(apps))
	for _, a := range apps {
		name := strings.TrimSpace(a.ApplicationName)
		if name == "" {
			continue
		}
		appNameByID[a.ApplicationID] = name
	}

	// ── 3. Team↔Application ownership ────────────────────────────────────
	teamApps, err := s.client.ListTeamApplications(ctx, s.adminKey, 0, 0)
	if err != nil {
		res.Err = fmt.Errorf("list teamapplications: %w", err)
		return res
	}
	ownership := make([]cloudstore.AppTeamOwnership, 0, len(teamApps))
	// appTeamRoles indexes (app_name, team_code) → role for fast lookup when
	// hydrating membership.apps[].role_in_app per collaborator.
	appTeamRoles := make(map[string]map[string]string)
	for _, ta := range teamApps {
		if !ta.IsActive {
			continue
		}
		// Prefer embedded objects; fall back to id lookup.
		teamCode := ""
		if ta.Team != nil {
			teamCode = strings.TrimSpace(ta.Team.TeamCode)
		}
		if teamCode == "" {
			teamCode = teamCodeByID[ta.TeamID]
		}
		appName := ""
		if ta.Application != nil {
			appName = strings.TrimSpace(ta.Application.ApplicationName)
		}
		if appName == "" {
			appName = appNameByID[ta.ApplicationID]
		}
		if teamCode == "" || appName == "" {
			continue
		}
		ownership = append(ownership, cloudstore.AppTeamOwnership{
			ApplicationName: appName,
			TeamCode:        teamCode,
			Role:            strings.TrimSpace(ta.Role),
		})
		if appTeamRoles[appName] == nil {
			appTeamRoles[appName] = map[string]string{}
		}
		appTeamRoles[appName][teamCode] = strings.TrimSpace(ta.Role)
	}
	if err := s.store.ReplaceAppTeamOwnership(ctx, ownership); err != nil {
		res.Err = fmt.Errorf("replace ownership: %w", err)
		return res
	}
	res.OwnershipsSynced = len(ownership)

	// ── 4. Collaborators ─────────────────────────────────────────────────
	collabs, err := s.client.ListCollaborators(ctx, s.adminKey)
	if err != nil {
		res.Err = fmt.Errorf("list collaborators: %w", err)
		return res
	}
	for _, c := range collabs {
		if !c.IsActive {
			continue
		}
		user := buildIntuitHubUser(c, teamHasFullVis, appTeamRoles, "")
		if _, err := s.store.UpsertIntuitHubUser(ctx, user); err != nil {
			res.Err = fmt.Errorf("upsert collaborator %d: %w", c.CollaboratorID, err)
			return res
		}
		res.CollaboratorsSynced++
	}

	return res
}

// HydrateStore is the subset of cloudstore methods needed for lazy
// hydration. The full *cloudstore.CloudStore satisfies it; tests can pass an
// in-memory fake.
type HydrateStore interface {
	UpsertIntuitHubUser(ctx context.Context, u cloudstore.IntuitHubUser) (int64, error)
}

// HydrateFromMe builds an IntuitHubUser from the /me endpoint payload. Used
// by the auth middleware for lazy hydration when a dev shows up before the
// next bulk sync. apiKey is the raw key the caller used; we hash it before
// storing.
func HydrateFromMe(ctx context.Context, store HydrateStore, me *MeResponse, apiKey string, teamHasFullVis map[string]bool, appTeamRoles map[string]map[string]string) (*cloudstore.IntuitHubUser, error) {
	if store == nil {
		return nil, errors.New("intuithub: store not configured")
	}
	if me == nil {
		return nil, errors.New("intuithub: me payload is nil")
	}
	// Convert MeResponse → CollaboratorRow shape so we can reuse buildIntuitHubUser.
	row := CollaboratorRow{
		CollaboratorID: me.CollaboratorID,
		FullName:       me.FullName,
		Email:          me.Email,
		Position:       me.Position,
		Role:           me.Role,
		IsActive:       true,
	}
	for _, t := range me.Teams {
		row.Teams = append(row.Teams, CollaboratorTeam{
			TeamID:   t.TeamID,
			TeamName: t.TeamName,
			TeamCode: t.TeamCode,
			Role:     t.Role,
		})
	}
	hash := hashAPIKey(apiKey)
	user := buildIntuitHubUser(row, teamHasFullVis, appTeamRoles, hash)

	// Also build apps from the embedded /me payload — when teamHasFullVis /
	// appTeamRoles are empty (cold start), /me itself tells us which apps
	// each team owns. We still won't know the per-app role here.
	if len(appTeamRoles) == 0 {
		appsByTeam := map[string][]string{}
		for _, t := range me.Teams {
			for _, a := range t.Applications {
				name := strings.TrimSpace(a.ApplicationName)
				code := strings.TrimSpace(t.TeamCode)
				if name == "" || code == "" {
					continue
				}
				appsByTeam[code] = append(appsByTeam[code], name)
			}
		}
		for i := range user.Memberships {
			code := user.Memberships[i].TeamCode
			if names := appsByTeam[code]; len(names) > 0 {
				apps := make([]cloudstore.IntuitHubMembershipApp, 0, len(names))
				for _, n := range names {
					apps = append(apps, cloudstore.IntuitHubMembershipApp{ApplicationName: n})
				}
				user.Memberships[i].Apps = apps
			}
		}
	}

	if _, err := store.UpsertIntuitHubUser(ctx, user); err != nil {
		return nil, fmt.Errorf("hydrate from /me: %w", err)
	}
	return &user, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────

func buildIntuitHubUser(c CollaboratorRow, teamHasFullVis map[string]bool, appTeamRoles map[string]map[string]string, apiKeyHash string) cloudstore.IntuitHubUser {
	teamCodes := make([]string, 0, len(c.Teams))
	memberships := make([]cloudstore.IntuitHubMembership, 0, len(c.Teams))
	hasFullVis := false
	for _, t := range c.Teams {
		code := strings.TrimSpace(t.TeamCode)
		if code == "" {
			continue
		}
		teamCodes = append(teamCodes, code)
		if teamHasFullVis[code] {
			hasFullVis = true
		}
		// For each team, list the apps it owns (from appTeamRoles) along with
		// the team's role over each app. This is what the visibility filter
		// will consult to decide promote permissions.
		var apps []cloudstore.IntuitHubMembershipApp
		for appName, byTeam := range appTeamRoles {
			role, ok := byTeam[code]
			if !ok {
				continue
			}
			apps = append(apps, cloudstore.IntuitHubMembershipApp{
				ApplicationName: appName,
				RoleInApp:       role,
			})
		}
		sort.Slice(apps, func(i, j int) bool { return apps[i].ApplicationName < apps[j].ApplicationName })
		memberships = append(memberships, cloudstore.IntuitHubMembership{
			TeamCode:   code,
			RoleInTeam: strings.TrimSpace(t.Role),
			Apps:       apps,
		})
	}
	sort.Strings(teamCodes)
	sort.Slice(memberships, func(i, j int) bool { return memberships[i].TeamCode < memberships[j].TeamCode })

	return cloudstore.IntuitHubUser{
		IntuitCollaboratorID: c.CollaboratorID,
		Email:                strings.TrimSpace(c.Email),
		FullName:             strings.TrimSpace(c.FullName),
		Role:                 strings.TrimSpace(c.Role),
		TeamCodes:            teamCodes,
		Memberships:          memberships,
		HasFullVisibility:    hasFullVis,
		APIKeyHash:           apiKeyHash,
	}
}

// hashAPIKey returns the hex sha256 of the raw key. Empty input → empty
// output (so the upsert leaves api_key_hash unchanged).
func hashAPIKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
