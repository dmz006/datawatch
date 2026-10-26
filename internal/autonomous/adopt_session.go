// Boot-resume session adoption: decides what to do with an in_progress task
// whose worker session may have outlived the previous daemon.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// resumeAction is the boot-resume outcome for an in_progress task.
type resumeAction int

const (
	// resumeReset — no usable session: kill any leftover and re-queue the task.
	resumeReset resumeAction = iota
	// resumeAdopt — the session is still alive: keep it and wait for it.
	resumeAdopt
	// resumeFinished — the session ended while the daemon was down but its
	// recorded output carries the completion marker: go straight to verifying.
	resumeFinished
	// resumeStartFailed — the session is alive but its agent never started (a
	// bare shell left by a boot-time launch failure): end it and reset the task
	// with SessionStartFailureReason. No retry is counted.
	resumeStartFailed
)

// decideInProgressResume classifies an in_progress task at boot-resume. A live
// session is adopted only when its agent has started; a live bare shell is
// resumeStartFailed. A nil agentStartedFn keeps the old behaviour and adopts
// on alive. A dead session whose recorded output holds the completion marker
// is finished work. Anything else (no session id, dead session without a
// marker, no hooks wired) is reset. Pure: it reads only through the supplied
// hooks.
func decideInProgressResume(aliveFn func(string) bool, agentStartedFn func(string) bool, outputFn func(string) (string, bool), sessionID string) resumeAction {
	if sessionID == "" {
		return resumeReset
	}
	if aliveFn != nil && aliveFn(sessionID) {
		if agentStartedFn == nil || agentStartedFn(sessionID) {
			return resumeAdopt
		}
		return resumeStartFailed
	}
	if outputFn != nil {
		if out, ok := outputFn(sessionID); ok && outputHasCompletionMarker(out) {
			return resumeFinished
		}
	}
	return resumeReset
}

// outputHasCompletionMarker reports whether session output carries a real
// DATAWATCH_COMPLETE: line, using the session monitor's own detection (the
// echoed command and the instruction placeholder do not count).
func outputHasCompletionMarker(output string) bool {
	return session.ExtractCompletionSummary(output) != ""
}

// adoptedSessionPollInterval is how often waitForAdoptedSession checks the
// session when no wait hook is wired, and how often the start-confirm bound
// probes an adopted session.
const adoptedSessionPollInterval = 5 * time.Second

// AdoptedWait identifies the task whose worker was adopted at boot-resume: the
// PRD and task its nudges are recorded on, the worker's session, and the nudge
// time the task already holds for that session (zero when none). The wait
// seeds its idle-nudge tracker from NudgedAt, so a nudge sent before the
// restart is neither repeated nor forgotten.
type AdoptedWait struct {
	PRDID     string
	TaskID    string
	SessionID string
	NudgedAt  time.Time
}

// adoptedStartProbe is how the start-confirm wait reads an adopted session on
// each tick, and the bound it applies: the same bound the worker wait loop
// applies to a freshly spawned claude-code session, where a session parked in
// waiting_input with no assistant turn past the limit never started and is
// ended. The bookkeeping lives in the StartTracker the wait builds from the
// limit, the launch time and the last evidence of life when it starts.
type adoptedStartProbe struct {
	// paneFn captures the session's visible pane (the session manager's
	// CapturePaneVisible in the daemon).
	paneFn func(sessionID string) (string, error)
	// stateFn returns the session state string, or "" when the bound does not
	// apply to the session (the daemon returns "" for non claude-code
	// families, whose start can't be confirmed from the assistant marker).
	stateFn func(sessionID string) string
	// killFn ends a session that never started. Nil leaves the session to the
	// retry path, which kills any leftover session before its respawn.
	killFn func(sessionID, caller, reason string) error
	// limit is the start-confirm bound; <= 0 disables it.
	limit time.Duration
	// startedAt is the task's recorded launch time. When nil the silent window
	// is measured from adoption time instead.
	startedAt *time.Time
	// lastLifeFn returns the session's last evidence of life (its last update,
	// channel event or log write), or the zero time when none is known. When it
	// reports a time, the silent window is measured from it rather than from
	// startedAt or adoption, so an adopted worker that was busy right up to the
	// restart is not charged for the time the daemon was down.
	lastLifeFn func(sessionID string) time.Time
	// now is the clock; nil uses time.Now. Tests inject a fake clock.
	now func() time.Time
	// interval is the probe period; <= 0 uses adoptedSessionPollInterval.
	interval time.Duration
	// tracker judges the start. The wait builds it when it begins and it is nil
	// before then.
	tracker *StartTracker
}

// failureTail returns the last pane lines captured when the bound fired, for
// the task's StartFailureTail. It is nil when the bound has not fired.
func (sc *adoptedStartProbe) failureTail() []string {
	if sc == nil || sc.tracker == nil {
		return nil
	}
	return sc.tracker.FailureTail()
}

// waitForAdoptedSession blocks until an adopted session finishes. It uses the
// wait hook when wired (which keys on session state and the completion
// marker, so it is backend-agnostic) and otherwise polls aliveFn until the
// session is no longer alive. A cancelled context returns ctx.Err().
//
// When sc carries a pane hook the same wait runs under the start-confirm
// bound: a session that sits in waiting_input with no assistant turn for
// longer than sc.limit (measured from sc.startedAt, or from adoption when it
// is unset) is ended and the wait returns ErrSessionNeverStarted wrapped with
// ErrWorkerStalled. A session that shows a first assistant turn, or leaves
// waiting_input, is never failed by the bound. A nil sc keeps the plain wait.
func waitForAdoptedSession(ctx context.Context, waitFn func(context.Context, AdoptedWait) error, aliveFn func(string) bool, w AdoptedWait, sc *adoptedStartProbe) error {
	if sc == nil || sc.paneFn == nil {
		return waitAdoptedSessionToEnd(ctx, waitFn, aliveFn, w)
	}
	return waitAdoptedSessionStartConfirmed(ctx, waitFn, aliveFn, w, sc)
}

// waitAdoptedSessionToEnd is the unbounded wait: the wait hook when wired (it
// also runs the idle nudge, seeded from w.NudgedAt), otherwise a poll of
// aliveFn.
func waitAdoptedSessionToEnd(ctx context.Context, waitFn func(context.Context, AdoptedWait) error, aliveFn func(string) bool, w AdoptedWait) error {
	if waitFn != nil {
		return waitFn(ctx, w)
	}
	if aliveFn == nil {
		return nil
	}
	tick := time.NewTicker(adoptedSessionPollInterval)
	defer tick.Stop()
	for aliveFn(w.SessionID) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	return nil
}

// waitAdoptedSessionStartConfirmed runs the unbounded wait in a goroutine and
// probes the session on each tick until either the wait returns or the start
// bound fires.
func waitAdoptedSessionStartConfirmed(ctx context.Context, waitFn func(context.Context, AdoptedWait) error, aliveFn func(string) bool, w AdoptedWait, sc *adoptedStartProbe) error {
	sessionID := w.SessionID
	waitCtx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	done := make(chan error, 1)
	go func() { done <- waitAdoptedSessionToEnd(waitCtx, waitFn, aliveFn, w) }()

	clock := sc.now
	if clock == nil {
		clock = time.Now
	}
	// The silent window starts at the task's launch time, or at adoption when no
	// launch time is recorded, and moves to the last evidence of life when the
	// session reports one.
	launchedAt := clock()
	if sc.startedAt != nil {
		launchedAt = *sc.startedAt
	}
	var lastLife time.Time
	if sc.lastLifeFn != nil {
		lastLife = sc.lastLifeFn(sessionID)
	}
	tracker := NewAdoptedStartTracker(sc.limit, launchedAt, lastLife)
	sc.tracker = tracker

	interval := sc.interval
	if interval <= 0 {
		interval = adoptedSessionPollInterval
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
		now := clock()
		state := ""
		if sc.stateFn != nil {
			state = sc.stateFn(sessionID)
		}
		if state != sessionStateWaitingInput {
			tracker.Observe(StartInput{Now: now, State: state})
			continue
		}
		pane, err := sc.paneFn(sessionID)
		if err != nil || pane == "" {
			// No evidence either way this tick; the window keeps running.
			continue
		}
		obs := tracker.Observe(StartInput{Now: now, State: state, Pane: pane})
		if stalled, _ := obs.Stalled(); !stalled {
			continue
		}
		cancelWait()
		if sc.killFn != nil {
			if kerr := sc.killFn(sessionID, adoptedStartCaller, sessionStartKillReason(obs)); kerr != nil {
				log.Printf("[autonomous] adopted session %s never started: end failed: %v", sessionID, kerr)
			}
		}
		return fmt.Errorf("%s: no assistant output after %s (last %d pane line(s) recorded on the task) — %w; %w",
			SessionStartFailureReason, obs.WaitingFor.Round(time.Second), len(tracker.FailureTail()),
			ErrSessionNeverStarted, ErrWorkerStalled)
	}
}

// adoptedStartCaller is the caller recorded when the bound ends an adopted
// session that never started.
const adoptedStartCaller = "autonomous.executor.adopted-start"

func sessionStartKillReason(obs SessionStartObservation) string {
	return fmt.Sprintf("adopted session never started: waiting_input without assistant output for %s (limit %s)",
		obs.WaitingFor.Round(time.Second), obs.Limit)
}

// resetStartFailedAtBoot ends the bare-shell session of a task whose agent never
// started, and resets the task to pending with SessionStartFailureReason. No
// retry is counted. It returns the ended session id for the event record.
func (m *Manager) resetStartFailedAtBoot(t *Task, killerFn func(sessionID, caller, reason string) error, caller, why string) string {
	if killerFn != nil {
		if err := killerFn(t.SessionID, caller, why); err != nil {
			log.Printf("[autonomous] boot-resume: kill start-failed session %s: %v", t.SessionID, err)
		}
	}
	m.releaseCapacity(t.ID)
	oldSession := t.SessionID
	t.SessionID = ""
	t.AdoptedSession = false
	t.Status = TaskPending
	t.Error = SessionStartFailureReason
	t.UpdatedAt = time.Now()
	return oldSession
}

// agentStartSamples and agentStartSampleGap bound the stable start check. A
// session is read as never started only when every sample says so.
var agentStartSampleGap = time.Second

const agentStartSamples = 3

// stableAgentStarted wraps an agent-started check so a boot-time decision does
// not rest on one snapshot. A bare shell (a launch that never started) reads
// not-started on every sample. A running worker is read started on at least
// one sample, even when a snapshot lands between its own ticks.
func stableAgentStarted(fn func(string) bool) func(string) bool {
	if fn == nil {
		return nil
	}
	return func(sessionID string) bool {
		for i := 0; i < agentStartSamples; i++ {
			if i > 0 {
				time.Sleep(agentStartSampleGap)
			}
			if fn(sessionID) {
				return true
			}
		}
		return false
	}
}
