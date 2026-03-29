package dns

import (
	"sync"
	"time"
)

// NonceStore is a bounded, TTL-expiring set of seen nonces for replay protection.
type NonceStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	maxSize int
	ttl     time.Duration
}

// NewNonceStore creates a NonceStore with the given capacity and TTL.
func NewNonceStore(maxSize int, ttl time.Duration) *NonceStore {
	if maxSize <= 0 {
		maxSize = 10000
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &NonceStore{
		entries: make(map[string]time.Time, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Check returns true if the nonce is new (not seen before).
// If new, it is added to the store. If seen, returns false.
func (n *NonceStore) Check(nonce string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Evict expired entries
	now := time.Now()
	for k, t := range n.entries {
		if now.Sub(t) > n.ttl {
			delete(n.entries, k)
		}
	}

	// Check for replay
	if _, seen := n.entries[nonce]; seen {
		return false
	}

	// Evict oldest if at capacity
	if len(n.entries) >= n.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, t := range n.entries {
			if oldestTime.IsZero() || t.Before(oldestTime) {
				oldestKey = k
				oldestTime = t
			}
		}
		if oldestKey != "" {
			delete(n.entries, oldestKey)
		}
	}

	n.entries[nonce] = now
	return true
}
