// Exit-silence wait loop (pi-prompt `pi --print` runs that exit without a
// completion or verdict): exitSilenceTick and its wiring into main.go's
// waitLoop.
package main

import (
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestExitSilenceWait_NonPiFamily_NeverStalled(t *testing.T) {
	limit := 900 * time.Second
	tr := autonomouspkg.NewExitSilenceTracker(time.Now().Add(-limit*2), limit)
	probes := 0
	stalled, reason := exitSilenceTick(tr, "claude-code", time.Now(),
		func() bool { probes++; return false },
		func() time.Time { probes++; return time.Time{} })
	if stalled || reason != "" {
		t.Fatalf("claude-code family must never report stalled, got stalled=%v reason=%q", stalled, reason)
	}
	if probes != 0 {
		t.Fatalf("non-pi family must not call the probes (got %d calls)", probes)
	}
	// A nil tracker (family not yet visible at tracker construction) is
	// likewise a no-op.
	if stalled, reason := exitSilenceTick(nil, "pi-prompt", time.Now(),
		func() bool { return false },
		func() time.Time { return time.Time{} }); stalled || reason != "" {
		t.Fatalf("nil tracker must never report stalled, got stalled=%v reason=%q", stalled, reason)
	}
}

func TestExitSilenceWait_PiPrompt_SilencePastLimit_Stalled(t *testing.T) {
	limit := 900 * time.Second
	start := time.Unix(0, 0)
	tr := autonomouspkg.NewExitSilenceTracker(start, limit)
	now := start.Add(limit + time.Minute)
	stalled, reason := exitSilenceTick(tr, "pi-prompt", now,
		func() bool { return false },            // runner gone
		func() time.Time { return time.Time{} }) // log never written / stat error
	if !stalled {
		t.Fatalf("expected stalled with runner gone and silence past the limit, got reason=%q", reason)
	}
	for _, want := range []string{
		"pi print run exited without a completion or verdict",
		"silent for",
		"16m0s", // 960s of silence past the limit
		"15m0s", // the 900s limit
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q missing %q", reason, want)
		}
	}
	// The tracker's state must be preserved across calls (same tracker
	// instance each tick in the real wait loop).
	if re, rer := exitSilenceTick(tr, "pi-prompt", now,
		func() bool { return false },
		func() time.Time { return time.Time{} }); !re || rer != reason {
		t.Errorf("second tick should still report the same stall, got stalled=%v reason=%q", re, rer)
	}
}

func TestExitSilenceWait_RunnerAlive_KeepsQuiet(t *testing.T) {
	tr := autonomouspkg.NewExitSilenceTracker(time.Unix(0, 0), 900*time.Second)
	now := time.Unix(0, 0).Add(2 * time.Hour)
	stalled, reason := exitSilenceTick(tr, "pi-prompt", now,
		func() bool { return true }, // runner still alive
		func() time.Time { return time.Time{} })
	if stalled || reason != "" {
		t.Fatalf("runner alive must stay quiet, got stalled=%v reason=%q", stalled, reason)
	}
}

func TestExitSilenceWait_RecentLogMtime_KeepsQuiet(t *testing.T) {
	limit := 900 * time.Second
	tr := autonomouspkg.NewExitSilenceTracker(time.Unix(0, 0), limit)
	now := time.Unix(0, 0).Add(10 * time.Minute)
	recentLog := now.Add(-10 * time.Second) // log grew 10s before "now"
	stalled, reason := exitSilenceTick(tr, "pi-prompt", now,
		func() bool { return false },
		func() time.Time { return recentLog })
	if stalled || reason != "" {
		t.Fatalf("recent log mtime must keep it quiet, got stalled=%v reason=%q", stalled, reason)
	}
}

// Source-inspection guard (same style as session_judgment_wiring_test.go):
// the wait loop must actually call exitSilenceTick, not just define it.
func TestExitSilenceWait_WiredIntoWaitLoop(t *testing.T) {
	src := readMainGo(t)
	for _, want := range []string{
		"exitSilenceTick(",
		"autonomouspkg.NewExitSilenceTracker(",
		"Task session exited silently",
		"autonomouspkg.ExitSilenceError(reason)",
	} {
		if !contains(src, want) {
			t.Errorf("main.go missing %q — exit-silence wiring disappeared", want)
		}
	}
}
