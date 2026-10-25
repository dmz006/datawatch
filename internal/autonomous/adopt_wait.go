package autonomous

import (
	"context"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// adoptedWaitPollInterval is how often WaitAdopted re-reads the session when
// no Pause hook is wired.
const adoptedWaitPollInterval = 3 * time.Second

// AdoptedWaitPorts are the reads and actions WaitAdopted uses. The daemon
// supplies them from its session manager; tests supply fakes and a fake clock.
type AdoptedWaitPorts struct {
	NudgeAfter time.Duration // idle window before the one completion nudge
	StallAfter time.Duration // silence window after the nudge before a stall

	// Status reports the session's state and backend family. ok=false means the
	// session no longer exists.
	Status func(sessionID string) (state session.State, family string, ok bool)
	// Completed reports whether the session's output carries the completion marker.
	Completed func(sessionID string) bool
	// Nudge returns the idle-nudge hooks for the session.
	Nudge func(sessionID string) WorkerNudgeHooks

	// Now is the clock; nil uses time.Now.
	Now func() time.Time
	// Pause blocks between polls and returns ctx.Err() when the context ends
	// first; nil waits adoptedWaitPollInterval.
	Pause func(ctx context.Context) error
}

// WaitAdopted blocks until an adopted worker finishes. It polls the session,
// returns nil when it completes, and returns an error when it fails or is
// killed. While the worker is idle it runs the same nudge decision as a fresh
// worker (StepWorkerNudge), seeded from w.NudgedAt. A stall returns an error
// wrapping ErrWorkerStalled. Output after the nudge ends nudge tracking but not
// the wait: the worker is still running until it completes.
func WaitAdopted(ctx context.Context, w AdoptedWait, p AdoptedWaitPorts) error {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	pause := p.Pause
	if pause == nil {
		pause = func(ctx context.Context) error {
			tick := time.NewTimer(adoptedWaitPollInterval)
			defer tick.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-tick.C:
				return nil
			}
		}
	}
	tr := NewWorkerNudgeTracker(p.NudgeAfter, p.StallAfter)
	tr.SeedNudgedAt(w.NudgedAt)
	label := w.PRDID + "/" + w.TaskID
	// finish closes nudge tracking when the wait ends on completion.
	finish := func() {
		if tr.Nudged() {
			CloseNudgeWait(tr, p.Nudge(w.SessionID))
		}
	}
	for {
		state, family, ok := p.Status(w.SessionID)
		if !ok {
			return fmt.Errorf("adopted session %s no longer exists", w.SessionID)
		}
		switch state {
		case session.StateComplete:
			finish()
			return nil
		case session.StateFailed, session.StateKilled:
			return fmt.Errorf("adopted session %s ended %s", w.SessionID, state)
		}
		if p.Completed(w.SessionID) {
			finish()
			return nil
		}
		if err := StepWorkerNudge(tr, now(), family, w.SessionID, label, p.Nudge(w.SessionID)); err != nil {
			return err
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
}
