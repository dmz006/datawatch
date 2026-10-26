package main

import (
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestIdleLivenessProbe(t *testing.T) {
	runner := func(string, string) bool { return true }
	child := func(string, string) bool { return false }
	tests := []struct {
		family     string
		wantRunner bool
	}{
		{"pi-prompt", true},
		{"claude-code", false},
		{"opencode", false},
		{"some-unknown-family", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.family, func(t *testing.T) {
			var ranRunner, ranChild bool
			r := func(a, b string) bool { ranRunner = true; return runner(a, b) }
			c := func(a, b string) bool { ranChild = true; return child(a, b) }
			got := selectIdleLivenessProbe(tt.family, r, c)("sess", "dir")
			if tt.wantRunner {
				if !ranRunner || ranChild || !got {
					t.Errorf("family %q: runner=%v child=%v got=%v, want runner probe only", tt.family, ranRunner, ranChild, got)
				}
			} else if ranRunner || !ranChild || got {
				t.Errorf("family %q: runner=%v child=%v got=%v, want child probe only", tt.family, ranRunner, ranChild, got)
			}
		})
	}
	if idleLivenessProbe("pi-prompt") == nil || idleLivenessProbe("claude-code") == nil {
		t.Fatal("idleLivenessProbe must never return nil")
	}
}

func TestIdleLivenessProbe_LiveRunnerNoChildNotStalled(t *testing.T) {
	probe := selectIdleLivenessProbe("pi-prompt",
		func(string, string) bool { return true },
		func(string, string) bool { return false })
	obs := autonomouspkg.SessionIdleObservation{
		State:               "waiting_input",
		IdleFor:             2 * time.Hour,
		Limit:               time.Minute,
		HasLiveChildProcess: probe("sess", "dir"),
	}
	if stalled, reason := obs.Stalled(); stalled {
		t.Fatalf("live one-shot runner with no child must not be stalled: %s", reason)
	}
	// The same observation built with the child probe reads as idle.
	obs.HasLiveChildProcess = selectIdleLivenessProbe("claude-code",
		func(string, string) bool { return true },
		func(string, string) bool { return false })("sess", "dir")
	if stalled, _ := obs.Stalled(); !stalled {
		t.Fatal("non-pi family with no child must still be stalled")
	}
}
