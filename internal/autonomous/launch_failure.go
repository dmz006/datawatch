package autonomous

import (
	"errors"
	"fmt"
)

// LaunchFailureReason prefixes the retry hint recorded when a one-shot worker
// session ended without its task ever being delivered.
const LaunchFailureReason = "worker launch failed: session ended before the task was delivered"

// ErrSessionKilledBeforeDelivery marks a worker session that was killed or
// ended before its task was delivered. LaunchFailureError wraps it so the
// infrastructure classifier can tell this case from any other stall.
var ErrSessionKilledBeforeDelivery = errors.New("session killed before delivery")

// launchFailure is the error LaunchFailureError returns. Its message is the
// reason plus the session details and the ErrWorkerStalled text; it unwraps to
// both ErrSessionKilledBeforeDelivery and ErrWorkerStalled.
type launchFailure struct {
	msg string
}

func (e *launchFailure) Error() string { return e.msg }

func (e *launchFailure) Unwrap() []error {
	return []error{ErrSessionKilledBeforeDelivery, ErrWorkerStalled}
}

// LaunchFailureError reports a session that reached a terminal state with no
// task delivery recorded. It wraps ErrWorkerStalled so the executor retries it
// through the normal auto_fix_retries budget as an infra failure instead of
// passing the (empty) session to verification as finished work. It also wraps
// ErrSessionKilledBeforeDelivery.
func LaunchFailureError(sessionID, backend, state string) error {
	return &launchFailure{msg: fmt.Sprintf("%s (session=%s, backend=%s, state=%s): %s",
		LaunchFailureReason, sessionID, backend, state, ErrWorkerStalled)}
}
