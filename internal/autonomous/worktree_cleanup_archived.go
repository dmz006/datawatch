// Cleanup of archived Automata.
//
// With autonomous.automation.cleanup_archived on (the default), an archived
// Automaton counts as finished for the cleanup, like a completed or cancelled
// one, and under the same keep-work rules (worktree_cleanup.go,
// task_worktree_keep.go). It removes nothing by itself: cleanup_mode still
// decides. Work that lands after the archive is released by the periodic pass.
//
// One rule protects every finished record, archived or not: a folder or a
// branch that an Automaton which has not finished still points at is kept.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// archivedTaskPassTimeout bounds the archived task step of one periodic pass.
// A pass that runs out of time carries on from where it stopped the next time.
const archivedTaskPassTimeout = 60 * time.Second

// cleanupFinishedStatus reports whether the cleanup treats an Automaton in this
// status as finished.
func (m *Manager) cleanupFinishedStatus(s PRDStatus) bool {
	switch s {
	case PRDCompleted, PRDCancelled:
		return true
	case PRDArchived:
		return m.cleanupArchivedOn()
	}
	return false
}

func (m *Manager) cleanupArchivedOn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Automation.EffectiveCleanupArchived()
}

// ---- folders and branches a later run needs ----

// refInFolder reports whether ref is the folder dir or lies below it.
func refInFolder(ref, dir string) bool {
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(dir) == "" {
		return false
	}
	rel, err := filepath.Rel(evalOrClean(absClean(dir)), evalOrClean(absClean(ref)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// worktreeNeededBy returns the id of an Automaton, other than owner, that has
// not finished and still points at the folder (or below it) or at the branch:
// as its project directory, its work branch, its base branch, or a task's
// directory, worktree or branch. Empty when none does. folder or branch may be
// empty.
func (m *Manager) worktreeNeededBy(owner, folder, branch string) string {
	if m == nil || m.store == nil {
		return ""
	}
	prds := m.store.ListPRDs()
	found := ""
	m.store.WithLock(func() {
		for _, p := range prds {
			if p == nil || p.ID == owner {
				continue
			}
			switch NormalizePRDStatus(p.Status) {
			case PRDCompleted, PRDCancelled, PRDRejected, PRDArchived:
				continue
			}
			if prdPointsAt(p, folder, branch) {
				// The smallest id, so the same state names the same record.
				if found == "" || p.ID < found {
					found = p.ID
				}
			}
		}
	})
	return found
}

func prdPointsAt(p *PRD, folder, branch string) bool {
	if branch != "" && (p.Git.Branch == branch || p.Git.BaseBranch == branch) {
		return true
	}
	if refInFolder(p.ProjectDir, folder) {
		return true
	}
	for si := range p.Story {
		for ti := range p.Story[si].Tasks {
			t := &p.Story[si].Tasks[ti]
			if branch != "" && t.WorktreeBranch == branch {
				return true
			}
			if refInFolder(t.ProjectDir, folder) || refInFolder(t.WorktreePath, folder) {
				return true
			}
		}
	}
	return false
}

// ---- task worktrees and task branches of archived Automata ----

// archivedTaskRefs is one archived Automaton that still records task work.
type archivedTaskRefs struct {
	prd      *PRD
	branches []string
	folders  []string
}

// releaseArchivedTaskWork releases the task worktrees and task branches of
// archived Automata older than the retention window, under the task keep-work
// rules. It is what releases work that landed after the archive. budget is the
// number of removals this periodic pass may still make; in on mode every task
// something is removed for spends one.
func (m *Manager) releaseArchivedTaskWork(ctx context.Context, repo, mode string, retention time.Duration, budget *int) {
	if !m.cleanupArchivedOn() || (mode == config.CleanupModeOn && *budget <= 0) {
		return
	}
	prds := m.store.ListPRDs()
	var cands []archivedTaskRefs
	m.store.WithLock(func() {
		for _, p := range prds {
			if p == nil || p.Status != PRDArchived || p.UpdatedAt.IsZero() || time.Since(p.UpdatedAt) < retention {
				continue
			}
			c := archivedTaskRefs{prd: p}
			for si := range p.Story {
				for ti := range p.Story[si].Tasks {
					t := &p.Story[si].Tasks[ti]
					if t.WorktreeBranch != "" {
						c.branches = append(c.branches, t.WorktreeBranch)
					}
					if t.WorktreePath != "" {
						c.folders = append(c.folders, t.WorktreePath)
					}
				}
			}
			if len(c.branches)+len(c.folders) > 0 {
				cands = append(cands, c)
			}
		}
	})
	if len(cands) == 0 {
		return
	}
	existing, ok := localBranches(ctx, repo)
	sort.Slice(cands, func(i, j int) bool { return cands[i].prd.ID < cands[j].prd.ID })

	// Carry on after the record the last pass stopped at.
	m.mu.Lock()
	cursor := m.archivedTaskCursor
	m.mu.Unlock()
	start := sort.Search(len(cands), func(i int) bool { return cands[i].prd.ID > cursor })
	last, stopped := "", false
	removed := 0
	for k := 0; k < len(cands); k++ {
		if ctx.Err() != nil || (mode == config.CleanupModeOn && *budget <= 0) {
			stopped = true
			break
		}
		c := cands[(start+k)%len(cands)]
		last = c.prd.ID
		// A record whose task branches and folders are all gone has nothing
		// left to release, and costs no further git call.
		if ok && !anyBranch(existing, c.branches) && !anyPathExists(c.folders) {
			continue
		}
		removed += m.cleanupTaskWorktrees(ctx, c.prd, taskCleanupRun{mode: mode, budget: budget})
	}
	if !stopped {
		last = ""
	}
	m.mu.Lock()
	m.archivedTaskCursor = last
	m.mu.Unlock()
	if removed > 0 {
		log.Printf("[autonomous] worktree-cleanup: removed %d task worktree(s) of archived Automata", removed)
	}
}

// localBranches lists the repository's local branches. ok is false when git
// could not say, so nothing is skipped on its account.
func localBranches(ctx context.Context, repo string) (map[string]bool, bool) {
	out, err := runGitInCtx(ctx, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, false
	}
	names := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names[l] = true
		}
	}
	return names, true
}

func anyBranch(existing map[string]bool, branches []string) bool {
	for _, b := range branches {
		if existing[b] {
			return true
		}
	}
	return false
}

func anyPathExists(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

// ---- the landing record of a released archived branch ----

// markArchivedBranchReleased records on an archived Automaton that its work
// landed, after the cleanup removed its work branch on proof that every commit
// was on the base branch. Without it the finished-work audit would read the
// missing branch as lost work. A recorded landing, keep or drop decision
// stands. UpdatedAt is left alone: the retention windows count from it.
func (m *Manager) markArchivedBranchReleased(prd *PRD, branch string) error {
	s := m.store
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.prds[prd.ID]
	if !ok || cur.Status != PRDArchived || cur.Git.Branch != branch {
		return nil
	}
	if cur.Landing != nil && cur.Landing.State != LandingPending {
		return nil
	}
	cur.Landing = &Landing{State: LandingLanded, Actor: "worktree-cleanup", At: time.Now(),
		Reason: "every commit was on the base branch when the cleanup removed the work branch"}
	return s.persistPRD(cur.ID)
}

// ---- a record's own missing worktree ----

// ownWorktreeMissing reports whether dir is the worktree the daemon makes for
// the Automaton id (worktreeDir/id) and nothing is at that path.
func ownWorktreeMissing(id, dir, worktreeDir string) bool {
	if !plainRecordIDRe.MatchString(id) || strings.TrimSpace(dir) == "" {
		return false
	}
	if absClean(dir) != absClean(filepath.Join(worktreeDir, id)) {
		return false
	}
	_, err := os.Lstat(dir)
	return errors.Is(err, os.ErrNotExist)
}

// recreateOwnWorktree makes the Automaton's own worktree again when it was
// cleared: on its work branch, or on a new branch from the base branch when the
// work branch is gone too. Only the daemon's own path is ever created. A
// locked registration, or one holding detached commits that the work branch
// does not have, is left alone and reported.
func (m *Manager) recreateOwnWorktree(prd *PRD) (bool, error) {
	if prd.ProjectProfile != "" || prd.ClusterProfile != "" || prd.Git.Branch == "" {
		return false, nil
	}
	m.mu.Lock()
	baseRepo, worktreeDir := m.cfg.WorktreeBaseRepo, m.cfg.WorktreeDir
	m.mu.Unlock()
	if strings.TrimSpace(worktreeDir) == "" {
		worktreeDir = defaultWorktreeDir(m.dataDir)
	}
	if strings.TrimSpace(baseRepo) == "" || !ownWorktreeMissing(prd.ID, prd.ProjectDir, worktreeDir) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeReleaseGitTimeout)
	defer cancel()
	tip, branchThere, err := branchTip(ctx, baseRepo, prd.Git.Branch)
	if err != nil {
		return false, fmt.Errorf("reading branch %s: %w", prd.Git.Branch, err)
	}
	// Git may still list the folder. Only that one registration is removed,
	// never with force, so a locked one stays.
	wt, listed, err := findLinkedWorktree(ctx, baseRepo, prd.ProjectDir)
	if err != nil {
		return false, fmt.Errorf("reading the worktree list: %w", err)
	}
	if listed {
		if wt.detached {
			onBranch := false
			if branchThere {
				_, aerr := taskGitOut(ctx, baseRepo, "merge-base", "--is-ancestor", wt.head, tip)
				onBranch = aerr == nil
			}
			if !onBranch {
				return false, fmt.Errorf("the missing worktree %s had a detached HEAD %s that is not on branch %s; keep that commit, then run git worktree remove on the path", prd.ProjectDir, wt.head, prd.Git.Branch)
			}
		}
		if out, rerr := runGitInCtx(ctx, baseRepo, "worktree", "remove", wt.path); rerr != nil {
			return false, fmt.Errorf("clearing the missing worktree %s: %s: %w", wt.path, strings.TrimSpace(out), rerr)
		}
	}
	path, _, err := EnsureWorktree(prd, baseRepo, worktreeDir)
	if err != nil {
		return false, err
	}
	how := "on its work branch " + prd.Git.Branch
	if !branchThere {
		how = "on a new branch " + prd.Git.Branch + " from the base branch, because the work branch was gone"
	}
	note := fmt.Sprintf("the Automaton's own worktree %s was missing and was created again %s", path, how)
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "worktree_recreated", Actor: "autonomous", Note: note})
	})
	log.Printf("[autonomous] prd=%s %s", prd.ID, note)
	return true, nil
}
