package session

import (
	"fmt"
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
	g, err := gitguard.NewCommitGuard(sess.FullID)
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

// adoptCommitGuards runs at daemon adoption. Guard directories of sessions
// that are not live are removed; a live session that is still guarded gets
// its guard back (same directory and token), and every other worktree is
// scrubbed of a leftover guard hooksPath.
func (m *Manager) adoptCommitGuards(live []*Session) {
	liveIDs := map[string]bool{}
	for _, s := range live {
		if sessionGuarded(s) {
			liveIDs[s.FullID] = true
		}
	}
	gitguard.ReapStaleGuardDirs(liveIDs)
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
		g, err := gitguard.AdoptCommitGuard(s.FullID)
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
