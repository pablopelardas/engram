package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gentleman-Programming/engram/internal/cloud/cloudstore"
	"github.com/Gentleman-Programming/engram/internal/cloud/dashboard"
	"github.com/Gentleman-Programming/engram/internal/server"
	"github.com/Gentleman-Programming/engram/internal/store"
)

// localDashboardStore adapts a local store.Store to dashboard.DashboardStore.
// This lets the dashboard work with SQLite (regular serve) without PostgreSQL.
type localDashboardStore struct {
	s *store.Store
}

func newLocalDashboardStore(s *store.Store) *localDashboardStore {
	return &localDashboardStore{s: s}
}

func (lds *localDashboardStore) toObservationRows(observations []store.Observation) []cloudstore.DashboardObservationRow {
	rows := make([]cloudstore.DashboardObservationRow, 0, len(observations))
	for _, o := range observations {
		project := ""
		if o.Project != nil {
			project = *o.Project
		}
		rows = append(rows, cloudstore.DashboardObservationRow{
			Project:         project,
			SessionID:       o.SessionID,
			SyncID:          o.SyncID,
			Type:            o.Type,
			Title:           o.Title,
			Content:         o.Content,
			CreatedAt:       o.CreatedAt,
			CanonicalStatus: o.CanonicalStatus,
		})
	}
	return rows
}

func (lds *localDashboardStore) ListProjects(query string) ([]cloudstore.DashboardProjectRow, error) {
	names, err := lds.s.ListProjectNames()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	rows := make([]cloudstore.DashboardProjectRow, 0)
	for _, name := range names {
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		count, _ := lds.s.CountObservationsForProject(name)
		rows = append(rows, cloudstore.DashboardProjectRow{
			Project:      name,
			Observations: count,
		})
	}
	return rows, nil
}

func (lds *localDashboardStore) ProjectDetail(project string) (cloudstore.DashboardProjectDetail, error) {
	return cloudstore.DashboardProjectDetail{Project: project}, nil
}

func (lds *localDashboardStore) ListContributors(query string) ([]cloudstore.DashboardContributorRow, error) {
	return []cloudstore.DashboardContributorRow{}, nil
}

func (lds *localDashboardStore) ListRecentSessions(project, query string, limit int) ([]cloudstore.DashboardSessionRow, error) {
	return []cloudstore.DashboardSessionRow{}, nil
}

func (lds *localDashboardStore) ListRecentObservations(project, query string, limit int) ([]cloudstore.DashboardObservationRow, error) {
	obs, err := lds.s.RecentObservations("", "", limit)
	if err != nil {
		return nil, err
	}
	rows := lds.toObservationRows(obs)
	// Filter by project
	if project != "" {
		filtered := make([]cloudstore.DashboardObservationRow, 0)
		for _, r := range rows {
			if strings.EqualFold(r.Project, project) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	// Filter by query
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" {
		filtered := make([]cloudstore.DashboardObservationRow, 0)
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.Title+" "+r.Content), query) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	return rows, nil
}

func (lds *localDashboardStore) ListRecentPrompts(project, query string, limit int) ([]cloudstore.DashboardPromptRow, error) {
	return []cloudstore.DashboardPromptRow{}, nil
}

func (lds *localDashboardStore) AdminOverview() (cloudstore.DashboardAdminOverview, error) {
	stats, err := lds.s.Stats()
	if err != nil {
		return cloudstore.DashboardAdminOverview{}, err
	}
	return cloudstore.DashboardAdminOverview{
		Projects:     stats.TotalObservations,
		Contributors: stats.TotalSessions,
		Chunks:       stats.TotalPrompts,
	}, nil
}

func (lds *localDashboardStore) ListProjectsPaginated(query string, limit, offset int) ([]cloudstore.DashboardProjectRow, int, error) {
	rows, err := lds.ListProjects(query)
	if err != nil {
		return nil, 0, err
	}
	total := len(rows)
	if offset > total {
		return []cloudstore.DashboardProjectRow{}, total, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return rows[offset:end], total, nil
}

func (lds *localDashboardStore) ListRecentObservationsPaginated(project, query, obsType string, limit, offset int) ([]cloudstore.DashboardObservationRow, int, error) {
	rows, err := lds.ListRecentObservations(project, query, 0)
	if err != nil {
		return nil, 0, err
	}
	// Apply type filter
	if obsType != "" {
		filtered := make([]cloudstore.DashboardObservationRow, 0)
		for _, r := range rows {
			if strings.EqualFold(r.Type, obsType) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	total := len(rows)
	if offset > total {
		return []cloudstore.DashboardObservationRow{}, total, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return rows[offset:end], total, nil
}

func (lds *localDashboardStore) ListObservationsByStatusPaginated(project, query, canonicalStatus string, limit, offset int) ([]cloudstore.DashboardObservationRow, int, error) {
	rows, err := lds.ListRecentObservations(project, query, 0)
	if err != nil {
		return nil, 0, err
	}
	// Apply canonical_status filter
	if canonicalStatus != "" {
		filtered := make([]cloudstore.DashboardObservationRow, 0)
		for _, r := range rows {
			if strings.EqualFold(r.CanonicalStatus, canonicalStatus) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	total := len(rows)
	if offset > total {
		return []cloudstore.DashboardObservationRow{}, total, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return rows[offset:end], total, nil
}

func (lds *localDashboardStore) ListRecentSessionsPaginated(project, query string, limit, offset int) ([]cloudstore.DashboardSessionRow, int, error) {
	return []cloudstore.DashboardSessionRow{}, 0, nil
}

func (lds *localDashboardStore) ListRecentPromptsPaginated(project, query string, limit, offset int) ([]cloudstore.DashboardPromptRow, int, error) {
	return []cloudstore.DashboardPromptRow{}, 0, nil
}

func (lds *localDashboardStore) ListContributorsPaginated(query string, limit, offset int) ([]cloudstore.DashboardContributorRow, int, error) {
	return []cloudstore.DashboardContributorRow{}, 0, nil
}

func (lds *localDashboardStore) GetSessionDetail(project, sessionID string) (cloudstore.DashboardSessionRow, []cloudstore.DashboardObservationRow, []cloudstore.DashboardPromptRow, error) {
	return cloudstore.DashboardSessionRow{}, nil, nil, fmt.Errorf("not implemented")
}

func (lds *localDashboardStore) GetObservationDetail(project, sessionID, syncID string) (cloudstore.DashboardObservationRow, cloudstore.DashboardSessionRow, []cloudstore.DashboardObservationRow, error) {
	// Find observation by syncID
	obs, err := lds.s.GetObservationBySyncID(syncID)
	if err != nil {
		return cloudstore.DashboardObservationRow{}, cloudstore.DashboardSessionRow{}, nil, err
	}

	obsProject := ""
	if obs.Project != nil {
		obsProject = *obs.Project
	}

	row := cloudstore.DashboardObservationRow{
		Project:         obsProject,
		SessionID:       obs.SessionID,
		SyncID:          obs.SyncID,
		Type:            obs.Type,
		Title:           obs.Title,
		Content:         obs.Content,
		CreatedAt:       obs.CreatedAt,
		CanonicalStatus: obs.CanonicalStatus,
	}

	// Get session info
	sess, err := lds.s.GetSession(obs.SessionID)
	if err != nil {
		return row, cloudstore.DashboardSessionRow{}, nil, nil
	}

	sessionRow := cloudstore.DashboardSessionRow{
		Project:   obsProject,
		SessionID: sess.ID,
		StartedAt: sess.StartedAt,
	}

	// Get related observations from same session
	related, err := lds.s.SessionObservations(obs.SessionID, 50)
	if err != nil {
		return row, sessionRow, nil, nil
	}

	relatedRows := make([]cloudstore.DashboardObservationRow, 0, len(related))
	for _, o := range related {
		if o.SyncID == syncID {
			continue // skip self
		}
		proj := ""
		if o.Project != nil {
			proj = *o.Project
		}
		relatedRows = append(relatedRows, cloudstore.DashboardObservationRow{
			Project:         proj,
			SessionID:       o.SessionID,
			SyncID:          o.SyncID,
			Type:            o.Type,
			Title:           o.Title,
			Content:         o.Content,
			CreatedAt:       o.CreatedAt,
			CanonicalStatus: o.CanonicalStatus,
		})
	}

	return row, sessionRow, relatedRows, nil
}

func (lds *localDashboardStore) GetPromptDetail(project, sessionID, syncID string) (cloudstore.DashboardPromptRow, cloudstore.DashboardSessionRow, []cloudstore.DashboardPromptRow, error) {
	return cloudstore.DashboardPromptRow{}, cloudstore.DashboardSessionRow{}, nil, fmt.Errorf("not implemented")
}

func (lds *localDashboardStore) SystemHealth() (cloudstore.DashboardSystemHealth, error) {
	stats, err := lds.s.Stats()
	if err != nil {
		return cloudstore.DashboardSystemHealth{}, err
	}
	return cloudstore.DashboardSystemHealth{
		DBConnected:  true,
		Observations: stats.TotalObservations,
		Sessions:     stats.TotalSessions,
		Prompts:      stats.TotalPrompts,
	}, nil
}

func (lds *localDashboardStore) ListProjectSyncControls() ([]cloudstore.ProjectSyncControl, error) {
	return []cloudstore.ProjectSyncControl{}, nil
}

func (lds *localDashboardStore) GetProjectSyncControl(project string) (*cloudstore.ProjectSyncControl, error) {
	return nil, fmt.Errorf("not implemented")
}

func (lds *localDashboardStore) SetProjectSyncEnabled(project string, enabled bool, updatedBy, reason string) error {
	return fmt.Errorf("not implemented")
}

func (lds *localDashboardStore) IsProjectSyncEnabled(project string) (bool, error) {
	return false, nil
}

func (lds *localDashboardStore) GetContributorDetail(name string) (cloudstore.DashboardContributorRow, []cloudstore.DashboardSessionRow, []cloudstore.DashboardObservationRow, []cloudstore.DashboardPromptRow, error) {
	return cloudstore.DashboardContributorRow{}, nil, nil, nil, fmt.Errorf("not implemented")
}

func (lds *localDashboardStore) ListDistinctTypes() ([]string, error) {
	return store.AllowedTypes, nil
}

func (lds *localDashboardStore) ListAuditEntriesPaginated(ctx context.Context, filter cloudstore.AuditFilter, limit, offset int) ([]cloudstore.DashboardAuditRow, int, error) {
	return []cloudstore.DashboardAuditRow{}, 0, nil
}

// serverSyncStatusAdapter adapts the local sync status to dashboard.SyncStatusProvider.
type serverSyncStatusAdapter struct {
	fallback server.SyncStatusProvider
}

func (a serverSyncStatusAdapter) Status() dashboard.SyncStatus {
	status := a.fallback.Status("")
	return dashboard.SyncStatus{
		Phase:         status.Phase,
		ReasonCode:    status.ReasonCode,
		ReasonMessage: status.ReasonMessage,
	}
}

var _ interface {
	ListProjects(query string) ([]cloudstore.DashboardProjectRow, error)
	ProjectDetail(project string) (cloudstore.DashboardProjectDetail, error)
	ListContributors(query string) ([]cloudstore.DashboardContributorRow, error)
	ListRecentSessions(project string, query string, limit int) ([]cloudstore.DashboardSessionRow, error)
	ListRecentObservations(project string, query string, limit int) ([]cloudstore.DashboardObservationRow, error)
	ListRecentPrompts(project string, query string, limit int) ([]cloudstore.DashboardPromptRow, error)
	AdminOverview() (cloudstore.DashboardAdminOverview, error)
	ListProjectsPaginated(query string, limit, offset int) ([]cloudstore.DashboardProjectRow, int, error)
	ListRecentObservationsPaginated(project, query, obsType string, limit, offset int) ([]cloudstore.DashboardObservationRow, int, error)
	ListObservationsByStatusPaginated(project, query, canonicalStatus string, limit, offset int) ([]cloudstore.DashboardObservationRow, int, error)
	ListRecentSessionsPaginated(project, query string, limit, offset int) ([]cloudstore.DashboardSessionRow, int, error)
	ListRecentPromptsPaginated(project, query string, limit, offset int) ([]cloudstore.DashboardPromptRow, int, error)
	ListContributorsPaginated(query string, limit, offset int) ([]cloudstore.DashboardContributorRow, int, error)
	GetSessionDetail(project, sessionID string) (cloudstore.DashboardSessionRow, []cloudstore.DashboardObservationRow, []cloudstore.DashboardPromptRow, error)
	GetObservationDetail(project, sessionID, syncID string) (cloudstore.DashboardObservationRow, cloudstore.DashboardSessionRow, []cloudstore.DashboardObservationRow, error)
	GetPromptDetail(project, sessionID, syncID string) (cloudstore.DashboardPromptRow, cloudstore.DashboardSessionRow, []cloudstore.DashboardPromptRow, error)
	SystemHealth() (cloudstore.DashboardSystemHealth, error)
	ListProjectSyncControls() ([]cloudstore.ProjectSyncControl, error)
	GetProjectSyncControl(project string) (*cloudstore.ProjectSyncControl, error)
	SetProjectSyncEnabled(project string, enabled bool, updatedBy, reason string) error
	IsProjectSyncEnabled(project string) (bool, error)
	GetContributorDetail(name string) (cloudstore.DashboardContributorRow, []cloudstore.DashboardSessionRow, []cloudstore.DashboardObservationRow, []cloudstore.DashboardPromptRow, error)
	ListDistinctTypes() ([]string, error)
	ListAuditEntriesPaginated(ctx context.Context, filter cloudstore.AuditFilter, limit, offset int) ([]cloudstore.DashboardAuditRow, int, error)
} = (*localDashboardStore)(nil)
