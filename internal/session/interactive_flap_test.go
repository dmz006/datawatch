// An interactive claude session that reports a finished piece of work stays a
// live session: it waits for the next instruction. It must never change to
// complete and back.
package session

import (
	"testing"
	"time"
)

const flapTask = "Implement the made-up widget and report when it is done."

// A healthy session at its input box after finishing the task: the completion
// line is among the last lines on screen.
const flapDonePane = `❯ Implement the made-up widget and report when it is done.

● The widget is written.

  DATAWATCH_COMPLETE: wrote the made-up widget
────────────────────────────────────────
❯ 
────────────────────────────────────────`

// The same later in the session, with the task scrolled off the screen.
const flapScrolledPane = `● The widget is written.

  DATAWATCH_COMPLETE: wrote the made-up widget
────────────────────────────────────────
❯ 
────────────────────────────────────────`

// A follow-up turn at work while the earlier completion line is still shown.
const flapWorkingPane = `  DATAWATCH_COMPLETE: wrote the made-up widget
❯ now the tests
✻ Working… (3s · esc to interrupt)
❯ `

func newFlapSession(t *testing.T, pane string, deliveredAgo time.Duration) *launch {
	t.Helper()
	l := newLaunch(t, func(s *Session) {
		s.OneShot = false
		s.Task = flapTask
		at := time.Now().Add(-deliveredAgo)
		s.TaskDeliveredAt, s.TaskVerifiedAt = &at, &at
		s.DaemonDeliversTask = true
	})
	l.screen(pane, paneAgent)
	return l
}

// The pane monitor and the periodic pass that brings a finished session with a
// live terminal back, in turn, as the daemon runs them.
func TestInteractiveSessionDoesNotFlapAfterDelivery(t *testing.T) {
	for name, c := range map[string]struct {
		pane string
		ago  time.Duration
	}{
		"minutes after delivery, task on screen":  {flapDonePane, time.Minute},
		"long after delivery, task scrolled away": {flapScrolledPane, 10 * time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := newFlapSession(t, c.pane, c.ago)
			for i := 0; i < 2; i++ {
				l.monitor(2500 * time.Millisecond)
				l.reconcile()
			}
			got := l.seen.list()
			if l.seen.count(StateComplete) != 0 {
				t.Errorf("a healthy interactive session changed to complete: %v", got)
			}
			if s := l.snap(); s.State != StateWaitingInput {
				t.Errorf("state = %s, want waiting_input; transitions %v", s.State, got)
			}
			if len(got) > 1 {
				t.Errorf("transitions %v, want one change to waiting_input", got)
			}
		})
	}
}

// The completion line staying on screen does not call a working session idle.
func TestInteractiveSessionAtWorkStaysRunning(t *testing.T) {
	l := newFlapSession(t, flapWorkingPane, 10*time.Minute)
	l.monitor(4500 * time.Millisecond)
	if s := l.snap(); s.State != StateRunning {
		t.Errorf("state = %s, want running; transitions %v", s.State, l.seen.list())
	}
}
