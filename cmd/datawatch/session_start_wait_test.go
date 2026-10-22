package main

// Boundary mapping for the session-start retry budget: a prompt-delivery
// failure from the session package (ErrPromptNotSubmitted) must become the
// autonomous sentinel ErrSessionNeverStarted when the daemon turns a wait
// loop / prompt delivery error into a task error — the autonomous package
// cannot see session sentinels directly.

import (
	"errors"
	"fmt"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

func TestMapSessionStartErrorPromptNotSubmitted(t *testing.T) {
	err := fmt.Errorf("prompt delivery for session x: %w", session.ErrPromptNotSubmitted)
	got := mapSessionStartError(err)
	if !errors.Is(got, autonomouspkg.ErrSessionNeverStarted) {
		t.Fatalf("errors.Is(got, ErrSessionNeverStarted) = false; got = %v", got)
	}
	// The cause must survive the mapping.
	if !errors.Is(got, session.ErrPromptNotSubmitted) {
		t.Fatalf("mapped error lost its ErrPromptNotSubmitted cause: %v", got)
	}
}

func TestMapSessionStartErrorOthersUnchanged(t *testing.T) {
	// non-start errors pass through unchanged
	other := errors.New("git diff failed")
	if got := mapSessionStartError(other); got != other {
		t.Fatalf("non-start error changed: %v", got)
	}
	// nil in, nil out
	if got := mapSessionStartError(nil); got != nil {
		t.Fatalf("nil mapped to %v", got)
	}
	// an error that already carries the autonomous sentinel passes through
	mapped := fmt.Errorf("session never started: %w: prompt not submitted", autonomouspkg.ErrSessionNeverStarted)
	if got := mapSessionStartError(mapped); got != mapped {
		t.Fatalf("already-mapped error changed: %v", got)
	}
}
