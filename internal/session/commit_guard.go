package session

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// sessionGuarded reports whether a session's commits are held back until the
// harness validates its work: autonomous task sessions, and one-shots that
// declare validation commands.
func sessionGuarded(sess *Session) bool {
	if sess == nil {
		return false
	}
	if sess.PRDID != "" {
		return true
	}
	return sess.OneShot && len(sess.ValidateCommands) > 0
}

// armCommitGuard creates a commit guard for the session, keeps it in the
// manager and puts its config-injection environment into the tmux session so
// a worker git commit hits the hooks. Nothing is written to any repository
// config. Failures are logged and leave the session unguarded.
func (m *Manager) armCommitGuard(sess *Session, tmuxSession string) {
	if !sessionGuarded(sess) {
		return
	}
	g, err := gitguard.NewOwnedCommitGuard(m.guardOwner(), sess.FullID)
	if err != nil {
		fmt.Printf("[warn] commit guard for %s: %v\n", sess.ID, err)
		return
	}
	m.installCommitGuard(sess, tmuxSession, g)
}

func (m *Manager) installCommitGuard(sess *Session, tmuxSession string, g *gitguard.CommitGuard) {
	env := map[string]string{}
	for _, kv := range g.Env() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	if tmuxSession != "" {
		if err := m.tmux.SetEnvironment(tmuxSession, env); err != nil {
			fmt.Printf("[warn] set commit guard env for %s: %v\n", sess.ID, err)
			_ = g.Cleanup()
			return
		}
	}
	m.guardMu.Lock()
	old := m.commitGuards[sess.FullID]
	if m.commitGuards == nil {
		m.commitGuards = map[string]*gitguard.CommitGuard{}
	}
	m.commitGuards[sess.FullID] = g
	m.guardMu.Unlock()
	if old != nil {
		_ = old.Cleanup()
	}
}

func (m *Manager) commitGuardFor(fullID string) *gitguard.CommitGuard {
	m.guardMu.Lock()
	defer m.guardMu.Unlock()
	return m.commitGuards[fullID]
}

// AuthorizeCommit gives pg the commit token of the session's guard. Call it
// only after the work has passed validation: until then the token is never
// exposed to any commit. Sessions without a guard are left unchanged.
func (m *Manager) AuthorizeCommit(fullID string, pg *ProjectGit) {
	if pg == nil {
		return
	}
	if g := m.commitGuardFor(fullID); g != nil {
		pg.SetCommitEnv(g.CommitEnv())
	}
}

// releaseCommitGuard removes the session's guard directory and scrubs any
// guard hooksPath from the project's repository config. Safe to call for a
// session without a guard and safe to call repeatedly.
func (m *Manager) releaseCommitGuard(fullID, projectDir string) {
	m.guardMu.Lock()
	g := m.commitGuards[fullID]
	delete(m.commitGuards, fullID)
	m.guardMu.Unlock()
	if g == nil {
		return
	}
	if err := g.Cleanup(); err != nil {
		fmt.Printf("[warn] commit guard cleanup %s: %v\n", fullID, err)
	}
	if projectDir != "" {
		if err := gitguard.ScrubHooksPath(projectDir, ""); err != nil {
			fmt.Printf("[warn] scrub hooksPath %s: %v\n", projectDir, err)
		}
	}
}

// guardOwner identifies the guard directories this daemon makes: its resolved
// data directory, which survives a restart. "" means unknown; such a manager
// owns, and so reaps, nothing.
func (m *Manager) guardOwner() string {
	if m.dataDir == "" {
		return ""
	}
	abs, err := filepath.Abs(m.dataDir)
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return ""
	}
	return resolved
}

// paneGoneProber is implemented by a TmuxAPI that can tell "the session is
// not there" apart from "tmux could not be asked", which SessionExists cannot.
type paneGoneProber interface {
	SessionGone(name string) bool
}

// guardSessionDead reports whether the session's process is provably over, so
// that nothing can still be relying on its guard. Anything it cannot establish
// reads as alive: no record here, another host's record, a pane that exists,
// a tmux that does not answer, or a subprocess with no recorded exit.
func (m *Manager) guardSessionDead(fullID string) bool {
	rec, ok := m.store.Get(fullID)
	if !ok {
		return false
	}
	sess := m.store.Snapshot(rec)
	if sess.FullID != fullID || sess.Hostname != m.hostname {
		return false
	}
	if sess.TmuxSession == "" {
		if !sess.Subprocess {
			return false
		}
		if _, ended := readSubprocessRC(&sess); !ended {
			return false
		}
		pid, ok := readSubprocessPID(&sess)
		return !ok || !subprocessAlive(pid, sess.Task)
	}
	prober, ok := m.tmux.(paneGoneProber)
	return ok && prober.SessionGone(sess.TmuxSession)
}

// adoptCommitGuards runs at daemon adoption. This daemon's guard directories
// for sessions that are provably over are removed; a live session that is
// still guarded gets its guard back (same directory and token), and every
// other worktree is scrubbed of a leftover guard hooksPath.
func (m *Manager) adoptCommitGuards(live []*Session) {
	liveIDs := map[string]bool{}
	for _, s := range live {
		if sessionGuarded(s) {
			liveIDs[s.FullID] = true
		}
	}
	owner := m.guardOwner()
	gitguard.ReapDeadGuardDirs(owner, func(id string) bool {
		return !liveIDs[id] && m.guardSessionDead(id)
	})
	for _, s := range m.store.List() {
		if s.Hostname != m.hostname || liveIDs[s.FullID] || s.ProjectDir == "" {
			continue
		}
		_ = gitguard.ScrubHooksPath(s.ProjectDir, "")
	}
	for _, s := range live {
		if !liveIDs[s.FullID] {
			continue
		}
		g, err := gitguard.AdoptCommitGuard(owner, s.FullID)
		if err != nil {
			fmt.Printf("[warn] adopt commit guard for %s: %v\n", s.ID, err)
			continue
		}
		m.installCommitGuard(s, s.TmuxSession, g)
	}
}

// releaseAllCommitGuards releases every remaining guard.
func (m *Manager) releaseAllCommitGuards() {
	m.guardMu.Lock()
	ids := make([]string, 0, len(m.commitGuards))
	for id := range m.commitGuards {
		ids = append(ids, id)
	}
	m.guardMu.Unlock()
	for _, id := range ids {
		dir := ""
		if s, ok := m.store.Get(id); ok {
			dir = s.ProjectDir
		}
		m.releaseCommitGuard(id, dir)
	}
}
