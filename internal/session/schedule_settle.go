// A schedule that can no longer run ends and says so. It is never left pending
// for ever, never dropped without an alert, and never typed late.

package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Why datawatch ended a schedule without delivering it.
const (
	settledSessionEndedKill = "cancelled: its session was ended"
	settledNoSession        = "its session no longer exists"
	settledFilterRefused    = "cancelled: the output filter that queued it may no longer type"
	settledNotDelivered     = "it could not be delivered"
	settledOrphanedChain    = "the schedule it follows finished, but this one was never typed; it is not typed late"
)

// deadTargetGrace is how long a session must have been finished before its
// schedules are given up, and a chained command orphaned before it is failed.
// A session that has only just ended can still be brought back.
const deadTargetGrace = 2 * time.Minute

// maxSettleAlerts is how many schedules of one pass get an alert each; more
// than that are reported together.
const maxSettleAlerts = 10

// settleLocked ends sc as state for reason and returns it. A failed one takes
// with it every pending command chained behind it, which can now never be
// typed; the chain behind a cancelled one is failed by the next timer pass
// (settleUnreachable). Callers hold s.mu and report the result with
// reportSettled after they unlock.
func (s *ScheduleStore) settleLocked(sc *ScheduledCommand, state, reason string, now time.Time) []ScheduledCommand {
	sc.State, sc.DoneAt, sc.LastError = state, now, reason
	if state == SchedFailed {
		sc.LastFireResult = "failed"
	}
	out := []ScheduledCommand{*sc}
	if state == SchedFailed {
		out = append(out, s.failChainLocked(sc.ID, now)...)
	}
	return out
}

// failChainLocked fails the pending commands chained after parentID.
func (s *ScheduleStore) failChainLocked(parentID string, now time.Time) []ScheduledCommand {
	var out []ScheduledCommand
	reason := fmt.Sprintf("the schedule it follows (%s) did not run", parentID)
	for _, sc := range s.entries {
		if sc.State == SchedPending && sc.RunAfterID == parentID && sc.FiringSince == nil {
			out = append(out, s.settleLocked(sc, SchedFailed, reason, now)...)
		}
	}
	return out
}

// reportSettled hands the schedules datawatch ended to the alert hook. Callers
// must not hold s.mu.
func (s *ScheduleStore) reportSettled(settled []ScheduledCommand) {
	if len(settled) == 0 {
		return
	}
	s.mu.Lock()
	hook := s.settleHook
	s.mu.Unlock()
	if hook != nil {
		hook(settled)
	}
}

// deliverSettled raises one alert for each schedule datawatch ended, or one for
// all of them when there are many.
func (m *Manager) deliverSettled(settled []ScheduledCommand) {
	for _, sc := range settled {
		fmt.Printf("[schedule] [%s] %s: %s\n", sc.ID, sc.State, sc.LastError)
	}
	m.mu.Lock()
	hook := m.schedAlertHook
	m.mu.Unlock()
	if hook == nil {
		return
	}
	if len(settled) <= maxSettleAlerts {
		for _, sc := range settled {
			hook(ScheduleAlert{Schedule: sc, Settled: sc.LastError})
		}
		return
	}
	lines := make([]string, 0, len(settled))
	for _, sc := range settled {
		lines = append(lines, fmt.Sprintf("%s (%s): %s", sc.ID, sc.State, sc.LastError))
	}
	hook(ScheduleAlert{Schedule: settled[0], Settled: strings.Join(lines, "\n"), Count: len(settled)})
}

// CancelBySystem cancels a pending schedule on datawatch's own decision, which
// the operator is told about.
func (s *ScheduleStore) CancelBySystem(id, reason string) {
	var out []ScheduledCommand
	s.mu.Lock()
	for _, sc := range s.entries {
		if sc.ID == id && sc.State == SchedPending {
			out = s.settleLocked(sc, SchedCancelled, reason, time.Now())
			if err := s.save(); err != nil {
				fmt.Printf("[schedule] [%s] save: %v\n", id, err)
			}
			break
		}
	}
	s.mu.Unlock()
	s.reportSettled(out)
}

// failedSend is what recordSendFailure did.
type failedSend struct {
	rearmed bool
	copy    ScheduledCommand
	settled []ScheduledCommand
}

// recordSendFailure records a command that could not be typed. A recurring
// schedule for a session id whose session can still take input moves to its
// next fire: one send error does not end it. Any other is failed, a recurring
// schedule for a session name included.
func (s *ScheduleStore) recordSendFailure(id string, cause error, targetGone bool, now time.Time) (failedSend, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.entries {
		if sc.ID != id {
			continue
		}
		if sc.State != SchedPending {
			return failedSend{}, nil
		}
		sc.FiringSince = nil
		reason := truncateReason(cause)
		sc.LastFireAt = now
		sc.appendFire(ScheduleFire{At: now, Result: "failed", Error: reason})
		next, recurring := sc.nextFire(now)
		if recurring && !targetGone && !sc.NameTargeted() && !sc.RunAt.IsZero() && (sc.RecurUntil.IsZero() || !next.After(sc.RecurUntil)) {
			sc.RunAt, sc.LastError, sc.LastFireResult = next, reason, "failed"
			sc.ConsecutiveFailures++
			return failedSend{rearmed: true, copy: *sc}, s.save()
		}
		why := settledNotDelivered
		if reason != "" {
			why += ": " + reason
		}
		out := failedSend{settled: s.settleLocked(sc, SchedFailed, why, now)}
		return out, s.save()
	}
	return failedSend{}, fmt.Errorf("scheduled command %q not found", id)
}

// sendFailed records a command that could not be typed into fullID and raises
// the alert it calls for.
func (m *Manager) sendFailed(store *ScheduleStore, fullID, id string, cause error) {
	gone := true
	if sess, ok := m.GetSession(fullID); ok {
		gone = m.store.Snapshot(sess).State.IsTerminal()
	}
	out, err := store.recordSendFailure(id, cause, gone, time.Now())
	if err != nil {
		fmt.Printf("[schedule] [%s] record the failed send: %v\n", id, err)
	}
	if out.rearmed {
		m.mu.Lock()
		hook := m.schedAlertHook
		m.mu.Unlock()
		// Alert on the first failure in a row, then once per 24.
		if hook != nil && scheduleAlertDue(StartFailure{Consecutive: out.copy.ConsecutiveFailures}) {
			hook(ScheduleAlert{Schedule: out.copy, SendFailed: true, Err: cause})
		}
		return
	}
	store.reportSettled(out.settled)
}

// orphanReasonLocked says why the command sc is chained to will never start
// it, or "". Callers hold s.mu.
func (s *ScheduleStore) orphanReasonLocked(sc *ScheduledCommand, now time.Time) string {
	for _, p := range s.entries {
		if p.ID != sc.RunAfterID {
			continue
		}
		switch p.State {
		case SchedPending:
			return ""
		case SchedDone:
			if now.Sub(p.DoneAt) < deadTargetGrace {
				return "" // it is being typed now
			}
			return settledOrphanedChain
		}
		return fmt.Sprintf("the schedule it follows (%s) did not run", p.ID)
	}
	return fmt.Sprintf("the schedule it follows (%s) no longer exists", sc.RunAfterID)
}

// pendingSessionIDs are the sessions pending schedules are addressed to.
func (s *ScheduleStore) pendingSessionIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var ids []string
	for _, sc := range s.entries {
		if sc.State == SchedPending && sc.SessionID != "" && !seen[sc.SessionID] {
			seen[sc.SessionID] = true
			ids = append(ids, sc.SessionID)
		}
	}
	return ids
}

// settleUnreachable fails the pending command schedules that can never be
// typed: those of a session id whose session is gone or has been finished for
// a while, and chains nothing will start. dead says why a session cannot be
// typed into again, or "".
func (s *ScheduleStore) settleUnreachable(now time.Time, dead func(sessionID string) string) {
	// The session lookups are made without the store lock.
	reasons := map[string]string{}
	for _, id := range s.pendingSessionIDs() {
		reasons[id] = dead(id)
	}
	var out []ScheduledCommand
	s.mu.Lock()
	for _, sc := range s.entries {
		if sc.State != SchedPending || sc.FiringSince != nil || (sc.Type != "" && sc.Type != SchedTypeCommand) {
			continue
		}
		why := ""
		if !sc.NameTargeted() && sc.SessionID != "" {
			why = reasons[sc.SessionID]
		}
		if why == "" && sc.RunAfterID != "" {
			why = s.orphanReasonLocked(sc, now)
		}
		if why != "" {
			out = append(out, s.settleLocked(sc, SchedFailed, why, now)...)
		}
	}
	if len(out) > 0 {
		if err := s.save(); err != nil {
			fmt.Printf("[schedule] save the settled schedules: %v\n", err)
		}
	}
	s.mu.Unlock()
	s.reportSettled(out)
}

// deadSession says why the session cannot be typed into again, or "". One that
// has only just finished is not given up yet.
func (m *Manager) deadSession(sessionID string, now time.Time) string {
	sess, ok := m.GetSession(sessionID)
	if !ok {
		return settledNoSession
	}
	snap := m.store.Snapshot(sess)
	if !snap.State.IsTerminal() {
		return ""
	}
	ended := snap.UpdatedAt
	if snap.EndedAt != nil {
		ended = *snap.EndedAt
	}
	if now.Sub(ended) < deadTargetGrace {
		return ""
	}
	return fmt.Sprintf("its session ended (%s)", snap.State)
}

// ── schedules that could never run are refused when they are made ───────────

// ErrScheduleRefused is returned when a new schedule could never be typed.
var ErrScheduleRefused = errors.New("schedule refused")

// SetTargetCheck sets the check a new command schedule for a session id must
// pass. With a check set, a chained schedule must also follow a pending one.
func (s *ScheduleStore) SetTargetCheck(fn func(sessionID string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targetCheck = fn
}

// checkNewLocked refuses a command schedule that could never be typed. check
// ran before the lock was taken.
func (s *ScheduleStore) checkNewLocked(runAfterID string) error {
	if s.targetCheck == nil || runAfterID == "" {
		return nil
	}
	for _, p := range s.entries {
		if p.ID == runAfterID && p.State == SchedPending {
			return nil
		}
	}
	return fmt.Errorf("%w: it follows schedule %q, which is not pending", ErrScheduleRefused, runAfterID)
}

// checkTarget runs the target check for a schedule addressed by session id
// alone; one with a session name waits for that name.
func (s *ScheduleStore) checkTarget(sessionID, sessionName string) error {
	s.mu.Lock()
	check := s.targetCheck
	s.mu.Unlock()
	if check == nil || sessionName != "" || sessionID == "" {
		return nil
	}
	return check(sessionID)
}

// EnableScheduleTargetCheck makes the schedule store refuse a schedule for a
// session id that has no session which can still be typed into. The daemon
// turns it on; every surface adds schedules through the store.
func (m *Manager) EnableScheduleTargetCheck() {
	if m.schedStore == nil {
		return
	}
	m.schedStore.SetTargetCheck(func(sessionID string) error {
		sess, ok := m.GetSession(sessionID)
		if !ok {
			return fmt.Errorf("%w: there is no session %q", ErrScheduleRefused, sessionID)
		}
		if state := m.store.Snapshot(sess).State; state.IsTerminal() {
			return fmt.Errorf("%w: session %q has ended (%s)", ErrScheduleRefused, sessionID, state)
		}
		return nil
	})
}

// ── the rate-limit resume ───────────────────────────────────────────────────

// SchedKindRateLimitResume marks the command that resumes a session when its
// rate limit resets.
const SchedKindRateLimitResume = "rate_limit_resume"

const rateLimitResumeMsg = "The rate limit has reset. Please read PAUSED.md in your working directory for context on what was in progress, then continue the task."

// isRateLimitResume also knows the resumes stored before schedules had a kind.
func (sc *ScheduledCommand) isRateLimitResume() bool {
	if sc.Kind == SchedKindRateLimitResume {
		return true
	}
	return sc.Kind == "" && sc.Command == rateLimitResumeMsg && !sc.RunAt.IsZero()
}

// ensureRateLimitResume makes sure the rate-limited session sessionID has one
// pending resume. fresh says the limit has just been detected: a pending resume
// then moves to the new reset time. Without it, at daemon start, none is made
// when one was cut off while being typed, or was typed or cancelled since the
// session last changed: it is never typed twice, and a cancelled one stays so.
func (s *ScheduleStore) ensureRateLimitResume(sessionID string, at, sessionChanged time.Time, fresh bool) (added bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last *ScheduledCommand
	for _, sc := range s.entries {
		if sc.SessionID != sessionID || !sc.isRateLimitResume() {
			continue
		}
		if sc.State == SchedPending {
			if fresh && sc.FiringSince == nil && !sc.RunAt.Equal(at) {
				sc.RunAt = at
				return false, s.save()
			}
			return false, nil
		}
		last = sc
	}
	if !fresh && last != nil {
		cutOff := last.State == SchedFailed && strings.Contains(last.LastError, interruptedFireReason)
		endedSince := (last.State == SchedDone || last.State == SchedCancelled) && last.DoneAt.After(sessionChanged)
		if cutOff || endedSince {
			return false, nil
		}
	}
	id, err := randomID()
	if err != nil {
		return false, err
	}
	s.entries = append(s.entries, &ScheduledCommand{
		ID: id, SessionID: sessionID, Command: rateLimitResumeMsg, Kind: SchedKindRateLimitResume,
		RunAt: at, State: SchedPending, CreatedAt: time.Now(),
	})
	return true, s.save()
}

// ensureResumeAtStart gives a session found rate limited at daemon start its
// resume. It does nothing until the schedule store is open.
func (m *Manager) ensureResumeAtStart(sess *Session) {
	if m.schedStore == nil {
		return
	}
	snap := m.store.Snapshot(sess)
	at := time.Now().Add(60 * time.Minute)
	if snap.RateLimitResetAt != nil && time.Until(*snap.RateLimitResetAt) > time.Minute {
		at = *snap.RateLimitResetAt
	}
	added, err := m.schedStore.ensureRateLimitResume(snap.FullID, at, snap.UpdatedAt, false)
	if err != nil {
		fmt.Printf("[warn] schedule rate-limit resume for %s: %v\n", snap.FullID, err)
	} else if added {
		fmt.Printf("[rate-limit] scheduled auto-resume for %s at %s\n", snap.ID, at.Format(time.RFC3339))
	}
}

// ensureResumesAtStart covers every session of this host that is rate limited.
// The daemon resumes sessions before it opens the schedule store, so the
// schedule timer calls this once the store is there.
func (m *Manager) ensureResumesAtStart() {
	for _, sess := range m.store.List() {
		if sess.Hostname == m.hostname && sess.TmuxSession != "" && m.store.Snapshot(sess).State == StateRateLimited {
			m.ensureResumeAtStart(sess)
		}
	}
}
