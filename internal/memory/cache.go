package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
)

// CachedEmbedder wraps an Embedder with an in-memory LRU cache.
type CachedEmbedder struct {
	inner    Embedder
	mu       sync.RWMutex
	cache    map[string][]float32
	order    []string // insertion order for eviction
	maxSize  int
	hits     atomic.Int64
	misses   atomic.Int64
}

// NewCachedEmbedder wraps an embedder with a cache. maxSize=0 defaults to 1000.
func NewCachedEmbedder(inner Embedder, maxSize int) *CachedEmbedder {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &CachedEmbedder{
		inner:   inner,
		cache:   make(map[string][]float32, maxSize),
		maxSize: maxSize,
	}
}

func (c *CachedEmbedder) Name() string       { return c.inner.Name() + " (cached)" }
func (c *CachedEmbedder) Dimensions() int    { return c.inner.Dimensions() }

// CacheHitRate returns the cache hit rate as a percentage.
func (c *CachedEmbedder) CacheHitRate() float64 {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total) * 100
}

// CacheSize returns the current number of cached embeddings.
func (c *CachedEmbedder) CacheSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

func (c *CachedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	key := cacheKey(text)

	// Check cache
	c.mu.RLock()
	if vec, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		c.hits.Add(1)
		// Return a copy to prevent mutation
		result := make([]float32, len(vec))
		copy(result, vec)
		return result, nil
	}
	c.mu.RUnlock()

	// Cache miss — compute embedding
	c.misses.Add(1)
	vec, err := c.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.mu.Lock()
	if len(c.cache) >= c.maxSize && len(c.order) > 0 {
		// Evict oldest
		evictKey := c.order[0]
		c.order = c.order[1:]
		delete(c.cache, evictKey)
	}
	cached := make([]float32, len(vec))
	copy(cached, vec)
	c.cache[key] = cached
	c.order = append(c.order, key)
	c.mu.Unlock()

	return vec, nil
}

func cacheKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:16]) // 128-bit key is sufficient
}
