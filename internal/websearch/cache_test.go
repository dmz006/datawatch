package websearch

import (
	"testing"
	"time"
)

func TestCacheHitMiss(t *testing.T) {
	c := NewCache(time.Minute)
	if _, ok := c.Get("p1", "q1", 10); ok {
		t.Fatal("expected miss on empty cache")
	}
	results := []Result{{Title: "t", URL: "u", Content: "c"}}
	c.Set("p1", "q1", 10, results, 0)
	got, ok := c.Get("p1", "q1", 10)
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if len(got) != 1 || got[0].Title != "t" {
		t.Errorf("unexpected cached results: %+v", got)
	}
}

func TestCacheKeyDistinguishesProviderQueryLimit(t *testing.T) {
	c := NewCache(time.Minute)
	c.Set("p1", "q", 10, []Result{{Title: "p1-result"}}, 0)
	c.Set("p2", "q", 10, []Result{{Title: "p2-result"}}, 0)
	c.Set("p1", "q", 5, []Result{{Title: "p1-limit5"}}, 0)

	r1, _ := c.Get("p1", "q", 10)
	r2, _ := c.Get("p2", "q", 10)
	r3, _ := c.Get("p1", "q", 5)
	if r1[0].Title != "p1-result" || r2[0].Title != "p2-result" || r3[0].Title != "p1-limit5" {
		t.Errorf("cache keys collided: r1=%+v r2=%+v r3=%+v", r1, r2, r3)
	}
}

func TestCacheExpiry(t *testing.T) {
	c := NewCache(10 * time.Millisecond)
	c.Set("p", "q", 10, []Result{{Title: "t"}}, 0)
	if _, ok := c.Get("p", "q", 10); !ok {
		t.Fatal("expected hit immediately after Set")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Get("p", "q", 10); ok {
		t.Fatal("expected miss after TTL expiry")
	}
	if c.Len() != 0 {
		t.Errorf("expired entry should be evicted on access, Len()=%d", c.Len())
	}
}

func TestCacheDisabledViaZeroTTL(t *testing.T) {
	c := NewCache(0)
	c.Set("p", "q", 10, []Result{{Title: "t"}}, 0)
	if _, ok := c.Get("p", "q", 10); ok {
		t.Fatal("expected miss — caching disabled (ttl<=0)")
	}
}

func TestCachePerEntryTTLOverride(t *testing.T) {
	c := NewCache(time.Hour) // long default
	c.Set("p", "q", 10, []Result{{Title: "t"}}, 5*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("p", "q", 10); ok {
		t.Fatal("expected per-entry TTL override to expire before the long default")
	}
}

func TestNilCacheIsSafe(t *testing.T) {
	var c *Cache
	if _, ok := c.Get("p", "q", 10); ok {
		t.Fatal("nil cache should always miss")
	}
	c.Set("p", "q", 10, []Result{{Title: "t"}}, 0) // must not panic
	if c.Len() != 0 {
		t.Errorf("nil cache Len() should be 0, got %d", c.Len())
	}
}
