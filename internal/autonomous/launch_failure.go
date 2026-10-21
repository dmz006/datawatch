package autonomous

import "fmt"

// LaunchFailureReason prefixes the retry hint recorded when a one-shot worker
// session ended without its task ever being delivered.
const LaunchFailureReason = "worker launch failed: session ended before the task was delivered"

// LaunchFailureError reports a session that reached a terminal state with no
// task delivery recorded. It wraps ErrWorkerStalled so the executor retries it
// through the normal auto_fix_retries budget as an infra failure instead of
// passing the (empty) session to verification as finished work.
func LaunchFailureError(sessionID, backend, state string) error {
	return fmt.Errorf("%s (session=%s, backend=%s, state=%s): %w",
		LaunchFailureReason, sessionID, backend, state, ErrWorkerStalled)
}
