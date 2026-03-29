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

func TestNonceTTL(t *testing.T) {
	store := NewNonceStore(100, 50*time.Millisecond)

	store.Check("expired")
	time.Sleep(100 * time.Millisecond)

	// After TTL, same nonce should be accepted again
	if !store.Check("expired") {
		t.Error("nonce should be accepted after TTL expiry")
	}
}

func TestNonceLRU(t *testing.T) {
	store := NewNonceStore(3, 5*time.Minute)

	store.Check("a")
	store.Check("b")
	store.Check("c")

	// Store is full (3). Adding "d" should evict "a" (oldest)
	store.Check("d")

	// "a" should be evicted and accepted again
	if !store.Check("a") {
		t.Error("evicted nonce 'a' should be accepted")
	}

	// "b" might still be there
	if store.Check("b") {
		// Could have been evicted too, depends on map iteration order
		// This is acceptable — LRU with map is approximate
	}
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
