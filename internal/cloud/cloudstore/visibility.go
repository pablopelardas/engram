package cloudstore

import (
	"context"
	"fmt"
	"strings"
)

// VisibleObservation is a row of cloud_observations as seen through a
// visibility filter. It carries enough metadata for the dashboard and
// cross-project search; full content fetching can be done on demand.
type VisibleObservation struct {
	SyncID            string
	Project           string
	SessionID         string
	Type              string
	Title             string
	Content           string
	Scope             string
	TopicKey          string
	CanonicalStatus   string
	CreatedBy         string
	CreatedByCollabID int
	CreatedAt         string
	UpdatedAt         string
}

// VisibilityFilter constrains which observations a caller may see based on
// their IntuitHub identity. Construct one per request.
type VisibilityFilter struct {
	// CollaboratorID is the IntuitHub collaborator id of the caller.
	// Required (filter is meaningless without identity).
	CollaboratorID int
	// TeamCodes the caller belongs to. Empty means "no team membership".
	TeamCodes []string
	// HasFullVisibility short-circuits team checks: caller sees all reviewed.
	// Typically derived from membership in a team flagged HasFullVisibility=true
	// (e.g. GERENCIAL, DevOps).
	HasFullVisibility bool
}

// SearchOptions adds query/project filters on top of the visibility rules.
type SearchOptions struct {
	// Query matches against title and content (ILIKE). Empty = no text filter.
	Query string
	// Project, when non-empty, restricts results to that project.
	// Empty means cross-project (subject to visibility).
	Project string
	// Type filters by observation type (decision, bugfix, etc.). Empty = any.
	Type string
	// Status filter on canonical_status. Empty means "any visible status".
	// When set explicitly, it's intersected with the visibility rules so the
	// caller can request e.g. "only canonical" without bypassing safety.
	Status string
	// Limit and Offset for pagination. Limit defaults to 50, capped at 200.
	Limit  int
	Offset int
}

// SearchVisible returns observations the caller can see, applying the
// visibility rules + optional filters in opts. Soft-deleted rows are excluded.
//
// Visibility rules (matching the design doc, "Reglas de visibilidad"):
//
//	1. scope='personal'      → only the author
//	2. canonical_status='draft'    → only the author
//	3. canonical_status='reviewed' → caller has full visibility, OR caller's
//	                                 team owns the project
//	4. canonical_status='canonical'   → everyone
//	5. canonical_status='deprecated'  → everyone (UI may flag visually)
func (cs *CloudStore) SearchVisible(ctx context.Context, vf VisibilityFilter, opts SearchOptions) ([]VisibleObservation, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	if vf.CollaboratorID <= 0 {
		return nil, fmt.Errorf("cloudstore: VisibilityFilter requires CollaboratorID")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	// Build the visibility predicate as a parameterized fragment we can mix
	// with optional filters. We always include canonical+deprecated, the rest
	// is conditional on caller identity.
	var conds []string
	args := []any{}
	idx := 1

	// drafts → only author. scope='personal' → only author (any status).
	conds = append(conds, fmt.Sprintf("(canonical_status = 'draft' AND created_by_collab_id = $%d)", idx))
	args = append(args, vf.CollaboratorID)
	idx++
	conds = append(conds, fmt.Sprintf("(scope = 'personal' AND created_by_collab_id = $%d)", idx))
	args = append(args, vf.CollaboratorID)
	idx++

	// reviewed → has full visibility OR project ∈ teams' apps.
	if vf.HasFullVisibility {
		conds = append(conds, "canonical_status = 'reviewed'")
	} else if len(vf.TeamCodes) > 0 {
		conds = append(conds, fmt.Sprintf(`(canonical_status = 'reviewed' AND project IN (
			SELECT application_name FROM app_team_ownership WHERE team_code = ANY($%d)
		))`, idx))
		args = append(args, vf.TeamCodes)
		idx++
	}
	// reviewed visibility is denied if not full and no teams. canonical/deprecated below.

	// canonical + deprecated visible to everyone.
	conds = append(conds, "canonical_status IN ('canonical', 'deprecated')")

	visibility := "(" + strings.Join(conds, " OR ") + ")"

	q := fmt.Sprintf(`
		SELECT sync_id, project, COALESCE(session_id, ''), COALESCE(type, ''),
		       COALESCE(title, ''), COALESCE(content, ''), COALESCE(scope, ''),
		       COALESCE(topic_key, ''), canonical_status,
		       COALESCE(created_by, ''), COALESCE(created_by_collab_id, 0),
		       COALESCE(created_at, ''), COALESCE(updated_at, '')
		FROM cloud_observations
		WHERE deleted_at IS NULL AND %s`, visibility)

	if p := strings.TrimSpace(opts.Project); p != "" {
		q += fmt.Sprintf(" AND project = $%d", idx)
		args = append(args, p)
		idx++
	}
	if t := strings.TrimSpace(opts.Type); t != "" {
		q += fmt.Sprintf(" AND type = $%d", idx)
		args = append(args, t)
		idx++
	}
	if st := strings.TrimSpace(opts.Status); st != "" {
		q += fmt.Sprintf(" AND canonical_status = $%d", idx)
		args = append(args, st)
		idx++
	}
	if qry := strings.TrimSpace(opts.Query); qry != "" {
		q += fmt.Sprintf(" AND (title ILIKE $%d OR content ILIKE $%d)", idx, idx+1)
		like := "%" + qry + "%"
		args = append(args, like, like)
		idx += 2
	}

	q += fmt.Sprintf(" ORDER BY updated_at DESC NULLS LAST, sync_id LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := cs.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cloudstore: search visible: %w", err)
	}
	defer rows.Close()

	var out []VisibleObservation
	for rows.Next() {
		var o VisibleObservation
		if err := rows.Scan(
			&o.SyncID, &o.Project, &o.SessionID, &o.Type,
			&o.Title, &o.Content, &o.Scope,
			&o.TopicKey, &o.CanonicalStatus,
			&o.CreatedBy, &o.CreatedByCollabID,
			&o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("cloudstore: scan visible obs: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// VisibleSyncIDs is the lightweight version of SearchVisible used by the
// pull filter: returns only the sync_ids the caller can see for a given
// project. Used to mask observations from /sync/mutations/pull responses.
func (cs *CloudStore) VisibleSyncIDs(ctx context.Context, vf VisibilityFilter, project string, syncIDs []string) (map[string]struct{}, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	if vf.CollaboratorID <= 0 {
		return nil, fmt.Errorf("cloudstore: VisibilityFilter requires CollaboratorID")
	}
	if len(syncIDs) == 0 {
		return map[string]struct{}{}, nil
	}

	// Reuse the visibility predicate from SearchVisible.
	var conds []string
	args := []any{syncIDs}
	idx := 2

	conds = append(conds, fmt.Sprintf("(canonical_status = 'draft' AND created_by_collab_id = $%d)", idx))
	args = append(args, vf.CollaboratorID)
	idx++
	conds = append(conds, fmt.Sprintf("(scope = 'personal' AND created_by_collab_id = $%d)", idx))
	args = append(args, vf.CollaboratorID)
	idx++

	if vf.HasFullVisibility {
		conds = append(conds, "canonical_status = 'reviewed'")
	} else if len(vf.TeamCodes) > 0 {
		conds = append(conds, fmt.Sprintf(`(canonical_status = 'reviewed' AND project IN (
			SELECT application_name FROM app_team_ownership WHERE team_code = ANY($%d)
		))`, idx))
		args = append(args, vf.TeamCodes)
		idx++
	}

	conds = append(conds, "canonical_status IN ('canonical', 'deprecated')")
	visibility := "(" + strings.Join(conds, " OR ") + ")"

	q := fmt.Sprintf(`
		SELECT sync_id
		FROM cloud_observations
		WHERE sync_id = ANY($1) AND deleted_at IS NULL AND %s`, visibility)
	if p := strings.TrimSpace(project); p != "" {
		q += fmt.Sprintf(" AND project = $%d", idx)
		args = append(args, p)
	}

	rows, err := cs.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("cloudstore: visible sync ids: %w", err)
	}
	defer rows.Close()

	out := make(map[string]struct{}, len(syncIDs))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}
