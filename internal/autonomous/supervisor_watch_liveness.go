// The supervisor watch's silence check (supervisor_watch_worktree.go,
// CheckSilent) reuses the session package's own liveness signal to decide
// whether a working Automaton has gone silent, instead of inferring it
// from the worktree's files alone.
//
// This file is the thin adapter between the two: the silence check knows
// only the SupervisorWatchLivenessSource interface and needs to ask "what
// is the liveness assessment of this working session at this moment?"; the
// session package answers. An assessment is a verdict (the classifier's
// answer) plus the alive-and-silent fact (the worker's process is alive
// but has no activity beyond the liveness window).
//
// Two things in particular: the silence check must never import the
// session package's internals (it works over PRDs) and must stay fully
// fakeable in tests (fake verdicts, no daemon, no tmux, no shell, no
// process, no version bump, no gh call). So the source is an interface
// with a single method — what the silence check needs, nothing else — and
// the adapter around the real session manager is a plain struct
// (SessionSupervisorWatchLiveness) with a function field. Production
// wires it to the session manager's exported assessment view,
// LivenessAssessmentFor (cmd/datawatch), which is the same view the busy
// probe and the gap watcher judge from, with the silence consent the
// silence check requires:
//
//   - a session the source reports running is never silent;
//
//   - a session it reports stalled (dead process, or alive and silent
//     beyond the liveness window, for every family the classifier stalls)
//     is silent;
//
//   - the alive-and-silent fact is silent too, even on claude-code: the
//     classifier deliberately keeps that verdict running there (rule (f)
//     preserves the busy probe's semantics), so the silence rides in the
//     fact and the silence check reads it as silence;
//
//   - a session it reports waiting for input (at a real prompt) is not
//     silent;
//
//   - an id the source reports no assessment for carries no information —
//     the silence check falls back to its worktree signals, exactly as
//     before this adapter existed.
package autonomous

import (
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// SupervisorWatchLivenessSource is the silence check's view of the liveness
// signal: one ask, one answer. WorkerVerdict asks for the liveness
// assessment of the working session sessionID as of now, and reports a
// verdict, the alive-and-silent fact, and found; or (LivenessNoOpinion,
// false, false) when the source has no assessment for that session.
//
// The source is deliberately a small interface, not a concrete type: a
// silence check only needs to ask that one question, and in tests the
// source is a fixed assessment table, so the check stays fakeable without
// a single daemon, tmux or process. Production wraps the session manager's
// LivenessAssessmentFor in SessionSupervisorWatchLiveness below.
type SupervisorWatchLivenessSource interface {
	// WorkerVerdict returns, as of now, the liveness assessment for the
	// working session sessionID: a verdict plus the alive-and-silent fact,
	// both with found; or (LivenessNoOpinion, false, false) when the
	// source has no assessment for that id (unknown session, or the
	// classifier has no opinion). The verdict and the fact are read
	// together: a running verdict with the fact true is alive and silent
	// beyond the window on a family that keeps the running verdict
	// (claude-code), and the silence check reads it as silence.
	WorkerVerdict(sessionID string, now time.Time) (session.LivenessVerdict, bool, bool)
}

// SessionSupervisorWatchLiveness adapts a session-id → assessment
// function — the session manager's LivenessAssessmentFor in production,
// a fixed table in tests — to SupervisorWatchLivenessSource. It is what
// cmd/datawatch wires into the autonomous manager (SetSupervisorWatchLiveness),
// and it keeps the silence check honest about which half of the answer it
// has: a found=false assessment is treated as "no verdict", not as
// silence, so an unknown session cannot manufacture a finding the signals
// do not support.
type SessionSupervisorWatchLiveness struct {
	// Assessment answers "what is the liveness assessment of sessionID as
	// of now?" — the verdict, the alive-and-silent fact, and found;
	// found=false means "no assessment for that session", not "silent".
	// In production this is *session.Manager.LivenessAssessmentFor; an
	// empty (blank) id or a nil function reads as no assessment.
	Assessment func(sessionID string, now time.Time) (session.LivenessVerdict, bool, bool)
}

// WorkerVerdict satisfies SupervisorWatchLivenessSource. It trims the
// session id before asking, answers "no assessment" for an empty id or a
// nil / not-found Assessment, and passes the assessment (verdict plus
// alive-and-silent fact) through otherwise. It does no I/O of its own:
// whatever it asks is the question the underlying function was asked.
func (a SessionSupervisorWatchLiveness) WorkerVerdict(sessionID string, now time.Time) (session.LivenessVerdict, bool, bool) {
	id := strings.TrimSpace(sessionID)
	if id == "" || a.Assessment == nil {
		return session.LivenessNoOpinion, false, false
	}
	v, aliveSilent, found := a.Assessment(id, now)
	if !found {
		return session.LivenessNoOpinion, false, false
	}
	return v, aliveSilent, true
}
