package session

import (
	"context"
	"time"
)

// finaliseIdleWait is how long the finaliser loop waits before it re-reads the
// interval when scheduled runs are switched off (an interval of zero or less).
// It is a variable so tests can shorten it.
var finaliseIdleWait = time.Minute

// StartFinaliser starts the background finaliser. It runs RunFinalise once
// straight away (the start-up run) and then repeats it on a schedule. The
// interval is read fresh on every cycle, so a changed setting applies without
// a restart. An interval of zero or less means no scheduled run: the loop
// waits a short while, reads the interval again and runs nothing. The
// goroutine ends when ctx is cancelled.
func (m *Manager) StartFinaliser(ctx context.Context, interval func() time.Duration) {
	go m.finaliseLoop(ctx, interval)
}

// finaliseLoop is the body of the finaliser goroutine. It returns when ctx is
// cancelled, and stops its timer on the way out.
func (m *Manager) finaliseLoop(ctx context.Context, interval func() time.Duration) {
	if ctx.Err() != nil {
		return
	}
	m.RunFinalise(time.Now())

	for {
		wait := interval()
		scheduled := wait > 0
		if !scheduled {
			wait = finaliseIdleWait
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if scheduled && ctx.Err() == nil {
			m.RunFinalise(time.Now())
		}
	}
}
