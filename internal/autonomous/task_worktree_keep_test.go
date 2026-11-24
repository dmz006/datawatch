package autonomous

// Closing an Automaton must never remove a task worktree or a task branch that
// holds work. Every test here works in throwaway repositories.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type keepFixture struct {
	m         *Manager
	audit     *recordedAudit
	repo      string
	wtDir     string
	prd       *PRD
	task      *Task
	path      string // the task worktree
	branch    string // the task branch
	cleanedUp bool
}

// newKeepFixture builds a throwaway repository (default branch main), an
// Automaton worktree and one task worktree, with the Automaton in the store.
func newKeepFixture(t *testing.T, mode string) *keepFixture {
	t.Helper()
	repo, wtDir, prd := prdWorktreeFixture(t)
	m, audit := buildManager(t, repo, mode, 0)
	on := true
	m.mu.Lock()
	m.cfg.WorktreeDir = wtDir
	m.cfg.Parallel.WorktreePerTask = &on
	m.mu.Unlock()
	prd.Status = PRDRunning
	prd.Story = []Story{{ID: "s1", PRDID: prd.ID, Title: "story", Tasks: []Task{
		{ID: "task0001", StoryID: "s1", PRDID: prd.ID, Title: "do it", Status: TaskCompleted},
	}}}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	task := &prd.Story[0].Tasks[0]
	if err := m.ensureTaskWorktreeFor(prd, task); err != nil {
		t.Fatalf("ensureTaskWorktreeFor: %v", err)
	}
	if task.WorktreePath == "" {
		t.Fatal("expected a task worktree")
	}
	gitRun(t, repo, "config", "user.email", "t@datawatch")
	gitRun(t, repo, "config", "user.name", "test")
	return &keepFixture{m: m, audit: audit, repo: repo, wtDir: wtDir, prd: prd, task: task,
		path: task.WorktreePath, branch: task.WorktreeBranch}
}

func keepWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit makes one commit in the task worktree and returns its id.
func (f *keepFixture) commit(t *testing.T, rel string) string {
	t.Helper()
	keepWrite(t, f.path, rel, "work in "+rel+"\n")
	gitRun(t, f.path, "add", rel)
	gitRun(t, f.path, "commit", "-q", "-m", "task work "+rel)
	return gitOut(t, f.path, "rev-parse", "HEAD")
}

// land puts the task branch on the default branch.
func (f *keepFixture) land(t *testing.T) {
	t.Helper()
	gitRun(t, f.repo, "merge", "-q", "--no-ff", "--no-edit", f.branch)
}

func (f *keepFixture) cancel(t *testing.T) {
	t.Helper()
	if err := NewAPI(f.m).Cancel(f.prd.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func (f *keepFixture) mustHaveWorktree(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(f.path, ".git")); err != nil {
		t.Fatalf("the task worktree was removed: %v", err)
	}
}

func (f *keepFixture) mustNotHaveWorktree(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("the task worktree is still there (err=%v)", err)
	}
}

func (f *keepFixture) hasBranch(t *testing.T) bool {
	t.Helper()
	return gitOut(t, f.repo, "branch", "--list", f.branch) != ""
}

func (f *keepFixture) backupSHAs(t *testing.T) string {
	t.Helper()
	return gitOut(t, f.repo, "for-each-ref", "--format=%(objectname)", "refs/datawatch/reap-backup/")
}

// auditWith returns the audit records whose details name the task branch.
func (f *keepFixture) auditWith(action string) []map[string]any {
	f.audit.mu.Lock()
	defer f.audit.mu.Unlock()
	var out []map[string]any
	for _, e := range f.audit.items {
		if e.action == action && e.details["branch"] == f.branch {
			out = append(out, e.details)
		}
	}
	return out
}

// cleanupSummary reads the Automaton the way a REST reply carries it.
func (f *keepFixture) cleanupSummary(t *testing.T) (kept, branchesKept int, reasons map[string]int) {
	t.Helper()
	prd, ok := f.m.Store().GetPRD(f.prd.ID)
	if !ok {
		t.Fatal("Automaton gone")
	}
	raw, err := json.Marshal(prd)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		C *struct {
			Kept         int            `json:"worktrees_kept"`
			BranchesKept int            `json:"branches_kept"`
			Reasons      map[string]int `json:"reasons"`
		} `json:"task_worktree_cleanup"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.C == nil {
		t.Fatalf("the Automaton carries no task_worktree_cleanup summary")
	}
	return view.C.Kept, view.C.BranchesKept, view.C.Reasons
}

func TestCancelKeepsDirtyTaskWorktree(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{"untracked", "notes.txt"},
		{"modified tracked", ".gitignore"},
		{"untracked in a new folder", "pkg/new/code.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newKeepFixture(t, config.CleanupModeOn)
			keepWrite(t, f.path, tc.file, "the only copy\n")
			f.cancel(t)
			f.mustHaveWorktree(t)
			if got, err := os.ReadFile(filepath.Join(f.path, tc.file)); err != nil || string(got) != "the only copy\n" {
				t.Fatalf("uncommitted file lost: %q %v", got, err)
			}
			if !f.hasBranch(t) {
				t.Fatal("the branch of a dirty worktree was deleted")
			}
			if f.task.WorktreePath != f.path || f.task.WorktreeBranch != f.branch {
				t.Fatalf("the task lost its worktree record: %q %q", f.task.WorktreePath, f.task.WorktreeBranch)
			}
		})
	}
}

// An empty new file holds nothing the daemon wrote, so it is the task's own.
func TestCancelKeepsWorktreeWithEmptyUntrackedFile(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	keepWrite(t, f.path, "pkg/__init__.py", "")
	f.cancel(t)
	f.mustHaveWorktree(t)
}

// A staged change must be kept even when the file on disk matches HEAD again.
func TestCancelKeepsWorktreeWithStagedOnlyWork(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	keepWrite(t, f.path, "CLAUDE.md", "rules\n")
	gitRun(t, f.path, "add", "CLAUDE.md")
	gitRun(t, f.path, "commit", "-q", "-m", "rules")
	f.land(t)
	keepWrite(t, f.path, "CLAUDE.md", "rules\nstaged work\n")
	gitRun(t, f.path, "add", "CLAUDE.md")
	keepWrite(t, f.path, "CLAUDE.md", "rules\n")
	f.cancel(t)
	f.mustHaveWorktree(t)
}

// status.showUntrackedFiles=no must not hide untracked work from the check.
func TestCancelSeesUntrackedFilesDespiteGitConfig(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	gitRun(t, f.repo, "config", "status.showUntrackedFiles", "no")
	keepWrite(t, f.path, "notes.txt", "the only copy\n")
	f.cancel(t)
	f.mustHaveWorktree(t)
}

func TestCancelKeepsUnmergedTaskBranch(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	tip := f.commit(t, "feature.txt")
	f.cancel(t)
	if !f.hasBranch(t) {
		t.Fatal("a task branch with commits that are not on the default branch was deleted")
	}
	if got := gitOut(t, f.repo, "rev-parse", f.branch); got != tip {
		t.Fatalf("branch tip %s, want %s", got, tip)
	}
	if f.task.WorktreeBranch != f.branch {
		t.Fatalf("the task forgot its kept branch: %q", f.task.WorktreeBranch)
	}
	// The worktree was clean: it goes, the branch keeps every commit.
	f.mustNotHaveWorktree(t)
	if f.task.WorktreePath != "" {
		t.Fatalf("removed worktree still recorded: %q", f.task.WorktreePath)
	}
	got := f.auditWith("autonomous_task_branch_kept")
	if len(got) != 1 || got[0]["branch_tip"] != tip {
		t.Fatalf("refusal audit = %v, want one record with tip %s", got, tip)
	}
}

// Merged into the Automaton branch is not enough while that branch itself is
// not on the default branch.
func TestCancelKeepsBranchMergedOnlyIntoUnlandedAutomatonBranch(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	f.commit(t, "feature.txt")
	gitRun(t, f.prd.ProjectDir, "merge", "-q", "--no-ff", "--no-edit", f.branch)
	// The finished-work guard asks for a decision before this cancel; even a
	// recorded drop must not delete the task branch.
	if err := NewAPI(f.m).CancelDecided(f.prd.ID, "drop", "test"); err != nil {
		t.Fatalf("CancelDecided: %v", err)
	}
	if !f.hasBranch(t) {
		t.Fatal("task branch deleted although its commits are not on the default branch")
	}
}

// git cherry ignores merge commits, so content that lives only in a merge
// commit must still keep the branch.
func TestCancelKeepsBranchWithOwnMergeCommit(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	side := f.commit(t, "side.txt")
	gitRun(t, f.repo, "cherry-pick", side) // the plain commit is on main by patch
	gitRun(t, f.path, "reset", "-q", "--hard", "HEAD~1")
	gitRun(t, f.path, "merge", "-q", "--no-ff", "--no-commit", side)
	keepWrite(t, f.path, "only-in-merge.txt", "resolution work\n")
	gitRun(t, f.path, "add", "only-in-merge.txt")
	gitRun(t, f.path, "commit", "-q", "-m", "merge with its own content")
	f.cancel(t)
	if !f.hasBranch(t) {
		t.Fatal("a branch whose merge commit holds its own content was deleted")
	}
}

func TestCancelKeepsDetachedWorktreeCommits(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	gitRun(t, f.path, "checkout", "-q", "--detach")
	sha := f.commit(t, "detached.txt")
	f.cancel(t)
	f.mustHaveWorktree(t)
	if got := gitOut(t, f.path, "rev-parse", "HEAD"); got != sha {
		t.Fatalf("detached HEAD %s, want %s", got, sha)
	}
}

func TestCancelKeepsDirectoryItCannotVerify(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	if err := os.Remove(filepath.Join(f.path, ".git")); err != nil {
		t.Fatal(err)
	}
	keepWrite(t, f.path, "notes.txt", "the only copy\n")
	f.cancel(t)
	if _, err := os.Stat(filepath.Join(f.path, "notes.txt")); err != nil {
		t.Fatalf("file lost: %v", err)
	}
	if !f.hasBranch(t) {
		t.Fatal("branch deleted")
	}
}

func TestTaskCleanupReportModeRemovesNothing(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeReport)
	f.commit(t, "feature.txt")
	f.land(t) // would be removable in "on" mode
	f.prd.Status = PRDCancelled
	if n := f.m.CleanupTaskWorktrees(context.Background(), f.prd); n != 0 {
		t.Fatalf("report mode removed %d", n)
	}
	f.mustHaveWorktree(t)
	if !f.hasBranch(t) {
		t.Fatal("report mode deleted the branch")
	}
	if refs := f.backupSHAs(t); refs != "" {
		t.Fatalf("report mode wrote refs: %s", refs)
	}
	if got := f.auditWith("autonomous_task_worktree_would_remove"); len(got) != 1 {
		t.Fatalf("would-remove audit = %v", got)
	}
}

func TestTaskCleanupOffModeRemovesNothing(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOff)
	f.prd.Status = PRDCancelled
	if n := f.m.CleanupTaskWorktrees(context.Background(), f.prd); n != 0 {
		t.Fatalf("off mode removed %d", n)
	}
	f.mustHaveWorktree(t)
	if !f.hasBranch(t) {
		t.Fatal("off mode deleted the branch")
	}
}

func TestRemovedTaskBranchHasBackupRef(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	tip := f.commit(t, "feature.txt")
	f.land(t)
	f.cancel(t)
	f.mustNotHaveWorktree(t)
	if f.hasBranch(t) {
		t.Fatal("a landed task branch should be deleted")
	}
	if got := f.backupSHAs(t); got != tip {
		t.Fatalf("backup refs = %q, want the branch tip %s", got, tip)
	}
	if f.task.WorktreePath != "" || f.task.WorktreeBranch != "" {
		t.Fatalf("record not cleared: %q %q", f.task.WorktreePath, f.task.WorktreeBranch)
	}
	for _, action := range []string{"autonomous_task_worktree_remove", "autonomous_task_branch_delete"} {
		got := f.auditWith(action)
		if len(got) != 1 || got[0]["branch_tip"] != tip {
			t.Fatalf("%s audit = %v, want one record with tip %s", action, got, tip)
		}
	}
	if ref, _ := f.auditWith("autonomous_task_branch_delete")[0]["backup_ref"].(string); !strings.HasPrefix(ref, "refs/datawatch/reap-backup/") {
		t.Fatalf("delete audit names no backup ref: %q", ref)
	}
}

// A backup ref written earlier is never overwritten by a later one.
func TestTaskBranchBackupNeverOverwritesAnEarlierBackup(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	old := gitOut(t, f.repo, "rev-parse", "HEAD")
	tip := f.commit(t, "feature.txt")
	f.land(t)
	// Occupy the names a backup made in the next few seconds would take.
	now := f.m.holdNow().Unix()
	var taken []string
	for i := int64(0); i < 5; i++ {
		ref := reapBackupRef(f.branch, now+i)
		gitRun(t, f.repo, "update-ref", ref, old)
		taken = append(taken, ref)
	}
	f.cancel(t)
	for _, ref := range taken {
		if got := gitOut(t, f.repo, "rev-parse", ref); got != old {
			t.Fatalf("earlier backup %s overwritten: %s", ref, got)
		}
	}
	if !strings.Contains(f.backupSHAs(t), tip) {
		t.Fatalf("no backup of the deleted tip %s", tip)
	}
}

// The daemon's own generated and tooling files do not make a worktree dirty.
func TestCancelRemovesWorktreeWithOnlyDaemonFiles(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	for _, rel := range []string{".mcp.json", "worker-mcp.json", "worker-starting-context.md", "CHECKPOINT.md", ".datawatch/skills/a/SKILL.md"} {
		keepWrite(t, f.path, rel, "daemon\n")
	}
	f.cancel(t)
	f.mustNotHaveWorktree(t)
}

func TestCancelResultSaysWhatWasKeptAndWhy(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	keepWrite(t, f.path, "notes.txt", "the only copy\n")
	f.cancel(t)
	kept, _, reasons := f.cleanupSummary(t)
	if kept != 1 || reasons["uncommitted_work"] != 1 {
		t.Fatalf("kept=%d reasons=%v, want 1 uncommitted_work", kept, reasons)
	}
	got := f.auditWith("autonomous_task_worktree_kept")
	if len(got) != 1 || got[0]["reason"] != "uncommitted_work" || got[0]["branch_tip"] == "" {
		t.Fatalf("refusal audit = %v", got)
	}
	// The archive result carries it too.
	if _, err := f.m.ArchiveWithOutcome(f.prd.ID, "operator", OutcomeInput{Outcome: OutcomeDropped}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	f.mustHaveWorktree(t)
	kept, _, reasons = f.cleanupSummary(t)
	if kept != 1 || reasons["uncommitted_work"] != 1 {
		t.Fatalf("after archive kept=%d reasons=%v", kept, reasons)
	}
}

func TestCancelResultInOffModeCountsKeptWorktrees(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOff)
	f.cancel(t)
	f.mustHaveWorktree(t)
	kept, _, reasons := f.cleanupSummary(t)
	if kept != 1 || reasons["cleanup_off"] != 1 {
		t.Fatalf("kept=%d reasons=%v, want 1 cleanup_off", kept, reasons)
	}
	if got := f.auditWith("autonomous_task_worktree_kept"); len(got) != 0 {
		t.Fatalf("off mode must not act or audit: %v", got)
	}
}

// A kept branch is looked at again, and goes (with a backup) once it landed.
func TestKeptTaskBranchIsDeletedOnceItLands(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	tip := f.commit(t, "feature.txt")
	f.cancel(t)
	if !f.hasBranch(t) {
		t.Fatal("unmerged branch deleted")
	}
	_, branchesKept, reasons := f.cleanupSummary(t)
	if branchesKept != 1 || reasons["unmerged_branch"] != 1 {
		t.Fatalf("branches kept=%d reasons=%v", branchesKept, reasons)
	}
	// A second pass records the same refusal once and keeps the summary.
	f.m.ReleaseAutomatonWorktree(f.prd)
	if got := f.auditWith("autonomous_task_branch_kept"); len(got) != 1 {
		t.Fatalf("the same refusal was audited %d times", len(got))
	}
	if _, branchesKept, _ = f.cleanupSummary(t); branchesKept != 1 {
		t.Fatalf("a second pass lost the kept branch from the summary: %d", branchesKept)
	}
	f.land(t)
	f.m.ReleaseAutomatonWorktree(f.prd)
	if f.hasBranch(t) {
		t.Fatal("landed branch not deleted")
	}
	if got := f.backupSHAs(t); got != tip {
		t.Fatalf("backup refs = %q, want %s", got, tip)
	}
}
