package session

import (
	"context"
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
	if !sess.OneShot || sess.TmuxSession == "" || sess.PRDID != "" {
		return false
	}
	if sess.State != StateRunning && sess.State != StateWaitingInput {
		return false
	}
	out, err := m.tmux.CapturePaneScrollback(sess.TmuxSession, 500)
	if err != nil {
		return false
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
			return true
		}
	}
	return false
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
		m.commitOnCompletion(sess, NewProjectGit(sess.ProjectDir))
		sess.State = StateComplete
		sess.UpdatedAt = time.Now()
		_ = m.store.Save(sess)
		if m.onStateChange != nil {
			m.onStateChange(sess, oldState)
		}
		m.KillTmuxSession(sess.FullID)
		removeOneShotScratch(sess.TmuxSession)
		reaped = append(reaped, sess.FullID)
	}
	return reaped
}

// StartOneShotReaper reaps finished one-shots once at startup and then every
// oneShotReapInterval until ctx is done. This is the prevention half of the
// zombie fix: a one-shot whose work has ended is closed even if its completion
// watcher missed the marker.
func (m *Manager) StartOneShotReaper(ctx context.Context) {
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
