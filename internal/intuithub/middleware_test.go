package intuithub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gentleman-Programming/engram/internal/cloud/cloudstore"
)

// memStore is an in-memory implementation of AuthStore + HydrateStore for
// unit tests. It mirrors the upsert semantics of the real cloudstore.
type memStore struct {
	mu       sync.Mutex
	byHash   map[string]*cloudstore.IntuitHubUser
	byID     map[int]*cloudstore.IntuitHubUser
	upserts  int32
	clockNow func() time.Time
}

func newMemStore(now func() time.Time) *memStore {
	if now == nil {
		now = time.Now
	}
	return &memStore{
		byHash:   map[string]*cloudstore.IntuitHubUser{},
		byID:     map[int]*cloudstore.IntuitHubUser{},
		clockNow: now,
	}
}

func (m *memStore) GetIntuitHubUserByAPIKeyHash(_ context.Context, hash string) (*cloudstore.IntuitHubUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.byHash[hash]
	if u == nil {
		return nil, nil
	}
	c := *u
	return &c, nil
}

func (m *memStore) GetIntuitHubUserByCollaboratorID(_ context.Context, id int) (*cloudstore.IntuitHubUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.byID[id]
	if u == nil {
		return nil, nil
	}
	c := *u
	return &c, nil
}

func (m *memStore) UpsertIntuitHubUser(_ context.Context, u cloudstore.IntuitHubUser) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	atomic.AddInt32(&m.upserts, 1)
	u.CachedAt = m.clockNow()
	stored := u
	m.byID[u.IntuitCollaboratorID] = &stored
	if strings.TrimSpace(u.APIKeyHash) != "" {
		m.byHash[u.APIKeyHash] = &stored
	}
	return int64(u.IntuitCollaboratorID), nil
}

// ─── tests ────────────────────────────────────────────────────────────────

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	mw := buildMW(t, func(*http.Request) (int, string) {
		t.Fatal("upstream should not be called when header is missing")
		return 0, ""
	}, newMemStore(nil), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not run")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddleware_LiveValidatesAndHydrates(t *testing.T) {
	store := newMemStore(nil)
	var meCalls int32

	mw := buildMW(t, func(r *http.Request) (int, string) {
		atomic.AddInt32(&meCalls, 1)
		if r.Header.Get("X-API-Key") != "valid" {
			return http.StatusUnauthorized, ""
		}
		return http.StatusOK, `{"collaboratorId":7,"fullName":"Pablo","email":"p@x","role":"Editor","teams":[{"teamId":1,"teamName":"Back","teamCode":"BACK","role":"Lead","applications":[{"applicationId":10,"applicationName":"engram","applicationType":"API"}]}]}`
	}, store, nil)

	var seen *cloudstore.IntuitHubUser
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "valid")
	mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if seen == nil || seen.Email != "p@x" || seen.IntuitCollaboratorID != 7 {
		t.Fatalf("expected user p@x in context, got %+v", seen)
	}
	if got := atomic.LoadInt32(&meCalls); got != 1 {
		t.Fatalf("expected 1 /me call, got %d", got)
	}
	// /me only knows team→app names, not per-app role. Apps for the membership
	// should still be populated via the fallback path in HydrateFromMe.
	if len(seen.Memberships) != 1 || len(seen.Memberships[0].Apps) != 1 {
		t.Fatalf("expected membership with 1 app, got %+v", seen.Memberships)
	}
	if seen.Memberships[0].Apps[0].ApplicationName != "engram" {
		t.Fatalf("expected engram app, got %+v", seen.Memberships[0].Apps[0])
	}
}

func TestAuthMiddleware_MemoryCacheAvoidsUpstream(t *testing.T) {
	store := newMemStore(nil)
	var meCalls int32

	mw := buildMW(t, func(r *http.Request) (int, string) {
		atomic.AddInt32(&meCalls, 1)
		return http.StatusOK, `{"collaboratorId":7,"email":"p@x","role":"Editor","teams":[]}`
	}, store, nil)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("X-API-Key", "k")
		mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d: expected 200, got %d", i, rec.Code)
		}
	}
	if got := atomic.LoadInt32(&meCalls); got != 1 {
		t.Fatalf("expected 1 upstream call (cache hit afterwards), got %d", got)
	}
}

func TestAuthMiddleware_DBCacheServesWithoutUpstream(t *testing.T) {
	store := newMemStore(nil)
	hash := hashAPIKey("cached-key")
	cached := &cloudstore.IntuitHubUser{
		IntuitCollaboratorID: 5,
		Email:                "cached@x",
		APIKeyHash:           hash,
		CachedAt:             time.Now(),
	}
	store.byHash[hash] = cached
	store.byID[5] = cached

	mw := buildMW(t, func(*http.Request) (int, string) {
		t.Fatal("upstream should not be called when DB cache is fresh")
		return 0, ""
	}, store, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "cached-key")
	mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if u == nil || u.Email != "cached@x" {
			t.Fatalf("expected cached@x, got %+v", u)
		}
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_DBCacheStaleFallsThroughToUpstream(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	store := newMemStore(clock)

	hash := hashAPIKey("stale-key")
	store.byHash[hash] = &cloudstore.IntuitHubUser{
		IntuitCollaboratorID: 5,
		Email:                "stale@x",
		APIKeyHash:           hash,
		CachedAt:             now.Add(-2 * time.Hour), // older than dbTTL
	}
	store.byID[5] = store.byHash[hash]

	var meCalls int32
	mw := buildMW(t, func(r *http.Request) (int, string) {
		atomic.AddInt32(&meCalls, 1)
		return http.StatusOK, `{"collaboratorId":5,"email":"refreshed@x","role":"Editor","teams":[]}`
	}, store, clock)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "stale-key")
	mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if u == nil || u.Email != "refreshed@x" {
			t.Fatalf("expected refreshed@x after stale cache miss, got %+v", u)
		}
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := atomic.LoadInt32(&meCalls); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}

func TestAuthMiddleware_InvalidKey401(t *testing.T) {
	mw := buildMW(t, func(*http.Request) (int, string) {
		return http.StatusUnauthorized, ""
	}, newMemStore(nil), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "bad")
	mw.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next should not run")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_BlockedRole403(t *testing.T) {
	mw := buildMW(t, func(*http.Request) (int, string) {
		return http.StatusForbidden, ""
	}, newMemStore(nil), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", "blocked")
	mw.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next should not run")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func buildMW(t *testing.T, responder func(*http.Request) (int, string), store *memStore, now func() time.Time) *AuthMiddleware {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, body := responder(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	mw, err := NewAuthMiddleware(New(srv.URL, nil), store, AuthMiddlewareConfig{
		MemTTL: 5 * time.Minute,
		DBTTL:  time.Hour,
		Now:    now,
	})
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	return mw
}
