package autonomous

// Removing an archived branch proves nothing about where its work went, and
// archiving itself releases nothing new. Throwaway repositories only.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// auditCause runs the finished-work audit and returns the cause it reports for
// the Automaton, or "" when it reports nothing.
func auditCause(t *testing.T, m *Manager, id string) string {
	t.Helper()
	cfg := m.Config()
	cfg.FinishedWork = FinishedWorkShadow
	m.SetConfig(cfg)
	res, err := m.RunFinishedWorkAudit(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, f := range res.Findings {
		if f.Automaton == id {
			return f.Cause
		}
	}
	return ""
}

func completedTaskStory(prd *PRD, branch string) []Story {
	return []Story{{ID: "s1", PRDID: prd.ID, Title: "story", Tasks: []Task{
		{ID: "t1", StoryID: "s1", PRDID: prd.ID, Title: "change the code", Status: TaskCompleted, WorktreeBranch: branch},
	}}}
}

// removeArchivedWorkBranch saves the record, runs one periodic pass and
// requires that it removed the work branch.
func removeArchivedWorkBranch(t *testing.T, m *Manager, repo string, prd *PRD) *PRD {
	t.Helper()
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	m.store.WithLock(func() { prd.UpdatedAt = old48h() })
	res := m.CleanupWorktreesPass()
	if len(res.Removed) != 1 || branchExists(t, repo, archivedTestBranch) {
		t.Fatalf("removed=%+v skipped=%+v failed=%+v", res.Removed, res.Skipped, res.Failed)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("record gone")
	}
	return got
}

func mustNotReadLanded(t *testing.T, prd *PRD) {
	t.Helper()
	if prd.Landing != nil && prd.Landing.State == LandingLanded {
		t.Fatalf("the cleanup recorded the work as landed without proof: %+v", prd.Landing)
	}
}

func TestArchivedRemovalOfABranchThatNeverRanRecordsNoLanding(t *testing.T) {
	m, _, repo, _, prd := archivedPassFixture(t, config.CleanupModeOn)
	prd.Landing = &Landing{State: LandingPending, Actor: "autonomous", At: old48h()}
	got := removeArchivedWorkBranch(t, m, repo, prd)
	mustNotReadLanded(t, got)
	if got.Landing == nil || got.Landing.State != LandingPending {
		t.Fatalf("the landing record changed: %+v", got.Landing)
	}
	// Nothing was ever done, so the audit has nothing to report.
	if cause := auditCause(t, m, prd.ID); cause != "" {
		t.Fatalf("audit cause = %q", cause)
	}
}

func TestArchivedRemovalOfDroppedWorkRecordsNoLanding(t *testing.T) {
	m, _, repo, _, prd := archivedPassFixture(t, config.CleanupModeOn)
	prd.ClosedOutcome = OutcomeDropped
	prd.Story = completedTaskStory(prd, "")
	got := removeArchivedWorkBranch(t, m, repo, prd)
	if cause := auditCause(t, m, prd.ID); cause != FinishedWorkBranchGone {
		t.Errorf("audit cause = %q, want %q", cause, FinishedWorkBranchGone)
	}
	mustNotReadLanded(t, got)
	if got.Landing != nil {
		t.Fatalf("a landing record was written: %+v", got.Landing)
	}
}

func TestArchivedWorkOnlyOnAnUnmergedTaskBranchStaysFlagged(t *testing.T) {
	m, _, repo, _, prd := archivedPassFixture(t, config.CleanupModeOn)
	const taskBranch = "automaton/fixture-t1"
	other := t.TempDir() + "/task"
	gitCmd(t, repo, "worktree", "add", "-q", "-b", taskBranch, other, "trunk")
	keepWrite(t, other, "feature.txt", "requested work\n")
	gitCmd(t, other, "add", "feature.txt")
	gitCmd(t, other, "commit", "-q", "-m", "task work")
	gitCmd(t, repo, "worktree", "remove", other)
	prd.Story = completedTaskStory(prd, taskBranch)

	got := removeArchivedWorkBranch(t, m, repo, prd)

	if !branchExists(t, repo, taskBranch) {
		t.Fatal("the never merged task branch was deleted")
	}
	if cause := auditCause(t, m, prd.ID); cause != FinishedWorkBranchGone {
		t.Errorf("audit cause = %q, want %q", cause, FinishedWorkBranchGone)
	}
	mustNotReadLanded(t, got)
}

func TestArchivedWorkMergedButNotPushedStaysFlagged(t *testing.T) {
	m, _, repo, worktree, prd := archivedPassFixture(t, config.CleanupModeOn)
	keepWrite(t, worktree, "feature.txt", "requested work\n")
	gitCmd(t, worktree, "add", "feature.txt")
	gitCmd(t, worktree, "commit", "-q", "-m", "work")
	gitCmd(t, repo, "merge", "-q", "--ff-only", archivedTestBranch) // local only
	if n := strings.TrimSpace(gitCmd(t, repo, "rev-list", "--count", "origin/trunk..trunk")); n != "1" {
		t.Fatalf("fixture: %s commit(s) on the local default branch only, want 1", n)
	}
	prd.Landing = &Landing{State: LandingPending, Actor: "autonomous", At: old48h()}
	prd.Story = completedTaskStory(prd, "")
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if m.CheckLandingAncestor(context.Background(), prd.ID) {
		t.Fatal("fixture: datawatch's own landing check says landed")
	}

	got := removeArchivedWorkBranch(t, m, repo, prd)

	if cause := auditCause(t, m, prd.ID); cause != FinishedWorkBranchGone {
		t.Errorf("audit cause = %q, want %q", cause, FinishedWorkBranchGone)
	}
	mustNotReadLanded(t, got)
}

// Archived work is released by the periodic pass alone, with its retention
// wait: neither archiving nor an end-path release takes anything.
func TestArchivingReleasesNoBranchAndNoTaskWorktree(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	f.commit(t, "feature.txt")
	f.land(t)
	f.archive(t)
	f.mustHaveWorktree(t)
	if !f.hasBranch(t) || !branchExists(t, f.repo, f.prd.Git.Branch) {
		t.Fatal("archiving deleted a branch")
	}
	if _, _, reasons := f.cleanupSummary(t); reasons[taskKeepByArchive] != 1 {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestEndPathReleaseLeavesAnArchivedRecordToThePeriodicPass(t *testing.T) {
	m, audit, repo, _, prd := archivedPassFixture(t, config.CleanupModeOn)
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, repo, archivedTestBranch) {
		t.Fatal("an end-path release deleted the branch of an archived Automaton")
	}
	if n := len(audit.events(worktreeCleanupAuditAction)); n != 0 {
		t.Fatalf("removals audited: %d", n)
	}
}
