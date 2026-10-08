// SEC-018 — discussionThrottleMap (a sync.Map keyed by raw Bearer token)
// had no eviction at all: every distinct token ever seen stayed in memory
// for the life of the daemon process. These tests pin the bounded TTL
// sweep that replaces that.

package server

import (
	"testing"
	"time"
)

func TestDiscussionThrottleSweep_EvictsIdleBuckets(t *testing.T) {
	resetThrottleMap()
	defer resetThrottleMap()

	// A bucket touched long enough ago to be past the idle TTL.
	stale := &throttleBucket{tokens: 60, lastAt: time.Now().Add(-discussionThrottleIdleTTL - time.Minute), rate: 1.0, max: 60}
	discussionThrottleMap.Store("stale-token", stale)

	// A bucket touched recently — must survive the sweep.
	fresh := &throttleBucket{tokens: 60, lastAt: time.Now(), rate: 1.0, max: 60}
	discussionThrottleMap.Store("fresh-token", fresh)

	discussionThrottleSweep()

	if _, ok := discussionThrottleMap.Load("stale-token"); ok {
		t.Error("a bucket idle past the TTL must be evicted by the sweep")
	}
	if _, ok := discussionThrottleMap.Load("fresh-token"); !ok {
		t.Error("a recently-touched bucket must survive the sweep")
	}
}

func TestDiscussionThrottleSweep_RecreatedWithFullBucketAfterEviction(t *testing.T) {
	resetThrottleMap()
	defer resetThrottleMap()

	tok := "evicted-then-returns"
	b := discussionThrottleBucket(tok)
	// Drain it most of the way down.
	for i := 0; i < 50; i++ {
		b.allow()
	}

	// Force eviction regardless of real elapsed time.
	b.mu.Lock()
	b.lastAt = time.Now().Add(-discussionThrottleIdleTTL - time.Minute)
	b.mu.Unlock()
	discussionThrottleSweep()

	if _, ok := discussionThrottleMap.Load(tok); ok {
		t.Fatal("bucket should have been evicted before re-fetching")
	}

	// A fresh LoadOrStore after eviction gets a brand-new, fully-refilled bucket.
	again := discussionThrottleBucket(tok)
	if !again.allow() {
		t.Error("a bucket recreated after eviction should start fully refilled, not still drained")
	}
}

func TestDiscussionThrottleBucket_StartsBackgroundSweeperOnce(t *testing.T) {
	resetThrottleMap()
	defer resetThrottleMap()

	// Calling discussionThrottleBucket multiple times must not panic or
	// start multiple sweeper goroutines (sync.Once) -- this is mostly a
	// smoke test that the lazy-start path is safe to call repeatedly.
	for i := 0; i < 5; i++ {
		discussionThrottleBucket("tok")
	}
}
