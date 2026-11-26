// How the daemon reports a session whose agent exited without running its task
// (session.Session.LaunchFailure).

package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// workerLaunchFailure is the error an Automaton worker's wait returns when the
// session records a failed launch, or nil. It takes the launch failure retry
// path: the work tree is never verified and the task is never done.
func workerLaunchFailure(s *session.Session) error {
	if s == nil || s.LaunchFailure == "" {
		return nil
	}
	return autonomouspkg.LaunchFailureErrorWithReason(s.FullID, s.BackendFamily, string(s.State), s.LaunchFailure)
}

// stateAlertText is the body of a state change alert and the text of its chat
// line. A failed launch says why instead of repeating the task.
func stateAlertText(sess *session.Session, old session.State, event string) (body, line string) {
	if sess.State == session.StateFailed && sess.LaunchFailure != "" {
		return sess.LaunchFailure, event + ": " + sess.LaunchFailure
	}
	return truncate(sess.Task, 100), event
}
