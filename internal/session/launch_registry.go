// Launch registry: tracks the Start calls whose LLM backend launch is in
// flight, so a daemon shutdown can drain them. A drain refuses new launches,
// waits a grace period for the running ones, then cancels and rolls back any
// still running. It acts on one session per launch through killPane only: it
// never stops a multiplexer server and never ends processes by name pattern.

package session

import (
	"context"
	"errors"
	"sort"
	"time"
)

// ErrDaemonDraining is returned by Start while the daemon drains its launches.
var ErrDaemonDraining = errors.New("daemon is shutting down; a new launch is refused")

const (
	// launchPollInterval is how often DrainLaunches checks the registry.
	launchPollInterval = 100 * time.Millisecond
	// launchSettleWait bounds the wait for cancelled launches to return.
	launchSettleWait = 2 * time.Second
)

// registerLaunch records a Start that is about to launch sess. It refuses the
// launch while draining. The check and the record share one lock, so a drain
// that starts after the check still sees this launch in the registry.
func (m *Manager) registerLaunch(sess *Session, cancel context.CancelFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.draining {
		return ErrDaemonDraining
	}
	if m.launching == nil {
		m.launching = make(map[string]context.CancelFunc)
		m.launchSessions = make(map[string]*Session)
	}
	m.launching[sess.FullID] = cancel
	m.launchSessions[sess.FullID] = sess
	return nil
}

// deregisterLaunch removes a launch once its Start has returned on any path.
func (m *Manager) deregisterLaunch(fullID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.launching, fullID)
	delete(m.launchSessions, fullID)
}

// launchesDraining reports whether DrainLaunches has begun.
func (m *Manager) launchesDraining() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.draining
}

// launchCount returns how many launches are registered.
func (m *Manager) launchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.launching)
}

// DrainLaunches refuses new launches and waits up to grace for the registered
// ones to finish. Launches still registered at the deadline are cancelled and
// rolled back: their panes are killed and their sessions marked killed and
// saved. It returns the full IDs of the rolled-back sessions, sorted, or nil
// when every launch finished inside the grace.
func (m *Manager) DrainLaunches(grace time.Duration) []string {
	m.mu.Lock()
	m.draining = true
	m.mu.Unlock()

	if m.waitForLaunches(grace) {
		return nil
	}

	type stuckLaunch struct {
		fullID string
		sess   *Session
	}
	var stuck []stuckLaunch
	m.mu.Lock()
	for fullID, cancel := range m.launching {
		cancel()
		stuck = append(stuck, stuckLaunch{fullID: fullID, sess: m.launchSessions[fullID]})
	}
	m.mu.Unlock()

	ids := make([]string, 0, len(stuck))
	for _, s := range stuck {
		ids = append(ids, s.fullID)
		m.killPane(s.fullID, s.sess.TmuxSession, "daemon drain: launch still running at the grace deadline")
		m.store.Mutate(func() {
			if !s.sess.State.IsTerminal() {
				s.sess.State = StateKilled
				s.sess.UpdatedAt = time.Now()
			}
		})
		_ = m.store.Save(s.sess)
	}
	sort.Strings(ids)

	m.waitForLaunches(launchSettleWait)
	return ids
}

// waitForLaunches polls the registry until it is empty or bound has elapsed.
// It reports whether the registry was empty when it returned.
func (m *Manager) waitForLaunches(bound time.Duration) bool {
	deadline := time.NewTimer(bound)
	defer deadline.Stop()
	tick := time.NewTicker(launchPollInterval)
	defer tick.Stop()
	for {
		if m.launchCount() == 0 {
			return true
		}
		select {
		case <-deadline.C:
			return m.launchCount() == 0
		case <-tick.C:
		}
	}
}
