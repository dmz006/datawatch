// Boot-resume session adoption: decides what to do with an in_progress task
// whose worker session may have outlived the previous daemon.
package autonomous

import (
	"context"
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
)

// decideInProgressResume classifies an in_progress task at boot-resume. A live
// session is adopted. A dead session whose recorded output holds the
// completion marker is finished work. Anything else (no session id, dead
// session without a marker, no hooks wired) is reset. Pure: it reads only
// through the supplied hooks.
func decideInProgressResume(aliveFn func(string) bool, outputFn func(string) (string, bool), sessionID string) resumeAction {
	if sessionID == "" {
		return resumeReset
	}
	if aliveFn != nil && aliveFn(sessionID) {
		return resumeAdopt
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
// session when no wait hook is wired.
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

// waitForAdoptedSession blocks until an adopted session finishes. It uses the
// wait hook when wired (which keys on session state and the completion
// marker, so it is backend-agnostic) and otherwise polls aliveFn until the
// session is no longer alive. A cancelled context returns ctx.Err().
func waitForAdoptedSession(ctx context.Context, waitFn func(context.Context, AdoptedWait) error, aliveFn func(string) bool, w AdoptedWait) error {
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
