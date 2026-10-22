package main

import (
	"os"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// silentPaneFixture mirrors the real daemon's capture: a claude-code pane
// sitting in waiting_input (startup/confirmation dialog) with no assistant
// turn rendered after the prompt.
func silentPaneFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../internal/autonomous/testdata/start/silent_waiting_input.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pane := string(b)
	const wantFooter = "Enter to confirm · Esc to cancel"
	if !contains(pane, wantFooter) {
		t.Fatalf("fixture must contain %q (a real waiting-input footer); it doesn't look like a waiting-input pane", wantFooter)
	}
	return pane
}

// TestSessionStartWaitDecision exercises the wait-loop's one-tick decision
// with a FAKE clock (time.Time values, no real sleep): the silent pane must
// not trip before the bound, must not trip AT the bound, and must trip the
// moment the tick clock passes it — while a pane that does show an assistant
// turn never trips.
func TestSessionStartWaitDecision(t *testing.T) {
	const limit = 120 * time.Second // autonomous.session_start_confirm_seconds default
	const tick = 3 * time.Second    // the wait loop's ticker period
	base := time.Now()              // "injected clock" origin; absolute value is irrelevant

	silent := silentPaneFixture(t)
	if autonomouspkg.HasAssistantOutput(silent) {
		t.Fatalf("silent fixture unexpectedly contains an assistant response line; fixture is broken")
	}

	// Tick 0: clock has not started (session just entered waiting_input).
	obs, stalled := evaluateSessionStart(base, time.Time{}, "waiting_input", silent, limit)
	if stalled {
		t.Fatalf("freshly observed waiting_input reported stalled immediately")
	}
	if obs.HasAssistantOutput || obs.State != "waiting_input" || obs.Limit != limit {
		t.Fatalf("observation mis-built: %+v", obs)
	}

	// Ticks 1..40: 3s, 6s, ... 120s. Nothing before or AT the bound trips.
	started := base
	for i := 1; i <= int(limit/tick); i++ {
		now := started.Add(time.Duration(i) * tick)
		_, stalled = evaluateSessionStart(now, started, "waiting_input", silent, limit)
		if stalled {
			t.Fatalf("tick %d (%s into the wait) reported stalled before or at the %s bound — boundary must not trip at equality", i, now.Sub(started), limit)
		}
	}

	// Tick 41: 123s > 120s bound — stalled, and every later tick stays
	// stalled with a monotonically non-decreasing WaitingFor.
	var lastWaiting time.Duration
	for i := int(limit/tick) + 1; i <= int(limit/tick)+5; i++ {
		now := started.Add(time.Duration(i) * tick)
		obs, stalled = evaluateSessionStart(now, started, "waiting_input", silent, limit)
		if !stalled {
			t.Fatalf("tick %d (%s into the wait) did NOT report stalled past the bound", i, now.Sub(started))
		}
		if obs.WaitingFor != now.Sub(started) {
			t.Fatalf("WaitingFor = %s, want %s (driven purely by the injected clock)", obs.WaitingFor, now.Sub(started))
		}
		if obs.WaitingFor < lastWaiting {
			t.Fatalf("WaitingFor decreased across ticks: %s then %s", lastWaiting, obs.WaitingFor)
		}
		lastWaiting = obs.WaitingFor
		if _, reason := obs.Stalled(); reason != autonomouspkg.SessionStartFailureReason {
			t.Fatalf("stall reason %q, want %q", reason, autonomouspkg.SessionStartFailureReason)
		}
	}
}

// TestSessionStartWaitDecisionActivePane — a session that does show its
// first assistant turn is, by definition, not "never started", no matter
// how long it then sits in waiting_input. Its behavior is unchanged.
func TestSessionStartWaitDecisionActivePane(t *testing.T) {
	const limit = 120 * time.Second
	base := time.Now()
	active := "❯ summarize the latest PR\n• I will start by reading the PR diff and the linked issues.\n"
	obs, stalled := evaluateSessionStart(base.Add(5*time.Minute), base, "waiting_input", active, limit)
	if stalled {
		t.Fatalf("pane with an assistant turn reported stalled — active sessions must be unaffected")
	}
	if !obs.HasAssistantOutput {
		t.Fatalf("active pane not recognized as assistant output: %+v", obs)
	}
	// And a zero limit disables the check entirely (defensive).
	if _, stalled := evaluateSessionStart(base.Add(5*time.Minute), base, "waiting_input", silentPaneFixture(t), 0); stalled {
		t.Fatalf("zero limit must disable the start-confirm check")
	}
}
