// Where a command schedule is typed. One created with a session id belongs to
// that session and ends with it. One created with a session name belongs to
// the name: it outlives the session it first resolved to, waits while no live
// session has the name, and is typed into the next one that does.

package session

import (
	"errors"
	"fmt"
	"time"
)

// RearmStateWaitingSession marks a name schedule that missed its turn because
// no live session has its name.
const RearmStateWaitingSession = "waiting_session"

// ErrScheduleTargetGone is returned by FireScheduled when a name schedule
// could not be typed because its session can no longer take input. The
// schedule stays pending.
var ErrScheduleTargetGone = errors.New("the target session can no longer take input; the schedule waits for a live session with that name")

// NameTargeted reports whether sc is a command schedule created with a
// session name.
func (sc *ScheduledCommand) NameTargeted() bool {
	return sc.SessionName != "" && (sc.Type == "" || sc.Type == SchedTypeCommand)
}

// takesInput reports whether SendInput accepts a session in state s.
func takesInput(s State) bool {
	return s == StateRunning || s == StateWaitingInput || s == StateRateLimited
}

// ScheduleTarget returns the session a command schedule is typed into now.
// By id that is the session with that id, whatever its state. By name it is
// the stored session while it can take input, else the live session with the
// name: running or waiting before rate limited, then the oldest.
func (m *Manager) ScheduleTarget(sc *ScheduledCommand) (*Session, bool) {
	if !sc.NameTargeted() {
		return m.GetSession(sc.SessionID)
	}
	if sc.SessionID != "" {
		if sess, ok := m.GetSession(sc.SessionID); ok && takesInput(m.store.Snapshot(sess).State) {
			return sess, true
		}
	}
	var best *Session
	var bestSnap Session
	for _, s := range m.store.List() {
		snap := m.store.Snapshot(s)
		if snap.Name != sc.SessionName || !takesInput(snap.State) {
			continue
		}
		if best != nil {
			limited, bestLimited := snap.State == StateRateLimited, bestSnap.State == StateRateLimited
			if limited != bestLimited {
				if limited {
					continue
				}
			} else if !snap.CreatedAt.Before(bestSnap.CreatedAt) {
				continue
			}
		}
		best, bestSnap = s, snap
	}
	return best, best != nil
}

// targetGone reports whether the session can no longer be typed into: its
// record is gone or closed, or its terminal no longer exists.
func (m *Manager) targetGone(fullID string) bool {
	sess, ok := m.GetSession(fullID)
	if !ok {
		return true
	}
	snap := m.store.Snapshot(sess)
	return !takesInput(snap.State) || !m.tmux.SessionExists(snap.TmuxSession)
}

// WaitingInputSchedules returns the on-input commands to type into sess, which
// has asked for input: those queued for its id, and those queued for its name
// when it is the session that name resolves to. A name schedule whose own
// session is still live stays with that session.
func (m *Manager) WaitingInputSchedules(store *ScheduleStore, sess *Session) []*ScheduledCommand {
	out := store.WaitingInputPending(sess.FullID)
	if len(out) == 0 {
		out = store.WaitingInputPending(sess.ID) // try short ID
	}
	name := m.store.Snapshot(sess).Name
	for _, sc := range store.NamedOnInput() {
		if sc.SessionName != name || sc.SessionID == sess.FullID || sc.SessionID == sess.ID {
			continue
		}
		if t, ok := m.ScheduleTarget(sc); ok && t.FullID == sess.FullID {
			out = append(out, sc)
		}
	}
	return out
}

// HoldNameSchedule keeps a name schedule that has no live session to go to. A
// recurring one skips the fire it missed. The operator is alerted once for
// each wait; a delivery ends the wait.
func (m *Manager) HoldNameSchedule(store *ScheduleStore, sc *ScheduledCommand, now time.Time) {
	m.mu.Lock()
	hook := m.schedAlertHook
	m.mu.Unlock()
	// With no hook yet (daemon still starting) the alert is still owed.
	held, alert, err := store.HoldForName(sc.ID, now, hook != nil)
	if err != nil {
		fmt.Printf("[schedule] hold [%s] for session name %q: %v\n", sc.ID, sc.SessionName, err)
		return
	}
	if alert {
		fmt.Printf("[schedule] [%s] waits: no live session named %q\n", sc.ID, sc.SessionName)
		hook(ScheduleAlert{Schedule: held, NoSession: true})
	}
}

// holdUnreachableOnInput holds the on-input name schedules that have no live
// session. Timed ones are held when they come due.
func (m *Manager) holdUnreachableOnInput(now time.Time) {
	for _, sc := range m.schedStore.NamedOnInput() {
		if _, ok := m.ScheduleTarget(sc); !ok {
			m.HoldNameSchedule(m.schedStore, sc, now)
		}
	}
}

// NamedOnInput returns the pending name schedules that are typed when their
// session asks for input.
func (s *ScheduleStore) NamedOnInput() []*ScheduledCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ScheduledCommand
	for _, sc := range s.entries {
		if sc.State == SchedPending && sc.NameTargeted() && sc.RunAt.IsZero() && sc.RunAfterID == "" {
			out = append(out, sc)
		}
	}
	return out
}

// HoldForName records that a pending name schedule has no live session. A
// recurring one that is due moves to its next fire, or ends when that is past
// its deadline; a one-time one stays due. alert is true the first time in a
// wait that canAlert is set; held is then a copy of the entry.
func (s *ScheduleStore) HoldForName(id string, now time.Time, canAlert bool) (held ScheduledCommand, alert bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID != id || sc.State != SchedPending {
			continue
		}
		changed := false
		reason := fmt.Sprintf("no live session named %q", sc.SessionName)
		if sc.RearmState != RearmStateWaitingSession {
			sc.RearmState, sc.LastError, changed = RearmStateWaitingSession, reason, true
		}
		if next, recurring := sc.nextFire(now); recurring && !sc.RunAt.IsZero() && !sc.RunAt.After(now) {
			sc.LastFireAt, sc.LastFireResult, sc.LastError = now, "skipped", reason
			sc.appendFire(ScheduleFire{At: now, Result: "skipped", Error: reason})
			if !sc.RecurUntil.IsZero() && next.After(sc.RecurUntil) {
				sc.State, sc.DoneAt = SchedDone, now
			} else {
				sc.RunAt = next
			}
			changed = true
		}
		if canAlert && !sc.NameWaitAlerted {
			sc.NameWaitAlerted, alert, changed = true, true, true
		}
		if changed {
			err = s.save()
		}
		return *sc, alert, err
	}
	return ScheduledCommand{}, false, nil
}

// nextFire is the fire after now of a recurring schedule.
func (sc *ScheduledCommand) nextFire(now time.Time) (time.Time, bool) {
	if sc.RecurEverySeconds > 0 {
		return now.Add(time.Duration(sc.RecurEverySeconds) * time.Second), true
	}
	if sc.CronExpr != "" {
		if next, err := CronNext(sc.CronExpr, now); err == nil {
			return next, true
		}
	}
	return time.Time{}, false
}

// endNameWait clears what a wait for a session left on the entry. Callers
// hold the store lock.
func (sc *ScheduledCommand) endNameWait() {
	sc.NameWaitAlerted = false
	if sc.RearmState == RearmStateWaitingSession {
		sc.RearmState, sc.LastError, sc.LastFireResult = "", "", ""
	}
}

// ReleaseChained makes a chained command due on its own when the command it
// followed is finished, so a later pass can still type it. While that command
// is still pending the chain is kept, and its next fire types this one.
func (s *ScheduleStore) ReleaseChained(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID != id || sc.State != SchedPending || sc.RunAfterID == "" {
			continue
		}
		for _, parent := range s.entries {
			if parent.ID == sc.RunAfterID && parent.State == SchedPending {
				return nil
			}
		}
		sc.RunAfterID, sc.RunAt = "", now
		return s.save()
	}
	return nil
}
