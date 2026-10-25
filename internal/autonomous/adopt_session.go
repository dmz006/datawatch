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

// adoptedStartConfirm is the start-confirm bound applied while an adopted
// session is waited on. It is the same bound the worker wait loop applies to
// a freshly spawned claude-code session: a session parked in waiting_input
// with no assistant turn past the limit never started and is ended.
type adoptedStartConfirm struct {
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
	// now is the clock; nil uses time.Now. Tests inject a fake clock.
	now func() time.Time
	// interval is the probe period; <= 0 uses adoptedSessionPollInterval.
	interval time.Duration
	// failureTail holds the last pane lines captured when the bound fires, for
	// the task's StartFailureTail.
	failureTail []string
}

func (sc *adoptedStartConfirm) clock() time.Time {
	if sc.now != nil {
		return sc.now()
	}
	return time.Now()
}

func (sc *adoptedStartConfirm) probeInterval() time.Duration {
	if sc.interval > 0 {
		return sc.interval
	}
	return adoptedSessionPollInterval
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
func waitForAdoptedSession(ctx context.Context, waitFn func(context.Context, string) error, aliveFn func(string) bool, sessionID string, sc *adoptedStartConfirm) error {
	if sc == nil || sc.paneFn == nil {
		return waitAdoptedSessionToEnd(ctx, waitFn, aliveFn, sessionID)
	}
	return waitAdoptedSessionStartConfirmed(ctx, waitFn, aliveFn, sessionID, sc)
}

// waitAdoptedSessionToEnd is the unbounded wait: the wait hook when wired,
// otherwise a poll of aliveFn.
func waitAdoptedSessionToEnd(ctx context.Context, waitFn func(context.Context, string) error, aliveFn func(string) bool, sessionID string) error {
	if waitFn != nil {
		return waitFn(ctx, sessionID)
	}
	if aliveFn == nil {
		return nil
	}
	tick := time.NewTicker(adoptedSessionPollInterval)
	defer tick.Stop()
	for aliveFn(sessionID) {
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
func waitAdoptedSessionStartConfirmed(ctx context.Context, waitFn func(context.Context, string) error, aliveFn func(string) bool, sessionID string, sc *adoptedStartConfirm) error {
	waitCtx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	done := make(chan error, 1)
	go func() { done <- waitAdoptedSessionToEnd(waitCtx, waitFn, aliveFn, sessionID) }()

	// silentFrom is where the current silent window starts: the task's launch
	// time, or adoption time when no launch time is recorded. A probe that finds
	// the session outside waiting_input restarts it.
	silentFrom := sc.clock()
	if sc.startedAt != nil {
		silentFrom = *sc.startedAt
	}
	// started latches once a first assistant turn is seen: the session is up,
	// and the bound never fails it afterwards.
	started := false

	tick := time.NewTicker(sc.probeInterval())
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
		now := sc.clock()
		state := ""
		if sc.stateFn != nil {
			state = sc.stateFn(sessionID)
		}
		if state != sessionStateWaitingInput {
			silentFrom = now
			continue
		}
		pane, err := sc.paneFn(sessionID)
		if err != nil || pane == "" {
			// No evidence either way this tick; the window keeps running.
			continue
		}
		if HasAssistantOutput(pane) {
			started = true
		}
		if started {
			continue
		}
		obs := SessionStartObservation{State: state, Limit: sc.limit}
		if now.After(silentFrom) {
			obs.WaitingFor = now.Sub(silentFrom)
		}
		stalled, _ := obs.Stalled()
		if !stalled {
			continue
		}
		sc.failureTail = LastLines(pane, 20)
		cancelWait()
		if sc.killFn != nil {
			if kerr := sc.killFn(sessionID, adoptedStartCaller, sessionStartKillReason(obs)); kerr != nil {
				log.Printf("[autonomous] adopted session %s never started: end failed: %v", sessionID, kerr)
			}
		}
		return fmt.Errorf("%s: no assistant output after %s (last %d pane line(s) recorded on the task) — %w; %w",
			SessionStartFailureReason, obs.WaitingFor.Round(time.Second), len(sc.failureTail),
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
