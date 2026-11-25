// Operator pause admission: a new start of work waits, or is refused, while
// the operator has paused it (globally or for the Automaton's project). The
// manager only ever holds the read side of the pause service (pause.Gate), so
// nothing in the autonomous package can clear a pause. Work already running
// finishes: only new Automaton starts, restarts and new worker admissions are
// held, never a task that holds a lease, its verification or its judges.

package autonomous

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dmz006/datawatch/internal/pause"
)

// defaultPauseWaitPoll is how often a held task re-checks the pause.
const defaultPauseWaitPoll = 5 * time.Second

// SetPauseGate wires the operator pause service. Nil (the default) leaves
// every start as it was.
func (m *Manager) SetPauseGate(g pause.Gate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauseGate = g
}

// SetPauseWaitPoll sets how often a task held by a pause re-checks it; zero
// or negative restores the default.
func (m *Manager) SetPauseWaitPoll(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pausePoll = d
}

func (m *Manager) pauseWaitPoll() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pausePoll > 0 {
		return m.pausePoll
	}
	return defaultPauseWaitPoll
}

// pauseBlock reports whether starting new work in dir is paused.
func (m *Manager) pauseBlock(dir string) (pause.Block, bool) {
	m.mu.Lock()
	g := m.pauseGate
	m.mu.Unlock()
	if g == nil {
		return pause.Block{}, false
	}
	return g.Blocked(dir)
}

// refusePaused returns an error when the Automaton's project is paused, and
// nil otherwise. Every path that starts or restarts an Automaton calls it
// next to refuseHeld: a paused project waits by operator decision, so no start
// path (run, lane fill, boot resume) launches it until the operator resumes.
// An Automaton already running is not refused: its executor is the thing that
// adopts and finishes the work in flight (a session that survived a restart),
// and every new worker admission it makes waits on the pause (see
// waitOperatorPause), so nothing new starts either way. It reads the record
// under the store lock, so call it with no store lock held.
func (m *Manager) refusePaused(prd *PRD) error {
	if prd == nil {
		return nil
	}
	var dir string
	var running bool
	m.store.WithLock(func() {
		dir = prd.ProjectDir
		running = NormalizePRDStatus(prd.Status) == PRDRunning
	})
	if running {
		return nil
	}
	if b, blocked := m.pauseBlock(dir); blocked {
		return fmt.Errorf("automaton %q: %s", prd.ID, b.Message())
	}
	return nil
}

// pausedReasonFor is the lane fill refusal text for a project, or "".
func (m *Manager) pausedReasonFor(dir string) string {
	if b, blocked := m.pauseBlock(dir); blocked {
		return b.Message()
	}
	return ""
}

// waitOperatorPause blocks a new worker admission while the task's project is
// paused. The task shows as waiting with the pause reason, the wait has no
// timeout, and it never counts as a retry or a failure, exactly like the
// capacity wait. It returns errCancelledWhileWaiting when the task is
// cancelled and the context error when ctx ends.
func (m *Manager) waitOperatorPause(ctx context.Context, prd *PRD, t *Task) error {
	return m.waitOperatorPauseIn(ctx, t, m.resolveTaskProjectDir(prd, t))
}

// waitOperatorPauseIn is waitOperatorPause for work in dir.
func (m *Manager) waitOperatorPauseIn(ctx context.Context, t *Task, dir string) error {
	b, blocked := m.pauseBlock(dir)
	if !blocked {
		return nil
	}
	poll := m.pauseWaitPoll()
	clock := m.spawnWaitClock()
	shown := ""
	for blocked {
		if m.taskCancelled(t.ID) {
			return errCancelledWhileWaiting
		}
		if reason := b.Message(); reason != shown {
			m.setWaiting(t, reason)
			shown = reason
		}
		select {
		case <-ctx.Done():
			if m.taskCancelled(t.ID) {
				return errCancelledWhileWaiting
			}
			return ctx.Err()
		case <-clock.After(poll):
		}
		b, blocked = m.pauseBlock(dir)
	}
	m.clearWaiting(t)
	return nil
}

// waitDirUnpaused blocks while dir is paused, for engine work that has no task
// to show the wait on. It returns the context error when ctx ends.
func (m *Manager) waitDirUnpaused(ctx context.Context, dir string) error {
	b, blocked := m.pauseBlock(dir)
	if !blocked {
		return nil
	}
	log.Printf("[autonomous] engine work in %q waits: %s", dir, b.Message())
	poll := m.pauseWaitPoll()
	clock := m.spawnWaitClock()
	for blocked {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-clock.After(poll):
		}
		_, blocked = m.pauseBlock(dir)
	}
	return nil
}
