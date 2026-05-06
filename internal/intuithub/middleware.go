package intuithub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Gentleman-Programming/engram/internal/cloud/cloudstore"
)

// Header used by both IntuitHub-API and Engram for API key auth.
const APIKeyHeader = "X-API-Key"

// userCtxKey is the unexported key under which the resolved IntuitHubUser is
// stored on the request context.
type userCtxKey struct{}

// FromContext returns the resolved IntuitHubUser from the request context.
// Returns nil when the request was not authenticated.
func FromContext(ctx context.Context) *cloudstore.IntuitHubUser {
	v, _ := ctx.Value(userCtxKey{}).(*cloudstore.IntuitHubUser)
	return v
}

// withUser returns a new context with the IntuitHubUser attached.
func withUser(ctx context.Context, u *cloudstore.IntuitHubUser) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

// AuthStore is the subset of cloudstore methods the middleware needs. Defined
// as an interface so unit tests can supply an in-memory fake.
type AuthStore interface {
	GetIntuitHubUserByAPIKeyHash(ctx context.Context, apiKeyHash string) (*cloudstore.IntuitHubUser, error)
	GetIntuitHubUserByCollaboratorID(ctx context.Context, collabID int) (*cloudstore.IntuitHubUser, error)
	UpsertIntuitHubUser(ctx context.Context, u cloudstore.IntuitHubUser) (int64, error)
}

// AuthMiddleware resolves the X-API-Key header into an IntuitHubUser and
// attaches it to the request context. It uses a two-tier cache:
//
//   - in-memory LRU keyed by API key hash, TTL 5 min
//   - cloud_users table (cached_at column), TTL configurable
//
// If both caches miss, it calls IntuitHub /api/auth/me to validate the key
// and lazy-hydrate cloud_users.
type AuthMiddleware struct {
	client *Client
	store  AuthStore
	memTTL time.Duration
	dbTTL  time.Duration
	now    func() time.Time

	mu  sync.Mutex
	mem map[string]memEntry // key: api_key_hash
}

type memEntry struct {
	user      *cloudstore.IntuitHubUser
	expiresAt time.Time
}

// AuthMiddlewareConfig groups optional knobs for NewAuthMiddleware.
type AuthMiddlewareConfig struct {
	// MemTTL is how long a positive lookup is cached in memory. Default 5min.
	MemTTL time.Duration
	// DBTTL is how stale a cloud_users row may be before we re-fetch /me.
	// Default 1h.
	DBTTL time.Duration
	// Now is the time source (overridable for tests). Default time.Now.
	Now func() time.Time
}

// NewAuthMiddleware constructs an AuthMiddleware. client and store are required.
func NewAuthMiddleware(client *Client, store AuthStore, cfg AuthMiddlewareConfig) (*AuthMiddleware, error) {
	if client == nil {
		return nil, errors.New("intuithub: AuthMiddleware requires a client")
	}
	if store == nil {
		return nil, errors.New("intuithub: AuthMiddleware requires a cloudstore")
	}
	if cfg.MemTTL <= 0 {
		cfg.MemTTL = 5 * time.Minute
	}
	if cfg.DBTTL <= 0 {
		cfg.DBTTL = time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &AuthMiddleware{
		client: client,
		store:  store,
		memTTL: cfg.MemTTL,
		dbTTL:  cfg.DBTTL,
		now:    cfg.Now,
		mem:    make(map[string]memEntry),
	}, nil
}

// Handler wraps next with API-key resolution. Behavior:
//
//   - missing or empty X-API-Key → 401 unauthorized
//   - invalid key (IntuitHub returns 401) → 401 unauthorized
//   - blocked role (IntuitHub returns 403) → 403 forbidden
//   - any other failure → 502 bad gateway with a generic message
//   - success → next.ServeHTTP with user in context
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := strings.TrimSpace(r.Header.Get(APIKeyHeader))
		if apiKey == "" {
			http.Error(w, "missing "+APIKeyHeader+" header", http.StatusUnauthorized)
			return
		}
		user, err := m.Resolve(r.Context(), apiKey)
		if err != nil {
			switch {
			case errors.Is(err, ErrUnauthorized):
				http.Error(w, "invalid API key", http.StatusUnauthorized)
			case errors.Is(err, ErrForbidden):
				http.Error(w, "API key blocked or insufficient role", http.StatusForbidden)
			default:
				http.Error(w, fmt.Sprintf("auth resolve failed: %v", err), http.StatusBadGateway)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// Resolve looks up an API key through the cache hierarchy. Exposed for callers
// that aren't HTTP-bound (e.g. CLI tooling or background jobs).
func (m *AuthMiddleware) Resolve(ctx context.Context, apiKey string) (*cloudstore.IntuitHubUser, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrUnauthorized
	}
	hash := hashAPIKey(apiKey)

	// Tier 1: in-memory LRU.
	if u := m.memGet(hash); u != nil {
		return u, nil
	}

	// Tier 2: cloud_users row.
	dbUser, err := m.store.GetIntuitHubUserByAPIKeyHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("intuithub: db lookup: %w", err)
	}
	if dbUser != nil && m.now().Sub(dbUser.CachedAt) < m.dbTTL {
		m.memPut(hash, dbUser)
		return dbUser, nil
	}

	// Tier 3: live /me call. Validates the key and refreshes the cache.
	me, err := m.client.Me(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	hydrated, err := HydrateFromMe(ctx, m.store, me, apiKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("intuithub: hydrate from /me: %w", err)
	}
	// Re-read so we get the canonical row (with id, cached_at from DB).
	user, err := m.store.GetIntuitHubUserByCollaboratorID(ctx, hydrated.IntuitCollaboratorID)
	if err != nil || user == nil {
		// Fall back to the in-memory hydrated copy if the read-back fails.
		user = hydrated
	}
	m.memPut(hash, user)
	return user, nil
}

// InvalidateCache drops the in-memory cache. Useful for tests or after a
// known cache-invalidating event (e.g. admin regenerated a key).
func (m *AuthMiddleware) InvalidateCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem = make(map[string]memEntry)
}

func (m *AuthMiddleware) memGet(hash string) *cloudstore.IntuitHubUser {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.mem[hash]
	if !ok {
		return nil
	}
	if m.now().After(e.expiresAt) {
		delete(m.mem, hash)
		return nil
	}
	return e.user
}

func (m *AuthMiddleware) memPut(hash string, u *cloudstore.IntuitHubUser) {
	if u == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mem[hash] = memEntry{user: u, expiresAt: m.now().Add(m.memTTL)}
}
