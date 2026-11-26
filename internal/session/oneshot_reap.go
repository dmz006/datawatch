package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// oneShotCompletionMarker is the line a one-shot's command prints once its
// worker has returned. See the pi backend's buildLaunchCmd.
const oneShotCompletionMarker = "DATAWATCH_COMPLETE:"

// BL468 — piFailureMarker is the line the pi launch wrapper prints when pi
// exits non-zero ("DATAWATCH_PI_FAILED: pi exited with status <N>", with an
// optional " (model <m>)" suffix on the one-shot path). It is a FAILURE
// signal, never a completion; see isRealFailureLine/parsePiFailureExit.
const piFailureMarker = "DATAWATCH_PI_FAILED:"

// defaultSessionRetentionDays is used when session_retention_days is unset.
const defaultSessionRetentionDays = 30

// oneShotPruneInterval is how often finished session folders are checked for
// retention. Folder scans are heavier than pane checks, so this runs hourly.
const oneShotPruneInterval = time.Hour

// oneShotReapInterval is how often the daemon looks for finished one-shots
// that the completion watcher missed.
const oneShotReapInterval = 60 * time.Second

// oneShotFinished reports whether an ad-hoc one-shot's pane shows its
// completion marker on a line of its own. The echoed command text does not
// count: there the marker sits after "echo '", so the line does not start
// with it. PRD-owned sessions are never reaped here; their executor owns them.
func (m *Manager) oneShotFinished(sess *Session) bool {
	return m.oneShotFinishReason(sess) != ""
}

// launchRoleSupervisor is the launch role every lean supervisor spawn carries
// (supervisorloop.LaunchRoleSupervisor; this package cannot import it).
const launchRoleSupervisor = "supervisor"

const (
	// endOnStopGrace is how long after the Stop hook a supervisor pass must stay
	// quiet before it is ended, so input that arrives right after a stop (a wake
	// event, a nudge) is not cut off.
	endOnStopGrace = 20 * time.Second
	// supervisorPassMaxAge is the backstop: a supervisor pass is short by design,
	// so one still alive after this long is ended whether or not its hook fired.
	supervisorPassMaxAge = 30 * time.Minute
)

// supervisorPassFinishReason says why a scheduled supervisor pass should end now,
// or "" if it should not. A pass is a single turn: once Claude Code's Stop hook
// has fired after the task was delivered and nothing has happened for the grace
// period, it is done, whatever the model printed. Interactive (not one-shot)
// supervisor sessions are never ended here.
func (m *Manager) supervisorPassFinishReason(sess *Session) string {
	if !sess.OneShot || sess.LaunchRole != launchRoleSupervisor {
		return ""
	}
	now := m.now()
	m.mu.Lock()
	stopAt, stopped := m.hookStopAt[sess.FullID]
	m.mu.Unlock()
	if stopped && now.Sub(stopAt) >= endOnStopGrace {
		if deliveredAt, ok := m.taskDeliveredTime(sess); ok && stopAt.After(deliveredAt) {
			return "supervisor pass finished: the Stop hook fired and no new input followed"
		}
	}
	if !sess.CreatedAt.IsZero() && now.Sub(sess.CreatedAt) >= supervisorPassMaxAge {
		return fmt.Sprintf("supervisor pass ran past %s without ending", supervisorPassMaxAge)
	}
	return ""
}

// oneShotFinishReason says why a one-shot is finished, or "" if it is not.
func (m *Manager) oneShotFinishReason(sess *Session) string {
	if !sess.OneShot || sess.TmuxSession == "" || sess.PRDID != "" {
		return ""
	}
	if sess.State != StateRunning && sess.State != StateWaitingInput {
		return ""
	}
	if reason := m.supervisorPassFinishReason(sess); reason != "" {
		return reason
	}
	out, err := m.tmux.CapturePaneScrollback(sess.TmuxSession, 500)
	if err != nil {
		return ""
	}
	if m.exitHeld(sess.FullID, sess.TmuxSession) {
		return "" // the agent exited without its task: not finished work
	}
	// BL468 — note the state guard above already leaves sessions in a
	// terminal state (complete/failed/killed) alone, so a StateFailed
	// record is never reaped or re-transitioned here.
	// The same real-completion rules as the pane scanners: the wrapped tail of
	// the typed launch command ("...echo 'DATAWATCH_COMPLETE: claude done'")
	// and an instruction placeholder are not the worker's own completion.
	// BL468 — the pi non-zero-exit failure marker is a failure signal, not a
	// completion: even a malformed marker line must never satisfy this check.
	for _, line := range strings.Split(StripANSI(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, piFailureMarker) {
			continue
		}
		if strings.HasPrefix(l, oneShotCompletionMarker) && isRealCompletionLine(l, oneShotCompletionMarker) {
			return "one-shot completion marker found in the pane"
		}
	}
	return ""
}

// FinishedOneShots lists ad-hoc one-shot sessions whose command has finished
// but which never reached complete. It only reads; nothing is changed.
func (m *Manager) FinishedOneShots() []*Session {
	var out []*Session
	for _, sess := range m.store.List() {
		if m.oneShotFinished(sess) {
			out = append(out, sess)
		}
	}
	return out
}

// ReapFinishedOneShots completes finished ad-hoc one-shots. It runs the normal
// completion path, so the validation gate still decides whether anything is
// committed, then it marks the session complete and closes its pane. Returns
// the full IDs it completed. Safe to call repeatedly.
//
// BL468 — sessions already in StateFailed are never listed by
// FinishedOneShots (oneShotFinished requires running/waiting_input), so a
// recorded failure is left alone — its pane and scratch are the failure
// path's problem, not the completion reaper's.
func (m *Manager) ReapFinishedOneShots() []string {
	var reaped []string
	for _, sess := range m.FinishedOneShots() {
		oldState := sess.State
		// Read the reason while the session still reads live: a finished
		// state has no finish reason.
		reason := m.oneShotFinishReason(sess)
		if reason == "" {
			reason = "one-shot completion marker found in the pane"
		}
		why := EndReason{Caller: callerAt(0), Reason: reason}
		m.commitOnCompletion(sess, NewProjectGit(sess.ProjectDir))
		// If completion left work unpushed (no commit made, or its push
		// failed / was refused), save it now — a reaped one-shot never
		// gets a second completion chance. commitOnCompletion's own push
		// already ran, so work it pushed is not pushed twice (the helper
		// sees zero unpushed commits and does nothing).
		m.pushUnpushedBeforeTeardown(sess)
		m.markEnded(sess, StateComplete, why)
		_ = m.store.Save(sess)
		m.releaseCommitGuard(sess.FullID, sess.ProjectDir)
		m.recordEnd(sess, m.getTracker(sess.FullID), oldState, StateComplete, why)
		if m.onStateChange != nil {
			m.onStateChange(sess, oldState)
		}
		m.killTmuxSessionFor(sess.FullID, reason)
		removeOneShotScratch(sess.TmuxSession)
		reaped = append(reaped, sess.FullID)
	}
	// An agent that exited without its task is never listed above as
	// finished. Where no watcher saw it exit, end it as failed here.
	m.failMissedEarlyExits()
	return reaped
}

// StartOneShotReaper reaps finished one-shots once at startup and then every
// oneShotReapInterval until ctx is done. This is the prevention half of the
// zombie fix: a one-shot whose work has ended is closed even if its completion
// watcher missed the marker.
//
// Only the daemon calls it, right after ResumeMonitors, so it is also where
// delivery claims the last daemon never verified are taken over. A second
// process that only resumes monitors must never type a task.
func (m *Manager) StartOneShotReaper(ctx context.Context) {
	m.recoverUnverifiedDeliveries(ctx)
	go func() {
		// The startup pass runs here, not inline: a reaped one-shot can run
		// validation commands for minutes, and daemon boot must not wait on that.
		m.ReapFinishedOneShots()
		m.PruneSessionFolders(time.Now())
		reap := time.NewTicker(oneShotReapInterval)
		prune := time.NewTicker(oneShotPruneInterval)
		defer reap.Stop()
		defer prune.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reap.C:
				m.ReapFinishedOneShots()
				m.CloseFinishedIdle() // does nothing unless session.finished_idle_close_minutes is set
			case now := <-prune.C:
				m.PruneSessionFolders(now)
			}
		}
	}()
}

// piScratchNamePart sanitizes a tmux session name the same way the pi
// backend's scratchNamePart does, so cleanup globs match the files buildLaunchCmd
// reserved: every non-alphanumeric rune becomes '-'.
func piScratchNamePart(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b = append(b, byte(r))
		} else {
			b = append(b, '-')
		}
	}
	return string(b)
}

// removeOneShotScratch deletes the scratch task, output and status files the pi
// launch command writes for one tmux session. The launch command removes its
// files itself only when pi returns normally, so a zombie or a killed run
// leaves them behind.
func removeOneShotScratch(tmuxSession string) {
	if tmuxSession == "" {
		return
	}
	session := piScratchNamePart(tmuxSession)
	for _, prefix := range []string{"datawatch-pi-task-", "datawatch-pi-status-", "datawatch-pi-out-"} {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), prefix+session+"-*"))
		for _, f := range matches {
			_ = os.Remove(f)
		}
	}
}

// sessionRetentionDays returns the configured retention window, falling back
// to the default when it is unset or not positive.
func (m *Manager) sessionRetentionDays() int {
	if m.cfg != nil && m.cfg.Session.SessionRetentionDays > 0 {
		return m.cfg.Session.SessionRetentionDays
	}
	return defaultSessionRetentionDays
}

// PruneSessionFolders deletes the folders of finished sessions whose last
// update is older than the retention window. Live sessions are never touched,
// and the store record is kept, so history still lists the session. Returns
// the full IDs whose folders were removed.
func (m *Manager) PruneSessionFolders(now time.Time) []string {
	if m.dataDir == "" {
		return nil
	}
	cutoff := now.Add(-time.Duration(m.sessionRetentionDays()) * 24 * time.Hour)
	var removed []string
	for _, sess := range m.store.List() {
		switch sess.State {
		case StateComplete, StateKilled, StateFailed:
		default:
			continue
		}
		if sess.UpdatedAt.IsZero() || sess.UpdatedAt.After(cutoff) {
			continue
		}
		dir := filepath.Join(m.dataDir, "sessions", sess.FullID)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err == nil {
			removed = append(removed, sess.FullID)
		}
	}
	return removed
}
