package autonomous

// No work may be lost when a worktree or branch is cleaned up: real files in an
// ignored folder keep their worktree, the reject and archive cleanup follows
// cleanup_mode, and an Automaton branch is backed up before it is deleted.
// Every test here works in throwaway repositories.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// ignoreDatawatch makes .datawatch/ an ignored folder in repo and every work
// tree of it, so git status does not show what is inside.
func ignoreDatawatch(t *testing.T, repo string) {
	t.Helper()
	p := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(old, []byte("\n.datawatch/\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// parkedCases are the ignored folders a daemon step or a task leaves real files
// in.
var parkedCases = []struct{ name, file string }{
	{"files moved aside before a task merge", ".datawatch/merge-aside/task0001/pkg/notes.txt"},
	{"read-only task report", ".datawatch/reports/audit.md"},
	{"dependency update results", ".datawatch/dependency-update/results.json"},
	{"supervisor handover", ".datawatch/supervisor/nightly-handover.md"},
	{"verification helper no save copies", ".datawatch/verify/lib/helper.sh"},
}

func mustStillHold(t *testing.T, dir, rel, body string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil || string(got) != body {
		t.Fatalf("%s lost: %q %v", rel, got, err)
	}
}

func TestCompletionKeepsWorktreeWithParkedWork(t *testing.T) {
	for _, tc := range parkedCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompletionFixture(t, config.CleanupModeOn)
			ignoreDatawatch(t, f.repo)
			keepWrite(t, f.path, tc.file, "the only copy\n")
			f.complete(t)
			mustStillHold(t, f.path, tc.file, "the only copy\n")
			got := f.mustAuditKept(t, "uncommitted_work")
			if files, _ := got["files"].([]string); !strings.Contains(strings.Join(files, "\n"), tc.file) {
				t.Fatalf("audit does not name %s: %v", tc.file, got["files"])
			}
		})
	}
}

// The real daemon step: an untracked file the task merge would overwrite is
// moved aside, and the completion must not delete it with the worktree.
func TestCompletionKeepsFileTheTaskMergeMovedAside(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	ignoreDatawatch(t, f.repo)
	side := filepath.Join(t.TempDir(), "side")
	gitRun(t, f.repo, "worktree", "add", "-q", "-b", "side", side, f.branch)
	keepWrite(t, side, "dup.txt", "from the task branch\n")
	gitRun(t, side, "add", "dup.txt")
	gitRun(t, side, "commit", "-q", "-m", "task adds dup.txt")
	keepWrite(t, f.path, "dup.txt", "the only copy\n")
	_, aside, err := clearOverwritableUntracked(context.Background(), f.path, "side", "task0001")
	if err != nil || len(aside) != 1 {
		t.Fatalf("the merge step should move dup.txt aside: %v %v", aside, err)
	}
	f.complete(t)
	mustStillHold(t, f.path, ".datawatch/merge-aside/task0001/dup.txt", "the only copy\n")
}

// The daemon's own gate report, and files git tracks, do not keep a worktree.
func TestCompletionRemovesWorktreeWithOnlyDisposableIgnoredFiles(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	keepWrite(t, f.path, ".datawatch/reports/committed.md", "tracked\n")
	gitRun(t, f.path, "add", "-f", ".datawatch/reports/committed.md")
	gitRun(t, f.path, "commit", "-q", "-m", "a tracked report")
	ignoreDatawatch(t, f.repo)
	for _, rel := range []string{defaultGateReportPath, ".datawatch/judge/p.md", ".datawatch/skills/a/SKILL.md", treeLockRelPath} {
		keepWrite(t, f.path, rel, "daemon\n")
	}
	f.complete(t)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("nothing here is work (err=%v): %v", err, f.events("autonomous_completion_worktree_kept"))
	}
}

func TestCancelKeepsTaskWorktreeWithParkedWork(t *testing.T) {
	for _, tc := range append(parkedCases, struct{ name, file string }{"new verification script", ".datawatch/verify/mine.sh"}) {
		t.Run(tc.name, func(t *testing.T) {
			f := newKeepFixture(t, config.CleanupModeOn) // this repository ignores .datawatch/
			keepWrite(t, f.path, tc.file, "the only copy\n")
			f.cancel(t)
			mustStillHold(t, f.path, tc.file, "the only copy\n")
			if _, _, reasons := f.cleanupSummary(t); reasons["uncommitted_work"] != 1 {
				t.Fatalf("reasons = %v", reasons)
			}
		})
	}
}

func TestCancelRemovesTaskWorktreeWhoseScriptHasACopy(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	keepWrite(t, f.prd.ProjectDir, ".datawatch/verify/check.sh", "same\n")
	keepWrite(t, f.path, ".datawatch/verify/check.sh", "same\n")
	f.cancel(t)
	f.mustNotHaveWorktree(t)
}

// ---- the release step and the periodic worktrees pass ----

type releaseFixture struct {
	m        *Manager
	audit    *recordedAudit
	repo     string
	worktree string
	prd      *PRD
	tip      string
}

const releaseBranch = "automaton/fixture"

// newReleaseFixture is a completed Automaton whose branch has one commit that
// has landed on the default branch (trunk).
func newReleaseFixture(t *testing.T, mode string) *releaseFixture {
	t.Helper()
	repo, worktree := fixtureRepo(t)
	keepWrite(t, worktree, "feature.txt", "feature\n")
	gitCmd(t, worktree, "add", "feature.txt")
	gitCmd(t, worktree, "commit", "-q", "-m", "feature")
	gitCmd(t, repo, "merge", "-q", "--no-ff", "--no-edit", releaseBranch)
	m, audit := buildManager(t, repo, mode, 0)
	wtDir := t.TempDir()
	m.mu.Lock()
	m.cfg.WorktreeDir = wtDir
	m.mu.Unlock()
	prd := saveFixturePRD(t, m, releaseBranch, PRDCompleted, "", time.Now().Add(-90*24*time.Hour))
	tip := strings.TrimSpace(gitCmd(t, repo, "rev-parse", releaseBranch))
	return &releaseFixture{m: m, audit: audit, repo: repo, worktree: worktree, prd: prd, tip: tip}
}

// releaseSteps are the two ways the daemon releases an Automaton branch.
var releaseSteps = []struct {
	name string
	run  func(*releaseFixture)
}{
	{"release at the end of work", func(f *releaseFixture) { f.m.ReleaseAutomatonWorktree(f.prd) }},
	{"periodic worktrees pass", func(f *releaseFixture) { f.m.CleanupWorktreesPass() }},
}

func (f *releaseFixture) backups(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(gitCmd(t, f.repo, "for-each-ref", "--format=%(objectname)", reapBackupRefPrefix))
}

func TestReleaseKeepsAutomatonWorktreeWithParkedWork(t *testing.T) {
	for _, step := range releaseSteps {
		for _, tc := range parkedCases {
			t.Run(step.name+"/"+tc.name, func(t *testing.T) {
				f := newReleaseFixture(t, config.CleanupModeOn)
				ignoreDatawatch(t, f.repo)
				keepWrite(t, f.worktree, tc.file, "the only copy\n")
				step.run(f)
				mustStillHold(t, f.worktree, tc.file, "the only copy\n")
				if !branchExists(t, f.repo, releaseBranch) {
					t.Fatal("the branch of a kept worktree was deleted")
				}
			})
		}
	}
}

func TestReleaseSavesVerificationScriptsThenRemoves(t *testing.T) {
	for _, step := range releaseSteps {
		t.Run(step.name, func(t *testing.T) {
			f := newReleaseFixture(t, config.CleanupModeOn)
			ignoreDatawatch(t, f.repo)
			keepWrite(t, f.worktree, ".datawatch/verify/check.sh", "script\n")
			step.run(f)
			if _, err := os.Stat(f.worktree); !os.IsNotExist(err) {
				t.Fatalf("worktree should be removed: %v", err)
			}
			mustStillHold(t, savedVerifyScriptsDir(f.m.cfg.WorktreeDir, f.prd.ID), "check.sh", "script\n")
		})
	}
}

func TestReleaseBacksUpAutomatonBranchBeforeDeletingIt(t *testing.T) {
	for _, step := range releaseSteps {
		t.Run(step.name, func(t *testing.T) {
			f := newReleaseFixture(t, config.CleanupModeOn)
			step.run(f)
			if branchExists(t, f.repo, releaseBranch) {
				t.Fatal("a landed branch should be deleted")
			}
			if got := f.backups(t); got != f.tip {
				t.Fatalf("backup refs = %q, want the branch tip %s", got, f.tip)
			}
			f.audit.mu.Lock()
			var details map[string]any
			for _, e := range f.audit.items {
				if e.action == worktreeCleanupAuditAction {
					details = e.details
				}
			}
			f.audit.mu.Unlock()
			if ref, _ := details["backup_ref"].(string); !strings.HasPrefix(ref, reapBackupRefPrefix) || details["branch_tip"] != f.tip {
				t.Fatalf("removal audit lacks the backup: %v", details)
			}
			sha, err := RestoreReaped(context.Background(), NewMergeGitRunner(), f.repo, releaseBranch)
			if err != nil || sha != f.tip {
				t.Fatalf("restore = %q %v, want %s", sha, err, f.tip)
			}
		})
	}
}

func TestReleaseKeepsAutomatonBranchWhenBackupFails(t *testing.T) {
	for _, step := range releaseSteps {
		t.Run(step.name, func(t *testing.T) {
			f := newReleaseFixture(t, config.CleanupModeOn)
			// A ref at the backup folder's own name blocks every ref below it.
			gitCmd(t, f.repo, "update-ref", reapBackupRefPrefix+sanitizeAttemptTaskKey(releaseBranch), "HEAD")
			step.run(f)
			if !branchExists(t, f.repo, releaseBranch) {
				t.Fatal("branch deleted without a backup ref")
			}
			if _, err := os.Stat(f.worktree); err != nil {
				t.Fatalf("the entry must be left whole when no backup can be written: %v", err)
			}
		})
	}
}

func TestReleaseBackupNeverOverwritesAnEarlierBackup(t *testing.T) {
	f := newReleaseFixture(t, config.CleanupModeOn)
	old := strings.TrimSpace(gitCmd(t, f.repo, "rev-parse", f.tip+"~1"))
	var taken []string
	now := time.Now().Unix()
	for i := int64(0); i < 10; i++ {
		ref := reapBackupRef(releaseBranch, now+i)
		gitCmd(t, f.repo, "update-ref", ref, old)
		taken = append(taken, ref)
	}
	f.m.ReleaseAutomatonWorktree(f.prd)
	for _, ref := range taken {
		if got := strings.TrimSpace(gitCmd(t, f.repo, "rev-parse", ref)); got != old {
			t.Fatalf("earlier backup %s overwritten with %s", ref, got)
		}
	}
	if !strings.Contains(f.backups(t), f.tip) {
		t.Fatalf("no backup of the deleted tip %s", f.tip)
	}
}

func TestReleaseReportModeWritesNoBackupRef(t *testing.T) {
	f := newReleaseFixture(t, config.CleanupModeReport)
	f.m.ReleaseAutomatonWorktree(f.prd)
	if got := f.backups(t); got != "" {
		t.Fatalf("report mode wrote refs: %s", got)
	}
}

// ---- the reject and archive cleanup ----

func (f *completionFixture) orphanEvents(t *testing.T, suffix string) []map[string]any {
	t.Helper()
	return f.events("autonomous_orphan_worktree_" + suffix)
}

func (f *completionFixture) mustAuditOrphanKept(t *testing.T, reason string) {
	t.Helper()
	got := f.orphanEvents(t, "kept")
	if len(got) != 1 || got[0]["reason"] != reason {
		t.Fatalf("kept audit = %v, want one record with reason %s", got, reason)
	}
}

func TestOrphanCleanupOffModeRemovesNothing(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOff)
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHaveWorktree(t)
	f.mustAuditOrphanKept(t, "cleanup_off")
}

func TestOrphanCleanupReportModeRemovesNothing(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeReport)
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHaveWorktree(t)
	if got := f.orphanEvents(t, "would_remove"); len(got) != 1 {
		t.Fatalf("would-remove audit = %v", got)
	}
}

func TestOrphanCleanupOnModeRemovesCleanWorktree(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	before := f.refs(t)
	f.m.cleanupOrphanedWorktree(f.prd)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("a clean worktree should be removed: %v", err)
	}
	if after := f.refs(t); after != before {
		t.Fatalf("this step deletes no branch:\n%s\n---\n%s", before, after)
	}
	if got := f.orphanEvents(t, "remove"); len(got) != 1 {
		t.Fatalf("removal audit = %v", got)
	}
}

func TestOrphanCleanupKeepsLiveSession(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	f.m.SetSessionListerFn(func() []SessionInfo { return []SessionInfo{{Alive: true, ProjectDir: f.path}} })
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHaveWorktree(t)
	f.mustAuditOrphanKept(t, "live_session")
}

func TestOrphanCleanupKeepsDetachedCommits(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	gitRun(t, f.path, "checkout", "-q", "--detach")
	keepWrite(t, f.path, "detached.txt", "only here\n")
	gitRun(t, f.path, "add", "detached.txt")
	gitRun(t, f.path, "commit", "-q", "-m", "detached")
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHaveWorktree(t)
	f.mustAuditOrphanKept(t, "detached_commits")
}

func TestOrphanCleanupKeepsParkedWork(t *testing.T) {
	for _, tc := range parkedCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCompletionFixture(t, config.CleanupModeOn)
			ignoreDatawatch(t, f.repo)
			keepWrite(t, f.path, tc.file, "the only copy\n")
			f.m.cleanupOrphanedWorktree(f.prd)
			mustStillHold(t, f.path, tc.file, "the only copy\n")
		})
	}
}

func TestOrphanCleanupSavesVerificationScriptsThenRemoves(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	ignoreDatawatch(t, f.repo)
	keepWrite(t, f.path, ".datawatch/verify/check.sh", "script\n")
	f.m.cleanupOrphanedWorktree(f.prd)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed: %v", err)
	}
	mustStillHold(t, savedVerifyScriptsDir(f.wtDir, f.prd.ID), "check.sh", "script\n")
}

// This step has never forced a removal, and still does not: untracked files,
// even the daemon's own, make git refuse.
func TestOrphanCleanupNeverForces(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	keepWrite(t, f.path, ".mcp.json", "daemon\n")
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHaveWorktree(t)
	f.mustAuditOrphanKept(t, "remove_failed")
}

func TestRejectAndArchiveFollowCleanupModeOff(t *testing.T) {
	t.Run("reject", func(t *testing.T) {
		f := newCompletionFixture(t, config.CleanupModeOff)
		f.prd.Status = PRDNeedsReview
		if _, err := f.m.Reject(f.prd.ID, "operator", "test"); err != nil {
			t.Fatalf("Reject: %v", err)
		}
		f.mustHaveWorktree(t)
	})
	t.Run("archive", func(t *testing.T) {
		f := newCompletionFixture(t, config.CleanupModeOff)
		f.prd.Status = PRDCancelled
		if _, err := f.m.ArchiveWithOutcome(f.prd.ID, "operator", OutcomeInput{Outcome: OutcomeDropped}); err != nil {
			t.Fatalf("Archive: %v", err)
		}
		f.mustHaveWorktree(t)
	})
}
