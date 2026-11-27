// How the daemon reports a session whose launch is recorded as failed
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

// neverGivenTaskFailure is the launch failure error of a local claude-code
// worker whose task datawatch had to type, with no proof that it arrived.
// Such a worker is never done. why is the delivery's own failure, if known.
func neverGivenTaskFailure(s *session.Session, why string) error {
	if s == nil || s.AgentID != "" || !s.TaskNeverProven() {
		return nil
	}
	if why == "" {
		why = "datawatch types this worker's task itself and has no proof it arrived"
	}
	return autonomouspkg.LaunchFailureErrorWithReason(s.FullID, s.BackendFamily, string(s.State), why)
}

// undeliveredWorkerFailure is the error for a worker that ended with nothing to
// show the task ran, or nil. delivered reports a recorded delivery claim.
//
// A task datawatch types needs proof of arrival. Any other claude-code one-shot
// with no delivery record is judged by its output, where the launch command's
// exit line is not the worker's completion marker.
func undeliveredWorkerFailure(s *session.Session, delivered bool, why string) error {
	if err := neverGivenTaskFailure(s, why); err != nil {
		return err
	}
	if s != nil && s.OneShot && s.AgentID == "" &&
		s.BackendFamily == "claude-code" && !delivered &&
		!autonomouspkg.JudgeCompletedByOutput(s.State == session.StateComplete,
			s.CompletionSummary != "" && !session.IsLauncherExitSummary(s.CompletionSummary),
			s.CommitSHA, s.LastResponse != "") {
		return autonomouspkg.LaunchFailureErrorWithReason(s.FullID, s.BackendFamily, string(s.State), why)
	}
	return nil
}

// stateAlertText is the body of a state change alert and the text of its chat
// line. A failed launch says why instead of repeating the task.
func stateAlertText(sess *session.Session, old session.State, event string) (body, line string) {
	if sess.State == session.StateFailed && sess.LaunchFailure != "" {
		return sess.LaunchFailure, event + ": " + sess.LaunchFailure
	}
	return truncate(sess.Task, 100), event
}
