package intuithub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_Me_HappyPath(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/me" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "key-abc" {
			t.Fatalf("expected X-API-Key=key-abc, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"collaboratorId": 7,
			"fullName": "Pablo Pelardas",
			"email": "pablo@intuit.local",
			"role": "Admin",
			"teams": [
				{"teamId": 1, "teamName": "Backend", "teamCode": "BACK", "role": "Lead",
				 "applications": [{"applicationId": 10, "applicationName": "engram", "applicationType": "API"}]}
			]
		}`))
	})

	c := New(srv.URL, nil)
	me, err := c.Me(context.Background(), "key-abc")
	if err != nil {
		t.Fatalf("Me failed: %v", err)
	}
	if me.CollaboratorID != 7 || me.Email != "pablo@intuit.local" || me.Role != "Admin" {
		t.Fatalf("unexpected payload: %+v", me)
	}
	if len(me.Teams) != 1 || me.Teams[0].TeamCode != "BACK" || me.Teams[0].Role != "Lead" {
		t.Fatalf("unexpected teams: %+v", me.Teams)
	}
	if len(me.Teams[0].Applications) != 1 || me.Teams[0].Applications[0].ApplicationName != "engram" {
		t.Fatalf("unexpected apps: %+v", me.Teams[0].Applications)
	}
}

func TestClient_Me_Unauthorized(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"API key required"}`))
	})

	c := New(srv.URL, nil)
	_, err := c.Me(context.Background(), "")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestClient_Me_Forbidden(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	c := New(srv.URL, nil)
	_, err := c.Me(context.Background(), "blocked-key")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestClient_ListCollaborators(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/collaborators" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "admin-key" {
			t.Fatalf("expected admin key, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"collaboratorId":1,"fullName":"Dev A","email":"a@x","role":"Editor","isActive":true,"createdAt":"2026-01-01T00:00:00Z","teams":[{"teamId":1,"teamName":"Back","teamCode":"BACK","role":"Member"}]},
			{"collaboratorId":2,"fullName":"Dev B","email":"b@x","role":"Viewer","isActive":true,"createdAt":"2026-01-01T00:00:00Z","teams":[]}
		]`))
	})

	c := New(srv.URL, nil)
	rows, err := c.ListCollaborators(context.Background(), "admin-key")
	if err != nil {
		t.Fatalf("ListCollaborators failed: %v", err)
	}
	if len(rows) != 2 || rows[0].Email != "a@x" || rows[1].Email != "b@x" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if len(rows[0].Teams) != 1 || rows[0].Teams[0].TeamCode != "BACK" {
		t.Fatalf("unexpected teams on rows[0]: %+v", rows[0].Teams)
	}
}

func TestClient_ListTeamApplications_QueryParams(t *testing.T) {
	var capturedQuery string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})

	c := New(srv.URL, nil)
	if _, err := c.ListTeamApplications(context.Background(), "k", 5, 0); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if !strings.Contains(capturedQuery, "teamId=5") {
		t.Fatalf("expected teamId=5 in query, got %q", capturedQuery)
	}
	if strings.Contains(capturedQuery, "applicationId=") {
		t.Fatalf("did not expect applicationId in query, got %q", capturedQuery)
	}
}

func TestClient_ListTeamApplications_EmptyFilter(t *testing.T) {
	var capturedQuery string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})

	c := New(srv.URL, nil)
	if _, err := c.ListTeamApplications(context.Background(), "k", 0, 0); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if capturedQuery != "" {
		t.Fatalf("expected empty query when no filters, got %q", capturedQuery)
	}
}

func TestClient_NoBaseURL(t *testing.T) {
	c := New("", nil)
	_, err := c.Me(context.Background(), "k")
	if err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("expected base URL error, got %v", err)
	}
}

func TestFlexibleTime_AcceptsAllFormats(t *testing.T) {
	cases := map[string]string{
		"RFC3339":              `"2026-04-20T15:28:03Z"`,
		"RFC3339-with-offset":  `"2026-04-20T15:28:03+00:00"`,
		"naive-7-digit-frac":   `"2026-04-20T15:28:03.7533333"`,
		"naive-3-digit-frac":   `"2026-04-20T15:28:03.753"`,
		"naive-no-frac":        `"2026-04-20T15:28:03"`,
	}
	want := time.Date(2026, 4, 20, 15, 28, 3, 0, time.UTC)
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var ft flexibleTime
			if err := json.Unmarshal([]byte(raw), &ft); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if !ft.Truncate(time.Second).Equal(want) {
				t.Fatalf("expected ~%s, got %s", want, ft.Time)
			}
		})
	}
}

func TestFlexibleTime_NullAndEmpty(t *testing.T) {
	for _, raw := range []string{`null`, `""`} {
		var ft flexibleTime
		if err := json.Unmarshal([]byte(raw), &ft); err != nil {
			t.Fatalf("expected nil for %q, got %v", raw, err)
		}
		if !ft.IsZero() {
			t.Fatalf("expected zero time, got %s", ft.Time)
		}
	}
}

func TestFlexibleTime_InvalidFormat(t *testing.T) {
	var ft flexibleTime
	if err := json.Unmarshal([]byte(`"not-a-date"`), &ft); err == nil {
		t.Fatal("expected error for invalid date")
	}
}
