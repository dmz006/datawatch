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
// session when no wait hook is wired.
const adoptedSessionPollInterval = 5 * time.Second

// waitForAdoptedSession blocks until an adopted session finishes. It uses the
// wait hook when wired (which keys on session state and the completion
// marker, so it is backend-agnostic) and otherwise polls aliveFn until the
// session is no longer alive. A cancelled context returns ctx.Err().
func waitForAdoptedSession(ctx context.Context, waitFn func(context.Context, string) error, aliveFn func(string) bool, sessionID string) error {
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
