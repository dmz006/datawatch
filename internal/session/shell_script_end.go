// A shell script says how it ended: the command the shell backend types prints
// a completion line on success and a failure line otherwise. The failure line
// ends a one-shot as failed. As with a completion line, it never ends an
// interactive session.

package session

import (
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/llm/backends/shell"
)

// isShellScriptFailureLine reports whether l is the shell backend's failure
// line. Only that exact line counts.
func isShellScriptFailureLine(l string) bool {
	for _, seg := range strings.Split(l, "\r") {
		if strings.TrimSpace(StripANSI(seg)) == shell.FailedLine {
			return true
		}
	}
	return false
}

func showsShellScriptFailure(lines []string) bool {
	for _, l := range lines {
		if isShellScriptFailureLine(l) {
			return true
		}
	}
	return false
}

// failsOnShellFailureLine reports whether sess is the kind a failure line ends.
// It reads only what is fixed at launch; failShellScript checks the state.
func failsOnShellFailureLine(sess *Session) bool {
	return sess.BackendFamily == "shell" && sess.OneShot
}

func isLive(state State) bool {
	return state == StateRunning || state == StateWaitingInput
}

// failShellScript ends a one-shot whose script exited with an error and reports
// whether this call ended it. More than one watcher can see the line, so the
// state is checked and changed in one step and the session ends once. As for
// any failed run, nothing is committed.
func (m *Manager) failShellScript(fullID string) bool {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return false
	}
	why := EndReason{Caller: callerAt(0), Reason: "the shell script exited with an error"}
	var oldState State
	ended := false
	m.store.Mutate(func() {
		if !failsOnShellFailureLine(sess) || !isLive(sess.State) {
			return
		}
		oldState, ended = sess.State, true
		now := time.Now()
		sess.State, sess.UpdatedAt = StateFailed, now
		stampEnd(sess, now, endNoteFor(oldState, StateFailed, why))
	})
	if !ended {
		return false
	}
	_ = m.store.Save(sess)
	m.releaseCommitGuard(sess.FullID, sess.ProjectDir)
	m.recordEnd(sess, m.getTracker(sess.FullID), oldState, StateFailed, why)
	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	m.emitSessionEnd(sess)
	m.triggerSummarize(sess.FullID, 200)
	return true
}

// failMissedShellScripts ends the ad-hoc one-shots whose failure line no
// watcher saw, as the reaper does for a missed completion line.
func (m *Manager) failMissedShellScripts() {
	for _, sess := range m.store.List() {
		snap := m.store.Snapshot(sess)
		if !failsOnShellFailureLine(&snap) || !isLive(snap.State) || snap.TmuxSession == "" || snap.PRDID != "" {
			continue
		}
		out, err := m.tmux.CapturePaneScrollback(snap.TmuxSession, 500)
		if err == nil && showsShellScriptFailure(strings.Split(out, "\n")) {
			m.failShellScript(snap.FullID)
		}
	}
}
