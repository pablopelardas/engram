// Package intuithub provides a thin HTTP client and types for talking to the
// IntuitHub-API. IntuitHub is the source of truth for organizational identity
// (Collaborators, Teams, Applications, TeamApplications). Engram caches a
// subset of this data to evaluate visibility filters without round-trips.
package intuithub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the API rejects the supplied X-API-Key.
var ErrUnauthorized = errors.New("intuithub: unauthorized (invalid or missing API key)")

// flexibleTime parses .NET's loose DateTime serialization. The IntuitHub API
// returns timestamps either as RFC3339 with timezone ("2026-04-20T15:28:03Z")
// or as a naive local-time string with no timezone ("2026-04-20T15:28:03.7533333").
// We accept both and assume UTC when the timezone is missing.
type flexibleTime struct {
	time.Time
}

func (t *flexibleTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		return nil
	}
	// Try the common formats in order. Go's time.Parse is strict about which
	// fractional-second precision and timezone shape it accepts, so we list
	// every format the API has been observed to emit.
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.9999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05.999",
		"2006-01-02T15:04:05",
	}
	var firstErr error
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, s)
		if err == nil {
			t.Time = parsed.UTC()
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return fmt.Errorf("intuithub: cannot parse time %q: %w", s, firstErr)
}

// ErrForbidden is returned when the caller is authenticated but lacks the
// required role for an admin endpoint.
var ErrForbidden = errors.New("intuithub: forbidden (insufficient role)")

// ErrNotFound is returned for 404 responses.
var ErrNotFound = errors.New("intuithub: not found")

// Client talks to IntuitHub-API. Construct with New.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a client pointed at baseURL (e.g. "https://intuithub-api.intuit.local").
// httpClient is optional; nil means use a default with a 10-second timeout.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// ─── Response shapes (subset of IntuitHub-API responses) ───────────────────

// MeResponse mirrors GET /api/auth/me.
type MeResponse struct {
	CollaboratorID int           `json:"collaboratorId"`
	FullName       string        `json:"fullName"`
	Email          string        `json:"email"`
	Position       string        `json:"position"`
	Role           string        `json:"role"`
	Teams          []TeamWithApps `json:"teams"`
}

// TeamWithApps is the per-team payload embedded in /me. The dev's role within
// the team comes from CollaboratorTeams.Role; the team's role over each app
// comes from TeamApplications.Role.
type TeamWithApps struct {
	TeamID           int           `json:"teamId"`
	TeamName         string        `json:"teamName"`
	TeamCode         string        `json:"teamCode"`
	ReleaseTagSuffix string        `json:"releaseTagSuffix"`
	Role             string        `json:"role"` // dev's role within the team
	Applications     []AppOfTeam   `json:"applications"`
}

// AppOfTeam represents an application that a team owns. Note the IntuitHub /me
// endpoint does NOT include the per-app role (TeamApplications.Role) — that
// detail is recovered via ListTeamApplications.
type AppOfTeam struct {
	ApplicationID   int    `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	DisplayName     string `json:"displayName"`
	ApplicationType string `json:"applicationType"`
	RepositoryURL   string `json:"repositoryUrl"`
	TagPrefix       string `json:"tagPrefix"`
	TechnicalStack  string `json:"technicalStack"`
}

// CollaboratorRow mirrors a row from GET /api/collaborators (admin view).
type CollaboratorRow struct {
	CollaboratorID int                `json:"collaboratorId"`
	FullName       string             `json:"fullName"`
	Email          string             `json:"email"`
	APIKey         string             `json:"apiKey,omitempty"` // present only for Admin caller
	Position       string             `json:"position"`
	Role           string             `json:"role"`
	IsActive       bool               `json:"isActive"`
	LastLoginAt    *flexibleTime      `json:"lastLoginAt"`
	CreatedAt      flexibleTime       `json:"createdAt"`
	Teams          []CollaboratorTeam `json:"teams"`
}

// CollaboratorTeam is the per-team membership entry inside CollaboratorRow.Teams.
type CollaboratorTeam struct {
	TeamID   int    `json:"teamId"`
	TeamName string `json:"teamName"`
	TeamCode string `json:"teamCode"`
	Role     string `json:"role"` // dev's role within the team
}

// TeamRow mirrors a row from GET /api/teams.
type TeamRow struct {
	TeamID              int    `json:"teamId"`
	TeamName            string `json:"teamName"`
	TeamCode            string `json:"teamCode"`
	ReleaseTagSuffix    string `json:"releaseTagSuffix"`
	Description         string `json:"description"`
	ContactEmail        string `json:"contactEmail"`
	HasFullVisibility   bool   `json:"hasFullVisibility"`
	IsActive            bool   `json:"isActive"`
}

// ApplicationRow mirrors a row from GET /api/applications.
type ApplicationRow struct {
	ApplicationID   int    `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	DisplayName     string `json:"displayName"`
	ApplicationType string `json:"applicationType"`
	IsActive        bool   `json:"isActive"`
}

// TeamApplicationRow mirrors a row from GET /api/teamapplications. The team
// and application are returned as full nested objects by the API; we only
// capture the keys we need.
type TeamApplicationRow struct {
	TeamApplicationID int             `json:"teamApplicationId"`
	TeamID            int             `json:"teamId"`
	ApplicationID     int             `json:"applicationId"`
	Role              string          `json:"role"`
	IsActive          bool            `json:"isActive"`
	Team              *TeamRow        `json:"team,omitempty"`
	Application       *ApplicationRow `json:"application,omitempty"`
}

// ─── Endpoints ────────────────────────────────────────────────────────────

// Me calls GET /api/auth/me with the dev's own API key.
func (c *Client) Me(ctx context.Context, apiKey string) (*MeResponse, error) {
	var resp MeResponse
	if err := c.do(ctx, http.MethodGet, "/api/auth/me", apiKey, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListCollaborators calls GET /api/collaborators with an admin key. Returns
// every active collaborator with their team memberships.
func (c *Client) ListCollaborators(ctx context.Context, adminKey string) ([]CollaboratorRow, error) {
	var resp []CollaboratorRow
	if err := c.do(ctx, http.MethodGet, "/api/collaborators", adminKey, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListTeams calls GET /api/teams with an admin key.
func (c *Client) ListTeams(ctx context.Context, adminKey string) ([]TeamRow, error) {
	var resp []TeamRow
	if err := c.do(ctx, http.MethodGet, "/api/teams", adminKey, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListApplications calls GET /api/applications with an admin key.
func (c *Client) ListApplications(ctx context.Context, adminKey string) ([]ApplicationRow, error) {
	var resp []ApplicationRow
	if err := c.do(ctx, http.MethodGet, "/api/applications", adminKey, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListTeamApplications calls GET /api/teamapplications. Optional teamID and
// applicationID filters mirror the upstream query params.
func (c *Client) ListTeamApplications(ctx context.Context, adminKey string, teamID, applicationID int) ([]TeamApplicationRow, error) {
	q := url.Values{}
	if teamID > 0 {
		q.Set("teamId", fmt.Sprintf("%d", teamID))
	}
	if applicationID > 0 {
		q.Set("applicationId", fmt.Sprintf("%d", applicationID))
	}
	path := "/api/teamapplications"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp []TeamApplicationRow
	if err := c.do(ctx, http.MethodGet, path, adminKey, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ─── HTTP plumbing ────────────────────────────────────────────────────────

func (c *Client) do(ctx context.Context, method, path, apiKey string, body io.Reader, out any) error {
	if c.baseURL == "" {
		return errors.New("intuithub: base URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("intuithub: build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("intuithub: request failed: %w", err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// fall through to body decode
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	default:
		bodyBytes, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("intuithub: unexpected status %d: %s", res.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("intuithub: decode response: %w", err)
	}
	return nil
}
