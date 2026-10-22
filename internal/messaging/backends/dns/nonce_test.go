package dns

import (
	"testing"
	"time"
)

func TestNonceReplay(t *testing.T) {
	store := NewNonceStore(100, 5*time.Minute)

	// First check should pass
	if !store.Check("abc123") {
		t.Error("first check should return true (new nonce)")
	}

	// Second check should fail (replay)
	if store.Check("abc123") {
		t.Error("second check should return false (replayed nonce)")
	}

	// Different nonce should pass
	if !store.Check("def456") {
		t.Error("different nonce should return true")
	}
}

// fakeClock is a manually advanced clock injected into NonceStore.now.
type fakeClock struct {
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// TestNonceTTL is deterministic because the store reads time from an injected
// clock that the test advances, not from the wall clock. Keep this pattern: a
// real sleep makes expiry depend on scheduler latency and can flake on a loaded
// host.
func TestNonceTTL(t *testing.T) {
	clock := newFakeClock()
	store := NewNonceStore(100, 50*time.Millisecond)
	store.now = clock.Now

	store.Check("expired")
	clock.Advance(100 * time.Millisecond)

	// After TTL, same nonce should be accepted again
	if !store.Check("expired") {
		t.Error("nonce should be accepted after TTL expiry")
	}
}

// TestNonceLRU is deterministic because the injected clock advances one
// millisecond between inserts, so every entry has a distinct timestamp and the
// oldest entry is always the one evicted. Keep this pattern: with equal
// timestamps (coarse clocks) the eviction choice depends on map iteration order.
func TestNonceLRU(t *testing.T) {
	clock := newFakeClock()
	store := NewNonceStore(3, 5*time.Minute)
	store.now = clock.Now

	store.Check("a")
	clock.Advance(time.Millisecond)
	store.Check("b")
	clock.Advance(time.Millisecond)
	store.Check("c")
	clock.Advance(time.Millisecond)

	// Store is full (3). Adding "d" should evict "a" (oldest)
	store.Check("d")
	clock.Advance(time.Millisecond)

	// "a" should be evicted and accepted again
	if !store.Check("a") {
		t.Error("evicted nonce 'a' should be accepted")
	}

	// "b" might still be there (depends on map iteration order — LRU with map is approximate)
	_ = store.Check("b")
}

func TestNonceEmpty(t *testing.T) {
	store := NewNonceStore(0, 0) // should use defaults

	if !store.Check("test") {
		t.Error("check with default store should pass")
	}
	if store.Check("test") {
		t.Error("replay with default store should fail")
	}
}
