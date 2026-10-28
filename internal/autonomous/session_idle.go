package autonomous

import (
	"fmt"
	"time"
)

// SessionIdleFailureReason is the failure reason recorded when a worker
// session sits waiting for input with no live runner child past the limit.
const SessionIdleFailureReason = "session idle without completion"

// sessionStateWaitingInput mirrors session.StateWaitingInput. It is compared
// as a plain string because internal/session imports this package's callers,
// so importing it here would create a cycle.
const sessionStateWaitingInput = "waiting_input"

// SessionIdleObservation is a point-in-time view of a worker session used to
// decide whether it has stalled without completing its task.
type SessionIdleObservation struct {
	SessionID           string        // the session's id, e.g. "idle-sess"
	Backend             string        // backend family, e.g. pi-prompt / claude / opencode
	Model               string        // e.g. qwen3.8 (may be empty)
	State               string        // session state string ("waiting_input", "running", ...)
	IdleFor             time.Duration // how long the session has been without a terminal state
	OutputAge           time.Duration // age of the session log/mtime signal (0 if unknown)
	LogLastEventAge     time.Duration // age of the newest event in the backend's own session log (0 if unknown)
	HasLiveChildProcess bool          // true when a runner child (claude/pi/opencode) is alive in the pane tree
	Limit               time.Duration // effective timeout (0 disables the detector)
	// Nudge is the nudge history of this session as the daemon read it from the
	// task: "no nudge sent", or "nudge N sent at T, outcome O". Empty when the
	// caller did not read it, in which case no clause is named.
	Nudge string
}

// nudgeClause returns the nudge history as a trailing clause of a reason, or
// "" when the caller did not read it.
func (o SessionIdleObservation) nudgeClause() string {
	if o.Nudge == "" {
		return ""
	}
	return ", " + o.Nudge
}

// ActivityAge returns the age of the freshest output signal and whether one
// is known. The backend's own log (LogLastEventAge) wins when known, because
// it only moves when the agent does something, while the terminal signal
// (OutputAge) stays silent for a pane that is thinking. A zero age means
// unknown, and then the terminal signal is used.
func (o SessionIdleObservation) ActivityAge() (time.Duration, bool) {
	if o.LogLastEventAge > 0 {
		return o.LogLastEventAge, true
	}
	if o.OutputAge > 0 {
		return o.OutputAge, true
	}
	return 0, false
}

// Stalled reports whether the observed session is idle past its limit with
// no live runner child and no output newer than the limit. The reason is
// non-empty only when it stalls.
func (o SessionIdleObservation) Stalled() (bool, string) {
	if o.Limit <= 0 {
		return false, ""
	}
	switch o.State {
	case sessionStateWaitingInput, "idle":
	default:
		return false, ""
	}
	if o.HasLiveChildProcess {
		return false, ""
	}
	if o.IdleFor <= o.Limit {
		return false, ""
	}
	// A log that keeps growing is progress even when the pane is silent.
	if age, known := o.ActivityAge(); known && age <= o.Limit {
		return false, ""
	}
	return true, fmt.Sprintf("%s (session=%s, backend=%s, model=%s, idle %s > limit %s%s)",
		SessionIdleFailureReason, o.SessionID, o.Backend, o.Model, o.IdleFor, o.Limit, o.nudgeClause())
}

// SessionIdleError returns an error that wraps ErrWorkerStalled, so the
// executor's stall retry path handles an idle session the same way it
// handles an SSE or no-output stall.
func SessionIdleError(o SessionIdleObservation) error {
	msg := fmt.Sprintf("%s (session=%s, backend=%s, model=%s%s)",
		SessionIdleFailureReason, o.SessionID, o.Backend, o.Model, o.nudgeClause())
	if stalled, reason := o.Stalled(); stalled {
		msg = reason
	}
	return fmt.Errorf("%s: %w", msg, ErrWorkerStalled)
}
