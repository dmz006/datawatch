package autonomous

import (
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// Supervisor watch liveness — the silence check's view of the
// session package's own liveness signal (internal/session/
// backend_liveness.go, ClassifyLiveness — the same classifier the
// busy probe and the gap watcher in the session package use).
//
// The worktree silence check (supervisor_watch_worktree.go,
// CheckSilent) used to judge silence from worktree file activity and
// the session records' log mtimes only. When a verdict source is
// wired, the check now asks the session liveness classifier for a
// verdict on each of an Automaton's working sessions first, and the
// verdict is authoritative:
//
//   - a worker reported running is NEVER silent — fresh liveness beats
//     a stale-looking worktree;
//
//   - a worker reported stalled (a dead process, or alive and silent
//     beyond the liveness window) is silent — the finding is raised
//     even for a worktree whose files still look recent;
//
//   - a worker at a real prompt is waiting for input, which is not
//     silence: no finding.
//
// When no verdict is available for any of the sessions the Automaton
// is working (unknown session, verdict source not wired, or the
// classifier reports no opinion), the check falls back to the existing
// worktree signals, so behaviour without a live session is unchanged.

// SupervisorWatchLivenessSource — the view the silence check reads
// worker-session liveness verdicts through.
type SupervisorWatchLivenessSource interface {
	// WorkerVerdict returns the liveness verdict for the working
	// session sessionID at now and true, or (LivenessNoOpinion, false)
	// when the source has no verdict for that session (unknown session,
	// no live process to probe): the check treats that as no verdict and
	// falls back to the worktree signals. Never blocks and never acts:
	// verdicts are computed from injected facts.
	WorkerVerdict(sessionID string, now time.Time) (session.LivenessVerdict, bool)
}

// SessionSupervisorWatchLiveness adapts the existing session manager
// (internal/session) to SupervisorWatchLivenessSource: WorkerVerdict
// asks the manager's own exported entry point (Manager.LivenessVerdictFor,
// which applies the session package's liveness classifier,
// ClassifyLiveness, to the session's caller facts — process alive,
// freshest activity, at a real prompt) for the verdict. Tests wire a
// fake Verdict function, so no daemon, tmux or process is involved.
type SessionSupervisorWatchLiveness struct {
	// Verdict is the verdict source for a session id at a moment:
	// *session.Manager's LivenessVerdictFor in production, a fixed
	// table in tests. false means the session is unknown (or has no
	// live process to probe): read as no verdict, never as silence.
	Verdict func(sessionID string, now time.Time) (session.LivenessVerdict, bool)
}

// WorkerVerdict satisfies SupervisorWatchLivenessSource. A nil Verdict,
// a blank id, or a lookup that reports unknown reads as no verdict
// (LivenessNoOpinion, false) — the check then falls back to the
// worktree signals, exactly like an unwired source.
func (a SessionSupervisorWatchLiveness) WorkerVerdict(sessionID string, now time.Time) (session.LivenessVerdict, bool) {
	id := strings.TrimSpace(sessionID)
	if a.Verdict == nil || id == "" {
		return session.LivenessNoOpinion, false
	}
	v, ok := a.Verdict(id, now)
	if !ok {
		return session.LivenessNoOpinion, false
	}
	return v, true
}
