// BL406 Phase 1 — regression test for the false-positive incident where
// the detector fired on a quoted phrase in a tool output line, not a
// real throttle notice.
//
// A pi-prompt worker session that is merely working on a task about
// throttling emits ordinary prose. One output line is a normal sentence
// that quotes a rate-limit phrase in the middle of the sentence. Before
// the per-backend anchored matchers, detection was a case-insensitive
// substring match against a flat string list: that line matched, the
// live session flipped to rate-limited, the rate-limit listeners
// (the same hook chain the daemon uses to request a usage back-off)
// fired, and the fallback chain was triggered on a healthy session.
//
// Anchored (line-start) per-backend matching makes the regression
// impossible: a phrase must lead the trimmed line of this session's
// backend family to count (see rate_limit_lines.go for rules and
// sources). This test feeds that exact line through the same
// output-line path the daemon uses for live output
// (Manager.processOutputLine) and asserts, end to end, that no
// listener fires, no fallback chain triggers, and the worker stays
// running.
package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput pins the
// anchored-matching contract at the end-to-end level: a line that quotes
// a claude-code built-in phrase mid-sentence, on a pi-prompt worker,
// must not flip state, must not fire any rate-limit listener (the
// daemon's usage back-off rides on exactly this hook chain), and must
// not trigger the fallback chain. The session starts running; after the
// line it must still be running.
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

	// Sanity: the quoted phrase really is a registered built-in detector
	// entry (claude-code; the quoted line lands on a pi-prompt session, so
	// per-backend scoping is what keeps it from matching).
	sanFound := false
	for _, p := range builtinRateLimitPhrases["claude-code"] {
		if p == strings.ToLower("You've hit your limit") {
			sanFound = true
			break
		}
	}
	if !sanFound {
		t.Fatalf("test setup: quoted phrase is not a registered built-in rate-limit pattern")
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
