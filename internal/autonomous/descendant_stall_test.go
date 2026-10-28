package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// All tests use a fake clock: time values are passed directly into Observe.
// No sleeping, tmux, or real processes.

var dscClockZero = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func TestDescendantStall_LimitZeroOrNegativeNeverStalls(t *testing.T) {
	for _, limit := range []time.Duration{0, -time.Minute} {
		tr := NewDescendantStallTracker(dscClockZero, limit)
		now := dscClockZero.Add(24 * time.Hour)
		stalled, reason := tr.Observe(now, 1, dscClockZero, dscClockZero)
		if stalled || reason != "" {
			t.Fatalf("limit %v must disable detection, got stalled=%v reason=%q", limit, stalled, reason)
		}
	}
}

func TestDescendantStall_NoDescendantsResetsClock(t *testing.T) {
	tr := NewDescendantStallTracker(dscClockZero, 10*time.Minute)
	// Worker is observed with live descendants, then silence with no
	// descendants at +11m moves the clock to that point.
	stalled, _ := tr.Observe(dscClockZero.Add(3*time.Minute), 2, time.Time{}, time.Time{})
	if stalled {
		t.Fatal("stalled within the limit")
	}
	now := dscClockZero.Add(11 * time.Minute)
	if stalled, _ := tr.Observe(now, 0, time.Time{}, time.Time{}); stalled {
		t.Fatal("descendants == 0 is not descendant-only silence; must not stall")
	}
	// Back to descendants: even 9 more minutes later (20m from tracker
	// start) we are within the 10m limit from the last no-descendant tick.
	if stalled, _ := tr.Observe(now.Add(9*time.Minute), 1, time.Time{}, time.Time{}); stalled {
		t.Fatal("stalled although the clock was reset by the no-descendants observation")
	}
	// And 11 minutes after the reset: stalled.
	if stalled, _ := tr.Observe(now.Add(11*time.Minute), 1, time.Time{}, time.Time{}); !stalled {
		t.Fatal("want stalled 11m after the clock reset")
	}
}

func TestDescendantStall_FreshLogWriteDoesNotStall(t *testing.T) {
	tr := NewDescendantStallTracker(dscClockZero, 10*time.Minute)
	// 20 minutes since tracker start, but the log was written 2 minutes ago.
	now := dscClockZero.Add(20 * time.Minute)
	stalled, _ := tr.Observe(now, 1, dscClockZero.Add(18*time.Minute), time.Time{})
	if stalled {
		t.Fatal("stalled although the log grew within the limit")
	}
	// A newer workdir file later also keeps resetting; then silence past
	// the limit since the last evidence stalls.
	if stalled, _ := tr.Observe(now, 1, dscClockZero.Add(18*time.Minute), now); stalled {
		t.Fatal("stalled although the workdir file changed at now")
	}
	if stalled, _ := tr.Observe(now.Add(11*time.Minute), 1, dscClockZero.Add(18*time.Minute), now); !stalled {
		t.Fatal("want stalled once silence since the newest evidence exceeds the limit")
	}
}

func TestDescendantStall_FreshWorkdirFileDoesNotStall(t *testing.T) {
	tr := NewDescendantStallTracker(dscClockZero, 10*time.Minute)
	// Zero logModTime is ignored; a fresh workdir file within the limit
	// keeps it from stalling.
	now := dscClockZero.Add(15 * time.Minute)
	stalled, _ := tr.Observe(now, 1, time.Time{}, dscClockZero.Add(14*time.Minute))
	if stalled {
		t.Fatal("stalled although the newest workdir file is within the limit")
	}
}

func TestDescendantStall_NoEvidenceStallsOnlyAfterLimit(t *testing.T) {
	tr := NewDescendantStallTracker(dscClockZero, 10*time.Minute)
	// At exactly the limit: not stalled (stall requires strictly greater).
	if stalled, _ := tr.Observe(dscClockZero.Add(10*time.Minute), 1, time.Time{}, time.Time{}); stalled {
		t.Fatal("stalled at exactly the limit, want only past the limit")
	}
	// Just past the limit: stalled, with the expected reason text.
	now := dscClockZero.Add(10*time.Minute + 9*time.Second)
	stalled, reason := tr.Observe(now, 1, time.Time{}, time.Time{})
	if !stalled {
		t.Fatal("want stalled just past the limit, got not stalled")
	}
	if !strings.Contains(reason, "10m9s") || !strings.Contains(reason, "limit 10m0s") {
		t.Fatalf("reason %q must name the silence duration and the limit", reason)
	}
	if !strings.Contains(reason, "child process") {
		t.Fatalf("reason %q must say the only liveness was a child process", reason)
	}
}

func TestDescendantStall_ResetRestartsClock(t *testing.T) {
	tr := NewDescendantStallTracker(dscClockZero, 10*time.Minute)
	now := dscClockZero.Add(20 * time.Minute)
	if stalled, _ := tr.Observe(now, 1, time.Time{}, time.Time{}); !stalled {
		t.Fatal("want stalled before the reset")
	}
	tr.Reset(now)
	if stalled, _ := tr.Observe(now.Add(9*time.Minute), 1, time.Time{}, time.Time{}); stalled {
		t.Fatal("stalled 9m after Reset, within the 10m limit")
	}
	if stalled, _ := tr.Observe(now.Add(11*time.Minute), 1, time.Time{}, time.Time{}); !stalled {
		t.Fatal("want stalled 11m after Reset")
	}
}

func TestDescendantStallError_WrapsErrWorkerStalled(t *testing.T) {
	reason := "worker in descendant-only silence for 10m9s (limit 10m0s); the only liveness was a child process"
	err := DescendantStallError(reason)
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("errors.Is(err, ErrWorkerStalled) = false: %v", err)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("error message %q does not contain reason %q", err.Error(), reason)
	}
}
