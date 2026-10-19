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
	HasLiveChildProcess bool          // true when a runner child (claude/pi/opencode) is alive in the pane tree
	Limit               time.Duration // effective timeout (0 disables the detector)
}

// Stalled reports whether the observed session is idle past its limit with
// no live runner child. The reason is non-empty only when it stalls.
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
	return true, fmt.Sprintf("%s (session=%s, backend=%s, model=%s, idle %s > limit %s)",
		SessionIdleFailureReason, o.SessionID, o.Backend, o.Model, o.IdleFor, o.Limit)
}

// SessionIdleError returns an error that wraps ErrWorkerStalled, so the
// executor's stall retry path handles an idle session the same way it
// handles an SSE or no-output stall.
func SessionIdleError(o SessionIdleObservation) error {
	msg := fmt.Sprintf("%s (session=%s, backend=%s, model=%s)",
		SessionIdleFailureReason, o.SessionID, o.Backend, o.Model)
	if stalled, reason := o.Stalled(); stalled {
		msg = reason
	}
	return fmt.Errorf("%s: %w", msg, ErrWorkerStalled)
}
