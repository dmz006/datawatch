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
	for _, line := range strings.Split(StripANSI(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), oneShotCompletionMarker) {
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
func (m *Manager) ReapFinishedOneShots() []string {
	var reaped []string
	for _, sess := range m.FinishedOneShots() {
		oldState := sess.State
		m.commitOnCompletion(sess, NewProjectGit(sess.ProjectDir))
		// If completion left work unpushed (no commit made, or its push
		// failed / was refused), save it now — a reaped one-shot never
		// gets a second completion chance. commitOnCompletion's own push
		// already ran, so work it pushed is not pushed twice (the helper
		// sees zero unpushed commits and does nothing).
		m.pushUnpushedBeforeTeardown(sess)
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

// removeOneShotScratch deletes the scratch task files the pi launch command
// writes for one tmux session. The launch command removes its file itself
// only when pi returns normally, so a zombie or a killed run leaves one behind.
func removeOneShotScratch(tmuxSession string) {
	if tmuxSession == "" {
		return
	}
	matches, _ := filepath.Glob("/tmp/datawatch-pi-task-" + tmuxSession + "-*.txt")
	for _, f := range matches {
		_ = os.Remove(f)
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
