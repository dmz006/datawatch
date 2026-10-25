package autonomous

import (
	"testing"
	"time"
)

// Idle clock of the nudge tracker. A cosmetic pane redraw touches the worker's
// log on every poll, so the log age stays near zero while the prompt sits idle.
// The idle clock must still run from the start of the idle run. Injected clock:
// no tmux, no sleeps.

// TestWorkerNudgeIdleClock_RedrawEveryTickDoesNotHoldTheClock proves that a log
// touched on every poll does not keep the idle clock below the nudge window. The
// nudge comes at the window, measured from the start of the idle run.
func TestWorkerNudgeIdleClock_RedrawEveryTickDoesNotHoldTheClock(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	redraw := nudgeObs(2 * time.Second) // log written 2s ago on every poll

	sentAt := time.Duration(-1)
	for at := time.Duration(0); at <= 9*time.Minute; at += 3 * time.Second {
		if nudgeObserve(tr, at, redraw) == WorkerNudgeSend {
			sentAt = at
			break
		}
	}
	if sentAt != nudgeTestWindow {
		t.Fatalf("nudge sent at %s with a redraw on every poll, want at the %s window", sentAt, nudgeTestWindow)
	}
	if tr.idleAtSend != nudgeTestWindow {
		t.Fatalf("idle time recorded for the nudge = %s, want the %s idle run", tr.idleAtSend, nudgeTestWindow)
	}
}
