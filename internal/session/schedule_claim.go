// One delivery for each fire. The daemon's 10 s pass, the session manager's
// 30 s pass and the input-prompt handler can all see the same due command, so
// each takes a claim on the fire before it types anything. The claim is saved
// with the schedule, so a restart in the middle of a send does not type it
// again.

package session

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrFireTaken is returned by FireScheduled when there is no fire left to
	// type: it is being typed or was already recorded, or the schedule is no
	// longer pending or due.
	ErrFireTaken = errors.New("not typed: this fire is already being typed or recorded, or the schedule is no longer due")
	// ErrFireDeferred is returned by FireScheduled when the fire cannot be
	// taken now. The schedule stays due.
	ErrFireDeferred = errors.New("not typed now: the schedule stays due")
)

// interruptedFireReason is recorded on a schedule whose send a restart cut off.
const interruptedFireReason = "the daemon stopped while this was being typed; it may or may not have arrived, so it is not typed again"

// claimFire takes the due fire of schedule id, to be typed into the session
// fullID, and saves the claim. Only one schedule at a time is typed into a
// session: two lines typed at once can join into a different command. It
// returns a copy of the entry as claimed, and the function that gives the
// claim and the session back once the outcome is recorded.
func (s *ScheduleStore) claimFire(id, fullID string, now time.Time) (ScheduledCommand, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID != id {
			continue
		}
		// A chained command is due when the one before it is typed, whatever
		// its own time says.
		if sc.State != SchedPending || sc.FiringSince != nil || (sc.RunAfterID == "" && sc.RunAt.After(now)) {
			break
		}
		if s.typing[fullID] {
			return ScheduledCommand{}, nil, fmt.Errorf("%w: another schedule is being typed into %s", ErrFireDeferred, fullID)
		}
		mark := &now
		sc.FiringSince = mark
		if err := s.save(); err != nil {
			sc.FiringSince = nil
			return ScheduledCommand{}, nil, fmt.Errorf("%w: the claim could not be saved: %w", ErrFireDeferred, err)
		}
		if s.typing == nil {
			s.typing = map[string]bool{}
		}
		s.typing[fullID] = true
		return *sc, func() { s.releaseFire(id, fullID, mark) }, nil
	}
	return ScheduledCommand{}, nil, ErrFireTaken
}

// releaseFire frees the session, and clears the claim made at mark when
// recording the outcome has not already cleared it.
func (s *ScheduleStore) releaseFire(id, fullID string, mark *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.typing, fullID)
	for _, sc := range s.entries {
		if sc.ID == id && sc.FiringSince == mark {
			sc.FiringSince = nil
			if err := s.save(); err != nil {
				fmt.Printf("[schedule] [%s] save the released claim: %v\n", id, err)
			}
		}
	}
}

// SettleInterruptedFires closes the fires that were claimed and never recorded:
// the daemon stopped while typing them. Whether the text arrived is unknown, so
// none is typed again: a recurring schedule moves to its next fire, any other
// becomes failed. It returns copies of the pending schedules it settled. Only
// the daemon calls it, at start, before anything can type.
func (s *ScheduleStore) SettleInterruptedFires(now time.Time) ([]ScheduledCommand, error) {
	var chain []ScheduledCommand
	defer func() { s.reportSettled(chain) }() // runs after the unlock
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ScheduledCommand
	changed := false
	for _, sc := range s.entries {
		if sc.FiringSince == nil {
			continue
		}
		at := *sc.FiringSince
		sc.FiringSince, changed = nil, true
		if sc.State != SchedPending {
			continue
		}
		result := "failed"
		if next, recurring := sc.nextFire(now); recurring {
			result = "skipped"
			if !sc.RecurUntil.IsZero() && next.After(sc.RecurUntil) {
				sc.State, sc.DoneAt = SchedDone, now
			} else {
				sc.RunAt = next
			}
		} else {
			sc.State, sc.DoneAt = SchedFailed, now
		}
		sc.LastFireAt, sc.LastFireResult, sc.LastError = at, result, interruptedFireReason
		sc.appendFire(ScheduleFire{At: at, Result: result, Error: interruptedFireReason})
		out = append(out, *sc)
		if sc.State == SchedFailed {
			chain = append(chain, s.failChainLocked(sc.ID, now)...)
		}
	}
	if !changed {
		return nil, nil
	}
	return out, s.save()
}

// endInterruptedNote clears the note a restart left once a later fire is
// delivered. Callers hold the store lock.
func (sc *ScheduledCommand) endInterruptedNote() {
	if sc.LastError == interruptedFireReason {
		sc.LastError, sc.LastFireResult = "", ""
	}
}

func (m *Manager) settleInterruptedFires(now time.Time) {
	settled, err := m.schedStore.SettleInterruptedFires(now)
	if err != nil {
		fmt.Printf("[schedule] settle interrupted fires: %v\n", err)
	}
	m.mu.Lock()
	hook := m.schedAlertHook
	m.mu.Unlock()
	for _, sc := range settled {
		fmt.Printf("[schedule] [%s] %s\n", sc.ID, interruptedFireReason)
		if hook != nil {
			hook(ScheduleAlert{Schedule: sc, Interrupted: true})
		}
	}
}

// FireChained types into fullID the commands chained after doneID, which has
// just been typed there. A chained command has no pass of its own. One that
// loses its session, or cannot be taken now, becomes due on its own once the
// command it followed is finished; one that is refused or fails is marked
// failed.
func (m *Manager) FireChained(store *ScheduleStore, fullID, doneID string) {
	for _, next := range store.AfterDone(doneID) {
		sent, err := m.FireScheduled(store, fullID, next)
		if sent || errors.Is(err, ErrFireTaken) {
			continue
		}
		fmt.Printf("[scheduler] failed to send chained input [%s]: %v\n", next.ID, err)
		switch {
		case errors.Is(err, ErrScheduleTargetGone):
			_ = store.ReleaseChained(next.ID, time.Now())
		case errors.Is(err, ErrFireDeferred):
			store.deferChained(next.ID, fullID, time.Now())
		case errors.Is(err, ErrPromptOnScreen):
			_ = store.MarkDone(next.ID, true)
		}
	}
}

// deferChained releases a chained command that could not be taken, keeping the
// session it was about to be typed into: one created by id is typed there.
func (s *ScheduleStore) deferChained(id, fullID string, now time.Time) {
	s.mu.Lock()
	for _, sc := range s.entries {
		if sc.ID == id && sc.State == SchedPending && !sc.NameTargeted() {
			sc.SessionID = fullID
		}
	}
	s.mu.Unlock()
	if err := s.ReleaseChained(id, now); err != nil {
		fmt.Printf("[schedule] [%s] release the chained command: %v\n", id, err)
	}
}
