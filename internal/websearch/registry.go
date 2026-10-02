package websearch

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// registeredProvider pairs a configured provider's identity with its live
// Provider implementation and per-provider cache TTL override.
type registeredProvider struct {
	Name     string
	Type     string
	Priority int
	CacheTTL time.Duration // 0 = use Registry's default
	Impl     Provider
}

// Registry holds the configured, priority-ordered set of search providers
// plus the shared cache and usage store. This is the single entry point
// both `datawatch mcp-search` and any in-daemon caller (REST test-provider
// endpoint, future direct callers) should use — it is what actually
// implements "try providers in priority order, first success wins, record
// usage either way, check cache first."
type Registry struct {
	providers  []registeredProvider
	cache      *Cache
	store      *Store
	defaultTTL time.Duration
}

// ProviderSpec is the minimal info Registry needs to build one provider —
// deliberately decoupled from internal/config.SearchProvider so this
// package has no import-cycle risk back to internal/config.
type ProviderSpec struct {
	Name            string
	Type            string // "searxng" | "brave"
	Enabled         bool
	Priority        int
	URL             string // searxng
	Engine          string // searxng
	APIKey          string // brave — already-resolved (${secret:...} expanded), never a literal ref
	NumResults      int
	CacheTTLSeconds int // 0 = use registry default
}

// NewRegistry builds a Registry from the given provider specs (only enabled
// ones are instantiated — disabled entries stay configured but inert),
// shared cache (nil-safe: a nil *Cache behaves as "caching disabled"), and
// usage store (nil-safe: a nil *Store makes Record/Summary/History no-ops,
// useful for tests or a misconfigured data dir that shouldn't block search
// itself from working).
func NewRegistry(specs []ProviderSpec, cache *Cache, store *Store, defaultTTL time.Duration) (*Registry, error) {
	r := &Registry{cache: cache, store: store, defaultTTL: defaultTTL}
	for _, spec := range specs {
		if !spec.Enabled {
			continue
		}
		impl, err := NewProvider(spec.Type, spec.URL, spec.Engine, spec.APIKey, spec.NumResults)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", spec.Name, err)
		}
		ttl := time.Duration(0)
		if spec.CacheTTLSeconds > 0 {
			ttl = time.Duration(spec.CacheTTLSeconds) * time.Second
		}
		r.providers = append(r.providers, registeredProvider{
			Name: spec.Name, Type: spec.Type, Priority: spec.Priority, CacheTTL: ttl, Impl: impl,
		})
	}
	sort.SliceStable(r.providers, func(i, j int) bool {
		return r.providers[i].Priority < r.providers[j].Priority
	})
	return r, nil
}

// Enabled reports whether at least one provider is active.
func (r *Registry) Enabled() bool {
	return r != nil && len(r.providers) > 0
}

// ProviderNames returns the configured (enabled) provider names in try
// order — used by callers that want to report "which providers would be
// tried" without actually searching (e.g. a status endpoint).
func (r *Registry) ProviderNames() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.providers))
	for i, p := range r.providers {
		out[i] = p.Name
	}
	return out
}

// Search tries enabled providers in priority order, serving from cache when
// possible, and records a usage Event for every attempt (cache hit, success,
// or failure) so stats/history reflect what actually happened. sessionID is
// optional (empty string when not known, e.g. a CLI/REST test call) and is
// recorded purely for the history view's "where used" column.
//
// Returns the results, the name of the provider that actually served them
// ("cache" if served from cache), and an error only when every provider
// failed (a single provider's failure is recorded but not returned — the
// next provider gets a chance).
func (r *Registry) Search(ctx context.Context, query string, limit int, sessionID string) ([]Result, string, error) {
	if r == nil || len(r.providers) == 0 {
		return nil, "", fmt.Errorf("websearch: no enabled providers configured")
	}

	// Cache is keyed per-provider (different providers can return different
	// results for the same query), so check each provider's cache slot in
	// priority order before falling through to a live call — a cache hit on
	// the first provider should win even if a lower-priority provider was
	// never tried live.
	for _, p := range r.providers {
		if results, ok := r.cache.Get(p.Name, query, limit); ok {
			r.record(ctx, Event{
				Time: time.Now(), ProviderName: p.Name, ProviderType: p.Type,
				Query: query, SessionID: sessionID, CacheHit: true, Success: true,
				ResultCount: len(results),
			})
			return results, p.Name, nil
		}
	}

	var lastErr error
	for _, p := range r.providers {
		start := time.Now()
		results, err := p.Impl.Search(ctx, query, limit)
		latency := time.Since(start)
		if err != nil {
			lastErr = err
			r.record(ctx, Event{
				Time: start, ProviderName: p.Name, ProviderType: p.Type,
				Query: query, SessionID: sessionID, Success: false, Error: err.Error(),
				LatencyMS: latency.Milliseconds(),
			})
			continue
		}
		if len(results) == 0 {
			// Empty-but-no-error: treat as a miss, try the next provider —
			// this is what protects against GH#165-style silent
			// degradation (a provider that "succeeds" with junk/empty
			// results shouldn't block a better provider from being tried).
			r.record(ctx, Event{
				Time: start, ProviderName: p.Name, ProviderType: p.Type,
				Query: query, SessionID: sessionID, Success: true,
				LatencyMS: latency.Milliseconds(), ResultCount: 0,
			})
			continue
		}
		r.record(ctx, Event{
			Time: start, ProviderName: p.Name, ProviderType: p.Type,
			Query: query, SessionID: sessionID, Success: true,
			LatencyMS: latency.Milliseconds(), ResultCount: len(results),
		})
		r.cache.Set(p.Name, query, limit, results, p.CacheTTL)
		return results, p.Name, nil
	}
	if lastErr != nil {
		return nil, "", fmt.Errorf("websearch: all providers failed, last error: %w", lastErr)
	}
	return nil, "", fmt.Errorf("websearch: all providers returned empty results")
}

func (r *Registry) record(ctx context.Context, e Event) {
	if r.store == nil {
		return
	}
	// Recording must never block or fail the search itself — usage logging
	// is observability, not a correctness dependency. Best-effort, errors
	// swallowed (nothing actionable a caller could do with a stats-write
	// failure mid-search).
	_ = r.store.Record(ctx, e)
}

// Summary, DailySeries, and History proxy to the underlying Store so
// callers only need to hold a *Registry, not a separate *Store reference.
func (r *Registry) Summary(ctx context.Context) (Summary, error) {
	if r == nil {
		return Summary{}, nil
	}
	return r.store.Summary(ctx)
}

func (r *Registry) DailySeries(ctx context.Context, days int) ([]DayCount, error) {
	if r == nil {
		return nil, nil
	}
	return r.store.DailySeries(ctx, days)
}

func (r *Registry) History(ctx context.Context, limit, offset int) ([]HistoryEntry, error) {
	if r == nil {
		return nil, nil
	}
	return r.store.History(ctx, limit, offset)
}

// StoreHandle returns the Registry's underlying *Store so a caller rebuilding
// the registry after a config change (e.g. a provider CRUD edit) can reuse
// the same SQLite handle instead of reopening the database file.
func (r *Registry) StoreHandle() *Store {
	if r == nil {
		return nil
	}
	return r.store
}
