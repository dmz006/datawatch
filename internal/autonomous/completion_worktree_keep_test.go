package autonomous

// Completion with an automatic pull request releases the Automaton worktree.
// That release must never remove a worktree that holds work. Every test here
// works in throwaway repositories with a local bare remote.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
)

type completionFixture struct {
	m      *Manager
	audit  *recordedAudit
	repo   string // the base repository
	wtDir  string
	path   string // the Automaton worktree
	branch string
	prd    *PRD
	pr     *fakeOpenPRProvider
}

func newCompletionFixture(t *testing.T, mode string) *completionFixture {
	t.Helper()
	path, branch, _ := gitCompletionFixture(t)
	common := gitOut(t, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	repo := filepath.Dir(common)
	gitRun(t, repo, "config", "user.email", "t@datawatch")
	gitRun(t, repo, "config", "user.name", "test")
	m, audit := buildManager(t, repo, mode, 0)
	wtDir := t.TempDir()
	m.mu.Lock()
	m.cfg.WorktreeDir = wtDir
	m.mu.Unlock()
	pr := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return pr }
	prd := &PRD{ID: "abc12345", Title: "Add widgets", ProjectDir: path, Status: PRDCompleted,
		Git: GitWorkflow{Branch: branch, BaseBranch: "main", AutoPR: true}, UpdatedAt: time.Now()}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return &completionFixture{m: m, audit: audit, repo: repo, wtDir: wtDir, path: path, branch: branch, prd: prd, pr: pr}
}

// complete runs the completion step and checks it got as far as the release.
func (f *completionFixture) complete(t *testing.T) {
	t.Helper()
	f.m.handleWorktreeCompletion(f.prd)
	if len(f.pr.calls) != 1 {
		t.Fatalf("expected the pull request to be opened once, got %d", len(f.pr.calls))
	}
}

func (f *completionFixture) mustHold(t *testing.T, rel, body string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(f.path, rel))
	if err != nil || string(got) != body {
		t.Fatalf("%s lost from the Automaton worktree: %q %v", rel, got, err)
	}
}

func (f *completionFixture) mustHaveWorktree(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(f.path, ".git")); err != nil {
		t.Fatalf("the Automaton worktree was removed: %v", err)
	}
}

func (f *completionFixture) events(action string) []map[string]any {
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

func (f *completionFixture) mustAuditKept(t *testing.T, reason string) map[string]any {
	t.Helper()
	got := f.events("autonomous_completion_worktree_kept")
	if len(got) != 1 || got[0]["reason"] != reason {
		t.Fatalf("kept audit = %v, want one record with reason %s", got, reason)
	}
	return got[0]
}

func (f *completionFixture) refs(t *testing.T) string {
	t.Helper()
	return gitOut(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/", "refs/datawatch/")
}

func TestCompletionKeepsWorktreeWithOwnUncommittedFiles(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{"untracked", "notes.txt"},
		{"untracked in a new folder", "pkg/new/code.go"},
		{"modified tracked", "tracked.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompletionFixture(t, config.CleanupModeOn)
			if tc.name == "modified tracked" {
				keepWrite(t, f.path, tc.file, "v1\n")
				gitRun(t, f.path, "add", tc.file)
				gitRun(t, f.path, "commit", "-q", "-m", "add tracked")
			}
			keepWrite(t, f.path, tc.file, "the only copy\n")
			tip := gitOut(t, f.repo, "rev-parse", f.branch)
			f.complete(t)
			f.mustHold(t, tc.file, "the only copy\n")
			got := f.mustAuditKept(t, "uncommitted_work")
			if got["branch_tip"] != tip {
				t.Fatalf("audit tip = %v, want %s", got["branch_tip"], tip)
			}
			if files, _ := got["files"].([]string); len(files) == 0 {
				t.Fatalf("audit names no files: %v", got)
			}
		})
	}
}

func TestCompletionOffModeRemovesNothing(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOff)
	f.complete(t)
	f.mustHaveWorktree(t)
	f.mustAuditKept(t, "cleanup_off")
}

func TestCompletionReportModeRemovesNothing(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeReport)
	before := f.refs(t)
	f.complete(t)
	f.mustHaveWorktree(t)
	if got := f.events("autonomous_completion_worktree_would_remove"); len(got) != 1 {
		t.Fatalf("would-remove audit = %v", got)
	}
	if after := f.refs(t); after != before {
		t.Fatalf("refs changed in report mode:\n%s\n---\n%s", before, after)
	}
}

func TestCompletionReportModeNamesDirtyWorktreeAsKeptForItsWork(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeReport)
	keepWrite(t, f.path, "notes.txt", "the only copy\n")
	f.complete(t)
	f.mustHaveWorktree(t)
	f.mustAuditKept(t, "uncommitted_work")
}

func TestCompletionKeepsWorktreeWithLiveSession(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	f.m.SetSessionListerFn(func() []SessionInfo { return []SessionInfo{{Alive: true, ProjectDir: f.path}} })
	f.complete(t)
	f.mustHaveWorktree(t)
	f.mustAuditKept(t, "live_session")
}

func TestCompletionKeepsDetachedCommits(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	gitRun(t, f.path, "checkout", "-q", "--detach")
	keepWrite(t, f.path, "detached.txt", "only here\n")
	gitRun(t, f.path, "add", "detached.txt")
	gitRun(t, f.path, "commit", "-q", "-m", "detached")
	f.complete(t)
	f.mustHaveWorktree(t)
	f.mustAuditKept(t, "detached_commits")
}

func TestCompletionKeepsWorktreeGitRefusesToRemove(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	gitRun(t, f.repo, "worktree", "lock", f.path)
	f.complete(t)
	f.mustHaveWorktree(t)
	f.mustAuditKept(t, "remove_failed")
}

func TestCompletionRemovesCleanWorktreeAndKeepsTheBranch(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	before := f.refs(t)
	tip := gitOut(t, f.repo, "rev-parse", f.branch)
	f.complete(t)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("a clean worktree should be removed in on mode (err=%v)", err)
	}
	if after := f.refs(t); after != before {
		t.Fatalf("this step deletes no branch and writes no ref:\n%s\n---\n%s", before, after)
	}
	got := f.events("autonomous_completion_worktree_remove")
	if len(got) != 1 || got[0]["branch_tip"] != tip {
		t.Fatalf("removal audit = %v, want one record with tip %s", got, tip)
	}
}

func TestCompletionRemovesWorktreeWithOnlyDaemonFiles(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	for _, rel := range []string{".mcp.json", "worker-mcp.json", "CHECKPOINT.md", treeLockRelPath, ".datawatch/verify/x.sh"} {
		keepWrite(t, f.path, rel, "daemon\n")
	}
	f.complete(t)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("daemon files alone must not keep the worktree (err=%v): %v", err, f.events("autonomous_completion_worktree_kept"))
	}
	saved := filepath.Join(savedVerifyScriptsDir(f.wtDir, f.prd.ID), "x.sh")
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("the verification script must be saved before the removal: %v", err)
	}
}

// With status.showUntrackedFiles=no, git's own unforced "worktree remove"
// deletes untracked files without a word. No removal path may rely on it.
func hiddenUntracked(t *testing.T, mode string) *completionFixture {
	t.Helper()
	f := newCompletionFixture(t, mode)
	gitRun(t, f.repo, "config", "status.showUntrackedFiles", "no")
	keepWrite(t, f.path, "notes.txt", "the only copy\n")
	if out := gitOut(t, f.path, "status", "--porcelain"); strings.Contains(out, "notes.txt") {
		t.Fatalf("fixture: the file should be hidden from plain status: %q", out)
	}
	return f
}

func TestCompletionSeesUntrackedFilesDespiteGitConfig(t *testing.T) {
	f := hiddenUntracked(t, config.CleanupModeOn)
	f.complete(t)
	f.mustHold(t, "notes.txt", "the only copy\n")
	f.mustAuditKept(t, "uncommitted_work")
}

// The release that runs right after completion.
func TestReleaseKeepsAutomatonWorktreeWithHiddenUntrackedFiles(t *testing.T) {
	f := hiddenUntracked(t, config.CleanupModeOn)
	f.m.ReleaseAutomatonWorktree(f.prd)
	f.mustHold(t, "notes.txt", "the only copy\n")
}

// The cleanup the reject and archive steps run.
func TestOrphanCleanupKeepsAutomatonWorktreeWithHiddenUntrackedFiles(t *testing.T) {
	f := hiddenUntracked(t, config.CleanupModeOn)
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHold(t, "notes.txt", "the only copy\n")
}
