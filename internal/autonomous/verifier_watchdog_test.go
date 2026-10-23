package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestVerifierWatchdogSilentSessionEndsAtLimit(t *testing.T) {
	start := time.Unix(1000, 0)
	limit := 10 * time.Minute
	w := NewVerifierWatchdog(start, limit)
	if w.Observe(start.Add(limit-time.Second), false, "same") {
		t.Fatal("exceeded before the limit")
	}
	if !w.Observe(start.Add(limit), false, "same") {
		t.Fatal("silent session not ended at the limit")
	}
	if msg := VerifierWatchdogMessage(limit); !strings.Contains(msg, "verifier no-progress watchdog") || !strings.Contains(msg, "10m0s") {
		t.Fatalf("message %q does not name the watchdog and limit", msg)
	}
}

func TestVerifierWatchdogSparesOutputAndChangingPane(t *testing.T) {
	start := time.Unix(1000, 0)
	limit := 10 * time.Minute
	w := NewVerifierWatchdog(start, limit)
	for i := 1; i <= 100; i++ {
		if w.Observe(start.Add(time.Duration(i)*3*time.Minute), false, string(rune('a'+i%26))+string(rune(i))) {
			t.Fatalf("changing pane output ended at poll %d", i)
		}
	}
	w2 := NewVerifierWatchdog(start, limit)
	if w2.Observe(start.Add(3*limit), true, "x") {
		t.Fatal("session with an output file was ended")
	}
}

func TestVerifierWatchdogTimerResetsOnActivity(t *testing.T) {
	start := time.Unix(1000, 0)
	limit := 10 * time.Minute
	w := NewVerifierWatchdog(start, limit)
	w.Observe(start, false, "a")
	if w.Observe(start.Add(9*time.Minute), false, "a") {
		t.Fatal("ended early")
	}
	if w.Observe(start.Add(9*time.Minute+time.Second), false, "b") {
		t.Fatal("ended on activity")
	}
	if w.Observe(start.Add(18*time.Minute), false, "b") {
		t.Fatal("timer did not reset on pane change")
	}
	if !w.Observe(start.Add(19*time.Minute+2*time.Second), false, "b") {
		t.Fatal("not ended a full limit after the last activity")
	}
}

func TestVerifierWatchdogDisabledWhenLimitNotPositive(t *testing.T) {
	w := NewVerifierWatchdog(time.Unix(0, 0), 0)
	if w.Observe(time.Unix(1<<30, 0), false, "") {
		t.Fatal("zero limit must disable the watchdog")
	}
}
