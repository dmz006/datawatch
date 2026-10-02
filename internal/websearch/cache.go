package websearch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// cacheEntry is one cached result set.
type cacheEntry struct {
	results   []Result
	expiresAt time.Time
}

// Cache is a simple in-memory, per-process TTL result cache keyed by
// provider name + normalized query + limit. Deliberately not persisted to
// disk across restarts — search results go stale and the daemon restarts
// often enough (deploys) that a warm-start cache miss is the safer default
// over silently serving an old cached page from before a restart.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

// NewCache creates a cache with the given default TTL. ttl<=0 disables
// caching (Get always misses, Set is a no-op) without the caller needing a
// separate enabled/disabled branch.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{entries: make(map[string]cacheEntry), ttl: ttl}
}

func cacheKey(providerName, query string, limit int) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d", providerName, query, limit))
	return hex.EncodeToString(h[:16])
}

// Get returns a cached result set and true if present and not expired.
func (c *Cache) Get(providerName, query string, limit int) ([]Result, bool) {
	if c == nil || c.ttl <= 0 {
		return nil, false
	}
	key := cacheKey(providerName, query, limit)
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		if ok {
			delete(c.entries, key) // expired — evict
		}
		return nil, false
	}
	return e.results, true
}

// Set stores a result set under the given TTL (falls back to the cache's
// default TTL if ttlOverride<=0). No-op when caching is disabled.
func (c *Cache) Set(providerName, query string, limit int, results []Result, ttlOverride time.Duration) {
	if c == nil || (c.ttl <= 0 && ttlOverride <= 0) {
		return
	}
	ttl := c.ttl
	if ttlOverride > 0 {
		ttl = ttlOverride
	}
	key := cacheKey(providerName, query, limit)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{results: results, expiresAt: time.Now().Add(ttl)}
}

// Len returns the current number of live (not-yet-expired-on-last-access)
// entries — used for a diagnostics/stats surface, not load-bearing logic.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
