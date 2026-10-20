package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// All tests use a fake clock: time values are passed directly into Observe.
// No sleeping, tmux, or real processes.

var esClockZero = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func TestExitSilence_NotStalledBeforeLimit(t *testing.T) {
	tr := NewExitSilenceTracker(esClockZero, 10*time.Minute)
	for _, mins := range []float64{0, 1, 5, 9.9} {
		now := esClockZero.Add(time.Duration(mins * float64(time.Minute)))
		stalled, _ := tr.Observe(now, false, time.Time{})
		if stalled {
			t.Fatalf("stalled at +%vm with runner gone, want not stalled", mins)
		}
	}
}

func TestExitSilence_StalledJustAfterLimit(t *testing.T) {
	tr := NewExitSilenceTracker(esClockZero, 10*time.Minute)
	const eps = 9 * time.Second // just over the limit
	now := esClockZero.Add(10*time.Minute + eps)
	stalled, reason := tr.Observe(now, false, time.Time{})
	if !stalled {
		t.Fatal("want stalled just past the limit, got not stalled")
	}
	want := "pi print run exited without a completion or verdict (silent for 10m9s after the runner stopped, limit 10m0s)"
	if reason != want {
		t.Fatalf("reason mismatch:\n got  %q\n want %q", reason, want)
	}
}

func TestExitSilence_NeverStalledWhileRunnerAlive(t *testing.T) {
	tr := NewExitSilenceTracker(esClockZero, 5*time.Minute)
	for _, hours := range []int{1, 6, 48} {
		now := esClockZero.Add(time.Duration(hours) * time.Hour)
		stalled, _ := tr.Observe(now, true, time.Time{})
		if stalled {
			t.Fatalf("stalled while runner alive at +%dh, want never", hours)
		}
	}
}

func TestExitSilence_LogGrowthResetsClock(t *testing.T) {
	tr := NewExitSilenceTracker(esClockZero, 10*time.Minute)
	// 11 minutes silent, but the log mtime is 1 minute ago: runner gone both times.
	now := esClockZero.Add(11 * time.Minute)
	stalled, _ := tr.Observe(now, false, esClockZero.Add(10*time.Minute))
	if stalled {
		t.Fatal("stalled although the log grew within the limit")
	}
	// Silence measured from the log mtime now exceeds the limit: stalled.
	stalled, _ = tr.Observe(esClockZero.Add(21*time.Minute), false, time.Time{})
	if !stalled {
		t.Fatal("want stalled once silence since the last log growth exceeds the limit")
	}
}

func TestExitSilence_RunnerAliveThenGoneStartsClockAtLastAlive(t *testing.T) {
	tr := NewExitSilenceTracker(esClockZero, 10*time.Minute)
	alive := esClockZero.Add(30 * time.Minute)
	if stalled, _ := tr.Observe(alive, true, time.Time{}); stalled {
		t.Fatal("stalled while runner alive")
	}
	// 9 minutes after the runner was last seen: not yet stalled, even
	// although 39 minutes have elapsed since tracker start.
	if stalled, _ := tr.Observe(alive.Add(9*time.Minute), false, time.Time{}); stalled {
		t.Fatal("stalled 9m after the runner stopped, within the 10m limit")
	}
	// 11 minutes after last seen: stalled.
	if stalled, _ := tr.Observe(alive.Add(11*time.Minute), false, time.Time{}); !stalled {
		t.Fatal("want stalled 11m after the runner stopped")
	}
}

func TestExitSilence_LimitZeroDisables(t *testing.T) {
	tr := NewExitSilenceTracker(esClockZero, 0)
	now := esClockZero.Add(24 * time.Hour)
	stalled, reason := tr.Observe(now, false, time.Time{})
	if stalled || reason != "" {
		t.Fatalf("limit 0 must disable detection, got stalled=%v reason=%q", stalled, reason)
	}
}

func TestExitSilenceError_WrapsStalled(t *testing.T) {
	reason := "pi print run exited without a completion or verdict (silent for 10m9s after the runner stopped, limit 10m0s)"
	err := ExitSilenceError(reason)
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("errors.Is(err, ErrWorkerStalled) = false: %v", err)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("error message %q does not contain reason %q", err.Error(), reason)
	}
}
