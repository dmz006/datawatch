// Incident reproduction: a pi-prompt worker session that is merely
// working on a task about throttling emits ordinary prose. One output
// line is a normal sentence that quotes one of the rate-limit
// detector's list entries in the middle of the sentence. The current
// detector matches a list entry at any position in a line, so this
// false-positives and flips the live session to rate limited, fires
// the rate-limit listeners (which is the same hook chain the daemon
// uses to request a usage back-off), and triggers the fallback chain.
//
// This test feeds that exact line through the same output-line path
// the daemon uses for live output (Manager.processOutputLine) and
// asserts the false positive does NOT happen. On the current code the
// test FAILS, because the quoted entry is matched as a substring
// anywhere in the line.
package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	// The daemon requests a usage back-off through a rate-limit
	// listener registered with AddRateLimitListener
	// (see cmd/datawatch/usage_backoff_wiring.go: rateLimitBackoffHandler).
	// Register one such listener plus an independent one and assert
	// none of them fire.
	var mu sync.Mutex
	backoffRequests := 0
	listenerCalls := []string{}
	mgr.AddRateLimitListener(func(backend string, resetAt time.Time, hasReset bool) {
		mu.Lock()
		backoffRequests++
		listenerCalls = append(listenerCalls, backend)
		mu.Unlock()
	})
	mgr.AddRateLimitListener(func(backend string, resetAt time.Time, hasReset bool) {
		mu.Lock()
		listenerCalls = append(listenerCalls, backend)
		mu.Unlock()
	})

	// The fallback-chain hook is the other downstream effect of a
	// false positive; it must not be triggered either.
	fallbackCalls := 0
	mgr.SetOnRateLimitFallback(func(*Session) { fallbackCalls++ })

	// A pi-prompt worker session in the running state, set up the
	// same way the nearby detector tests set one up.
	sess := newRateLimitTestSession(t, mgr, "thrq01", "pi-prompt")

	// Ordinary prose from the worker, mid-task: it is documenting
	// what the GitHub-token-throttled security workflow prints when
	// the API is exhausted. The sentence quotes one of the detector's
	// list entries verbatim in the middle of the line (the entry is
	// also one of the RateLimitPatterns defaults in internal/config).
	line := "I checked what the GitHub-token-throttled security workflow prints when the API " +
		"bucket is drained; it outputs You've hit your limit and then sleeps for the retry " +
		"window before attempting the next request."

	// Sanity: the quoted entry really is a registered detector entry.
	found := false
	for _, pat := range mgr.effectiveRateLimitPatterns() {
		if pat == "You've hit your limit" {
			found = true
		}
	}
	if !found {
		t.Fatalf("test setup: quoted phrase is not a registered rate-limit pattern")
	}

	// Feed the line through the same output-line path the daemon uses
	// for live output.
	var lastOutput, lastPrompt time.Time
	var pending []string
	mgr.processOutputLine(context.Background(), sess, nil, line,
		&lastOutput, &pending, &lastPrompt, func() *Tracker { return nil })

	// The quoted prose line is not a rate-limit event:
	// 1. session state must not change to rate limited
	got, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatalf("session %s not found in store", sess.FullID)
	}
	if got.State != StateRunning {
		t.Errorf("session state = %s, want %s (quoted phrase must not rate-limit the session)",
			got.State, StateRunning)
	}

	// 2. no registered rate-limit listener may be called
	//    3. no usage back-off may be requested (the daemon's back-off
	//    request rides on exactly this listener chain)
	mu.Lock()
	calls := len(listenerCalls)
	backs := backoffRequests
	mu.Unlock()
	if calls != 0 {
		t.Errorf("rate-limit listeners fired %d times (targets: %v), want 0", calls, listenerCalls)
	}
	if backs != 0 {
		t.Errorf("usage back-off requested %d times, want 0", backs)
	}

	if fallbackCalls != 0 {
		t.Errorf("rate-limit fallback chain fired %d times, want 0", fallbackCalls)
	}
}
