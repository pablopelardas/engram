package cloudstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ─── cloud_observations materialization ────────────────────────────────────

// observationPayloadFields is the subset of syncObservationPayload we need
// to materialize cloud_observations. We re-decode here instead of importing
// internal/store to avoid a dependency cycle.
type observationPayloadFields struct {
	SyncID            string  `json:"sync_id"`
	SessionID         string  `json:"session_id"`
	Type              string  `json:"type"`
	Title             string  `json:"title"`
	Content           string  `json:"content"`
	Project           *string `json:"project,omitempty"`
	Scope             string  `json:"scope"`
	TopicKey          *string `json:"topic_key,omitempty"`
	CreatedBy         *string `json:"created_by,omitempty"`
	CreatedByCollabID *int    `json:"created_by_collab_id,omitempty"`
	CanonicalStatus   string  `json:"canonical_status,omitempty"`
	CreatedAt         string  `json:"created_at,omitempty"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
	Deleted           bool    `json:"deleted,omitempty"`
	DeletedAt         *string `json:"deleted_at,omitempty"`
	HardDelete        bool    `json:"hard_delete,omitempty"`
}

// upsertCloudObservationFromMutationTx applies an observation mutation
// payload onto cloud_observations. It enforces last-writer-wins via
// updated_seq: out-of-order pushes (lower seq than what's already stored)
// are dropped, so replay is idempotent.
//
// project comes from the cloud_mutations row (authoritative); the payload's
// own project field is used as a fallback only.
func upsertCloudObservationFromMutationTx(ctx context.Context, tx *sql.Tx, seq int64, mutationProject, entityKey, op string, payload json.RawMessage) error {
	syncID := strings.TrimSpace(entityKey)
	if syncID == "" {
		return fmt.Errorf("empty entity_key")
	}

	var p observationPayloadFields
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
	}

	project := strings.TrimSpace(mutationProject)
	if project == "" && p.Project != nil {
		project = strings.TrimSpace(*p.Project)
	}
	if project == "" {
		project = "default"
	}

	canonicalStatus := strings.TrimSpace(p.CanonicalStatus)
	if canonicalStatus == "" {
		canonicalStatus = "draft"
	}

	// Hard delete: physically remove the row. Used for "reject draft" flows.
	if p.HardDelete {
		_, err := tx.ExecContext(ctx, `DELETE FROM cloud_observations WHERE sync_id = $1`, syncID)
		return err
	}

	// Soft delete: set deleted_at, keep the row for tombstone semantics.
	if op == "delete" || p.Deleted {
		deletedAt := ""
		if p.DeletedAt != nil {
			deletedAt = strings.TrimSpace(*p.DeletedAt)
		}
		if deletedAt == "" {
			deletedAt = time.Now().UTC().Format(time.RFC3339)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO cloud_observations (sync_id, project, deleted_at, canonical_status, updated_seq)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (sync_id) DO UPDATE SET
				project = EXCLUDED.project,
				deleted_at = EXCLUDED.deleted_at,
				updated_seq = EXCLUDED.updated_seq
			WHERE EXCLUDED.updated_seq > cloud_observations.updated_seq`,
			syncID, project, deletedAt, canonicalStatus, seq)
		return err
	}

	// Upsert: full row. updated_seq guards against out-of-order replays.
	_, err := tx.ExecContext(ctx, `
		INSERT INTO cloud_observations
			(sync_id, project, session_id, type, title, content, scope, topic_key,
			 canonical_status, created_by, created_by_collab_id,
			 created_at, updated_at, deleted_at, updated_seq)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6,
		        NULLIF($7, ''), $8,
		        $9, $10, $11,
		        NULLIF($12, ''), NULLIF($13, ''), NULL, $14)
		ON CONFLICT (sync_id) DO UPDATE SET
			project              = EXCLUDED.project,
			session_id           = EXCLUDED.session_id,
			type                 = EXCLUDED.type,
			title                = EXCLUDED.title,
			content              = EXCLUDED.content,
			scope                = EXCLUDED.scope,
			topic_key            = EXCLUDED.topic_key,
			canonical_status     = EXCLUDED.canonical_status,
			created_by           = EXCLUDED.created_by,
			created_by_collab_id = EXCLUDED.created_by_collab_id,
			created_at           = COALESCE(EXCLUDED.created_at, cloud_observations.created_at),
			updated_at           = EXCLUDED.updated_at,
			deleted_at           = NULL,
			updated_seq          = EXCLUDED.updated_seq
		WHERE EXCLUDED.updated_seq > cloud_observations.updated_seq`,
		syncID,
		project,
		p.SessionID,
		p.Type,
		p.Title,
		p.Content,
		p.Scope,
		nullableStringPtr(p.TopicKey),
		canonicalStatus,
		nullableStringPtr(p.CreatedBy),
		nullableIntPtr(p.CreatedByCollabID),
		p.CreatedAt,
		p.UpdatedAt,
		seq,
	)
	return err
}

// nullableStringPtr returns nil for empty/whitespace-only strings, *s otherwise.
func nullableStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return v
}

// nullableIntPtr returns nil for nil/zero ints, *n otherwise.
func nullableIntPtr(n *int) any {
	if n == nil || *n == 0 {
		return nil
	}
	return *n
}

// ─── Types ────────────────────────────────────────────────────────────────

// IntuitHubUser is the projection of cloud_users joined with the IntuitHub
// columns. Used by the auth middleware and the visibility filters.
type IntuitHubUser struct {
	ID                   int64
	IntuitCollaboratorID int
	Email                string
	FullName             string
	Role                 string   // 'Admin' | 'Editor' | 'Viewer' | 'None'
	TeamCodes            []string // ['BACK','DEVOPS']
	Memberships          []IntuitHubMembership
	HasFullVisibility    bool
	APIKeyHash           string // sha256 hex of the API key, never the raw key
	CachedAt             time.Time
}

// IntuitHubMembership captures a dev's role in a team and the team's roles
// over its applications.
type IntuitHubMembership struct {
	TeamCode   string                  `json:"team_code"`
	RoleInTeam string                  `json:"role_in_team"` // 'Lead' | 'Member' | 'App' | ...
	Apps       []IntuitHubMembershipApp `json:"apps"`
}

// IntuitHubMembershipApp pairs an application with the team's role over it.
// Note: the per-app role is filled in by the sync job (cross-referencing
// TeamApplications), not by /me — /me only knows which apps a team owns.
type IntuitHubMembershipApp struct {
	ApplicationName string `json:"application_name"`
	RoleInApp       string `json:"role_in_app"` // 'Maintainer' | 'Contributor' | ''
}

// IntuitHubSyncStatus is the singleton row from intuithub_sync_state.
type IntuitHubSyncStatus struct {
	LastSyncAt           sql.NullTime
	LastSyncStatus       sql.NullString // 'success' | 'failure' | 'partial'
	LastSyncError        sql.NullString
	TeamsSynced          sql.NullInt32
	CollaboratorsSynced  sql.NullInt32
	OwnershipsSynced     sql.NullInt32
}

// AppTeamOwnership is one row of app_team_ownership.
type AppTeamOwnership struct {
	ApplicationName string
	TeamCode        string
	Role            string
	SyncedAt        time.Time
}

// ─── cloud_users (IntuitHub-extended) operations ──────────────────────────

// UpsertIntuitHubUser creates or updates a cloud_users row from an IntuitHub
// collaborator payload. The caller must pass a stable username (we use email
// as username for IntuitHub-sourced users, since IntuitHub has no separate
// username field).
//
// The api_key_hash is updated when non-empty; pass "" to leave it unchanged.
func (cs *CloudStore) UpsertIntuitHubUser(ctx context.Context, u IntuitHubUser) (int64, error) {
	if cs == nil || cs.db == nil {
		return 0, fmt.Errorf("cloudstore: not initialized")
	}
	if u.IntuitCollaboratorID <= 0 {
		return 0, fmt.Errorf("cloudstore: UpsertIntuitHubUser requires IntuitCollaboratorID > 0")
	}
	if strings.TrimSpace(u.Email) == "" {
		return 0, fmt.Errorf("cloudstore: UpsertIntuitHubUser requires Email")
	}

	teamCodesJSON, err := json.Marshal(u.TeamCodes)
	if err != nil {
		return 0, fmt.Errorf("cloudstore: marshal team_codes: %w", err)
	}
	if u.Memberships == nil {
		u.Memberships = []IntuitHubMembership{}
	}
	membershipsJSON, err := json.Marshal(u.Memberships)
	if err != nil {
		return 0, fmt.Errorf("cloudstore: marshal memberships: %w", err)
	}

	// Username column is UNIQUE NOT NULL in the legacy schema. Use email as
	// username for IntuitHub-sourced rows. If a row already exists with this
	// intuit_collaborator_id we update in place; otherwise insert.
	const q = `
		INSERT INTO cloud_users
		    (username, email, password_hash,
		     intuit_collaborator_id, full_name, role,
		     team_codes_json, memberships_json,
		     has_full_visibility, api_key_hash, cached_at)
		VALUES ($1, $2, '', $3, $4, $5, $6::jsonb, $7::jsonb, $8, NULLIF($9, ''), NOW())
		ON CONFLICT (intuit_collaborator_id) DO UPDATE SET
		    username             = EXCLUDED.username,
		    email                = EXCLUDED.email,
		    full_name            = EXCLUDED.full_name,
		    role                 = EXCLUDED.role,
		    team_codes_json      = EXCLUDED.team_codes_json,
		    memberships_json     = EXCLUDED.memberships_json,
		    has_full_visibility  = EXCLUDED.has_full_visibility,
		    api_key_hash         = COALESCE(EXCLUDED.api_key_hash, cloud_users.api_key_hash),
		    cached_at            = NOW()
		RETURNING id`

	var id int64
	err = cs.db.QueryRowContext(ctx, q,
		strings.TrimSpace(u.Email),
		strings.TrimSpace(u.Email),
		u.IntuitCollaboratorID,
		strings.TrimSpace(u.FullName),
		strings.TrimSpace(u.Role),
		teamCodesJSON,
		membershipsJSON,
		u.HasFullVisibility,
		strings.TrimSpace(u.APIKeyHash),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("cloudstore: upsert intuithub user: %w", err)
	}
	return id, nil
}

// GetIntuitHubUserByAPIKeyHash looks up a cached user by the sha256 hash of
// their API key. Returns nil, nil when no match.
func (cs *CloudStore) GetIntuitHubUserByAPIKeyHash(ctx context.Context, apiKeyHash string) (*IntuitHubUser, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return nil, nil
	}
	return cs.queryIntuitHubUser(ctx,
		`WHERE api_key_hash = $1`, apiKeyHash)
}

// GetIntuitHubUserByCollaboratorID looks up a cached user by IntuitHub
// collaborator_id. Returns nil, nil when no match.
func (cs *CloudStore) GetIntuitHubUserByCollaboratorID(ctx context.Context, collabID int) (*IntuitHubUser, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	if collabID <= 0 {
		return nil, nil
	}
	return cs.queryIntuitHubUser(ctx,
		`WHERE intuit_collaborator_id = $1`, collabID)
}

func (cs *CloudStore) queryIntuitHubUser(ctx context.Context, whereClause string, args ...any) (*IntuitHubUser, error) {
	q := `SELECT id, COALESCE(intuit_collaborator_id, 0), email, COALESCE(full_name, ''),
	             COALESCE(role, ''), team_codes_json::text, memberships_json::text,
	             has_full_visibility, COALESCE(api_key_hash, ''), cached_at
	      FROM cloud_users ` + whereClause + ` LIMIT 1`
	row := cs.db.QueryRowContext(ctx, q, args...)
	var (
		u                IntuitHubUser
		teamCodesJSON    string
		membershipsJSON  string
	)
	err := row.Scan(&u.ID, &u.IntuitCollaboratorID, &u.Email, &u.FullName,
		&u.Role, &teamCodesJSON, &membershipsJSON,
		&u.HasFullVisibility, &u.APIKeyHash, &u.CachedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cloudstore: query intuithub user: %w", err)
	}
	if teamCodesJSON != "" {
		if err := json.Unmarshal([]byte(teamCodesJSON), &u.TeamCodes); err != nil {
			return nil, fmt.Errorf("cloudstore: decode team_codes_json: %w", err)
		}
	}
	if membershipsJSON != "" {
		if err := json.Unmarshal([]byte(membershipsJSON), &u.Memberships); err != nil {
			return nil, fmt.Errorf("cloudstore: decode memberships_json: %w", err)
		}
	}
	return &u, nil
}

// ─── app_team_ownership operations ────────────────────────────────────────

// ReplaceAppTeamOwnership atomically replaces the entire app_team_ownership
// table with the given rows. This is the simplest correct approach for a full
// pull sync: any team→app assignment that is no longer in IntuitHub is
// removed.
func (cs *CloudStore) ReplaceAppTeamOwnership(ctx context.Context, rows []AppTeamOwnership) error {
	if cs == nil || cs.db == nil {
		return fmt.Errorf("cloudstore: not initialized")
	}
	tx, err := cs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cloudstore: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM app_team_ownership`); err != nil {
		return fmt.Errorf("cloudstore: clear app_team_ownership: %w", err)
	}
	if len(rows) > 0 {
		const ins = `INSERT INTO app_team_ownership (application_name, team_code, role, synced_at)
		             VALUES ($1, $2, NULLIF($3, ''), NOW())
		             ON CONFLICT (application_name, team_code) DO UPDATE
		               SET role = EXCLUDED.role, synced_at = NOW()`
		stmt, err := tx.PrepareContext(ctx, ins)
		if err != nil {
			return fmt.Errorf("cloudstore: prepare ownership insert: %w", err)
		}
		defer stmt.Close()
		for _, r := range rows {
			app := strings.TrimSpace(r.ApplicationName)
			team := strings.TrimSpace(r.TeamCode)
			if app == "" || team == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, app, team, strings.TrimSpace(r.Role)); err != nil {
				return fmt.Errorf("cloudstore: insert ownership %s/%s: %w", app, team, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cloudstore: commit ownership: %w", err)
	}
	return nil
}

// TeamCodesForApplication returns the list of team_codes that own the given
// application. Used by the visibility filter.
func (cs *CloudStore) TeamCodesForApplication(ctx context.Context, applicationName string) ([]string, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	applicationName = strings.TrimSpace(applicationName)
	if applicationName == "" {
		return nil, nil
	}
	rows, err := cs.db.QueryContext(ctx,
		`SELECT team_code FROM app_team_ownership WHERE application_name = $1 ORDER BY team_code`,
		applicationName)
	if err != nil {
		return nil, fmt.Errorf("cloudstore: query app teams: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// ─── intuithub_sync_state operations ──────────────────────────────────────

// IntuitHubSyncStateUpdate is the payload for RecordIntuitHubSyncRun.
type IntuitHubSyncStateUpdate struct {
	Status              string // 'success' | 'failure' | 'partial'
	Error               string // empty on success
	TeamsSynced         int
	CollaboratorsSynced int
	OwnershipsSynced    int
}

// RecordIntuitHubSyncRun updates the singleton sync state row.
func (cs *CloudStore) RecordIntuitHubSyncRun(ctx context.Context, upd IntuitHubSyncStateUpdate) error {
	if cs == nil || cs.db == nil {
		return fmt.Errorf("cloudstore: not initialized")
	}
	const q = `
		UPDATE intuithub_sync_state SET
		    last_sync_at         = NOW(),
		    last_sync_status     = $1,
		    last_sync_error      = NULLIF($2, ''),
		    teams_synced         = $3,
		    collaborators_synced = $4,
		    ownerships_synced    = $5
		WHERE id = 1`
	if _, err := cs.db.ExecContext(ctx, q,
		strings.TrimSpace(upd.Status),
		strings.TrimSpace(upd.Error),
		upd.TeamsSynced, upd.CollaboratorsSynced, upd.OwnershipsSynced,
	); err != nil {
		return fmt.Errorf("cloudstore: record sync run: %w", err)
	}
	return nil
}

// GetIntuitHubSyncStatus reads the singleton sync state row.
func (cs *CloudStore) GetIntuitHubSyncStatus(ctx context.Context) (*IntuitHubSyncStatus, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	var s IntuitHubSyncStatus
	err := cs.db.QueryRowContext(ctx,
		`SELECT last_sync_at, last_sync_status, last_sync_error,
		        teams_synced, collaborators_synced, ownerships_synced
		 FROM intuithub_sync_state WHERE id = 1`,
	).Scan(&s.LastSyncAt, &s.LastSyncStatus, &s.LastSyncError,
		&s.TeamsSynced, &s.CollaboratorsSynced, &s.OwnershipsSynced)
	if errors.Is(err, sql.ErrNoRows) {
		return &IntuitHubSyncStatus{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cloudstore: get sync status: %w", err)
	}
	return &s, nil
}
