// Between-task branch refresh. In the gap after one task finishes and before
// the next starts, an Automaton branch that has fallen far enough behind its
// base gets the base merged in. A clean merge is recorded; a conflict leaves
// the branch as it was, stops refreshing for the Automaton and asks the
// operator what to do.
package autonomous

import (
	"context"
	"log"
	"strings"
	"time"
)

// branchRefreshEnabled resolves the effective setting: the per-Automaton
// override first, then the daemon config.
func (m *Manager) branchRefreshEnabled(prd *PRD) bool {
	if prd.BranchRefreshOverride != nil {
		return *prd.BranchRefreshOverride
	}
	return m.Config().BranchRefreshEnabled
}

// branchRefreshLimit returns the commits-behind limit; zero reads as 5.
func (m *Manager) branchRefreshLimit() int {
	if n := m.Config().BranchRefreshBehindLimit; n != 0 {
		return n
	}
	return 5
}

// taskRunningInWorktree reports whether any task of prd is running in the
// Automaton's own worktree.
func (m *Manager) taskRunningInWorktree(prd *PRD) bool {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.Status != TaskInProgress && t.Status != TaskRunningTests {
				continue
			}
			if dir := ResolveTaskProjectDir(prd, t); dir == "" || dir == prd.ProjectDir {
				return true
			}
		}
	}
	return false
}

// maybeRefreshBranch merges the base branch into the Automaton branch when
// the refresh is on, not stopped, no task is running in the worktree and the
// branch is at or over the behind limit. It never returns an error: a failure
// is logged and recorded and the run goes on.
func (m *Manager) maybeRefreshBranch(ctx context.Context, prd *PRD) {
	if prd == nil || prd.ProjectDir == "" || !m.branchRefreshEnabled(prd) {
		return
	}
	if prd.BranchRefresh != nil && prd.BranchRefresh.Stopped {
		return
	}
	if m.taskRunningInWorktree(prd) {
		return
	}
	m.mu.Lock()
	runner := m.branchRefreshGitRunner
	m.mu.Unlock()
	g := NewBranchRefreshGit(runner)
	dir := prd.ProjectDir
	base := m.baseBranchFor(ctx, prd, dir, func(c context.Context, d string, args ...string) (string, error) {
		return g.git.Run(c, d, args...)
	})

	behind, err := g.CommitsBehind(ctx, dir, base)
	if err != nil {
		log.Printf("[autonomous] prd=%s branch refresh: %v", prd.ID, err)
		return
	}
	if behind < m.branchRefreshLimit() {
		return
	}
	head, _ := g.MainHead(ctx, dir, base)
	st := BranchRefresh{CommitsBehind: behind, LastMainCommit: head, LastRefreshAt: time.Now()}

	files, err := g.Merge(ctx, dir, base)
	switch {
	case err != nil:
		log.Printf("[autonomous] prd=%s branch refresh merge: %v", prd.ID, err)
		st.LastResult = BranchRefreshError
		m.recordBranchRefreshLogged(prd, st)
	case len(files) > 0:
		st.LastResult = BranchRefreshConflict
		st.Stopped = true
		m.recordBranchRefreshLogged(prd, st)
		m.mu.Lock()
		store := m.decisionStore
		m.mu.Unlock()
		if store == nil {
			log.Printf("[autonomous] prd=%s branch refresh conflict in %s: no question store to ask the operator", prd.ID, strings.Join(files, ", "))
			return
		}
		if _, _, qerr := RaiseBranchRefreshQuestion(store, prd.ID, base, files); qerr != nil {
			log.Printf("[autonomous] prd=%s branch refresh question: %v", prd.ID, qerr)
		}
	default:
		st.LastResult = BranchRefreshClean
		m.recordBranchRefreshLogged(prd, st)
	}
}

func (m *Manager) recordBranchRefreshLogged(prd *PRD, st BranchRefresh) {
	if err := m.RecordBranchRefresh(prd.ID, st); err != nil {
		log.Printf("[autonomous] prd=%s record branch refresh: %v", prd.ID, err)
	}
}
