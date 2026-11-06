package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// TestVerifierIdleWiring pins that the one-shot loop's watchdog trip returns
// a VerifierIdleError, so the executor's idle retry path (verifyRetryingIdle,
// which classifies via autonomous.IsVerifierIdleError) sees the typed error
// and retries on a fresh verifier instead of counting a worker failure.
func TestVerifierIdleWiring(t *testing.T) {
	const limit = 15 * time.Minute
	err := verifierWatchdogTripError(limit)

	// The typed error, not just the text spelling.
	var idle *autonomouspkg.VerifierIdleError
	if !errors.As(err, &idle) {
		t.Fatalf("watchdog trip error = %T (%v), want *autonomous.VerifierIdleError", err, err)
	}

	// Standard watchdog text, so logs and incident records read identically
	// to the OneShotNoOutputError spelling the loop used to build.
	want := autonomouspkg.VerifierWatchdogMessage(limit)
	if err.Error() != want {
		t.Errorf("watchdog trip error text = %q, want %q", err.Error(), want)
	}

	// The executor's retry path classifies it as a verifier idle.
	if !autonomouspkg.IsVerifierIdleError(err) {
		t.Error("IsVerifierIdleError(watchdog trip) = false, want true (executor must treat it as a verifier incident)")
	}

	// The legacy text spelling (a OneShotNoOutputError carrying the watchdog
	// message) must still classify as an idle — older callers that raise it
	// directly keep the same retry behavior.
	legacy := &autonomouspkg.OneShotNoOutputError{Msg: autonomouspkg.VerifierWatchdogMessage(limit)}
	if !autonomouspkg.IsVerifierIdleError(legacy) {
		t.Error("IsVerifierIdleError(legacy OneShotNoOutputError) = false, want true")
	}

	// Negative controls: ordinary one-shot failures are worker failures, not
	// verifier idles — the retry path must not touch them.
	controls := []error{
		&autonomouspkg.OneShotNoOutputError{Msg: "one-shot verifier session sess-1 ended with state waiting_input without producing output (backend=claude-code, model=\"x\")"},
		&autonomouspkg.OneShotNoOutputError{Msg: "one-shot timeout after 15m0s", TimedOut: true},
		fmt.Errorf("session start: malformed JSON"),
	}
	for i, tc := range controls {
		if autonomouspkg.IsVerifierIdleError(tc) {
			t.Errorf("controls[%d] = %v classified as a verifier idle, want worker failure", i, tc)
		}
	}
}
