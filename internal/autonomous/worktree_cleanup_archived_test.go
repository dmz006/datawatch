package autonomous

// Cleanup of archived Automata: an archived record counts as finished, under
// the same keep-work rules as a completed or cancelled one. Every test here
// works in throwaway repositories and a throwaway data directory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const archivedTestBranch = "automaton/fixture"

func old48h() time.Time { return time.Now().Add(-48 * time.Hour) }

// archivedPassFixture is one archived Automaton whose work branch is on the
// default branch, with a clean worktree, older than the retention window.
func archivedPassFixture(t *testing.T, mode string) (m *Manager, audit *recordedAudit, repo, worktree string, prd *PRD) {
	t.Helper()
	repo, worktree = fixtureRepo(t)
	m, audit = buildManager(t, repo, mode, 24)
	prd = saveFixturePRD(t, m, archivedTestBranch, PRDArchived, "", old48h())
	prd.ProjectDir = worktree
	return
}

func saveOtherPRD(t *testing.T, m *Manager, id string, status PRDStatus, mutate func(*PRD)) *PRD {
	t.Helper()
	other := &PRD{ID: id, Title: "other " + id, Status: status}
	mutate(other)
	if err := m.Store().SavePRD(other); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return other
}

func skipReasonFor(res WorktreeCleanupResult, branch string) string {
	for _, s := range res.Skipped {
		if s.Branch == branch {
			return s.Reason
		}
	}
	return ""
}

func TestArchivedCleanupPassReleasesLandedWork(t *testing.T) {
	m, audit, repo, worktree, prd := archivedPassFixture(t, config.CleanupModeOn)
	prd.Landing = &Landing{State: LandingPending, Actor: "autonomous", At: old48h()}
	tip := strings.TrimSpace(gitCmd(t, repo, "rev-parse", archivedTestBranch))

	res := m.CleanupWorktreesPass()

	if len(res.Removed) != 1 {
		t.Fatalf("removed=%+v skipped=%+v failed=%+v", res.Removed, res.Skipped, res.Failed)
	}
	if branchExists(t, repo, archivedTestBranch) {
		t.Fatal("the landed work branch of an archived Automaton was not deleted")
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("the worktree is still there: %v", err)
	}
	if got := gitCmd(t, repo, "for-each-ref", "--format=%(objectname)", "refs/datawatch/reap-backup/"); !strings.Contains(got, tip) {
		t.Fatalf("no backup ref for the tip %s: %q", tip, got)
	}
	if len(audit.events(worktreeCleanupAuditAction)) != 1 {
		t.Fatalf("want one removal audit record")
	}
	// The record says the work landed, so the weekly audit does not later read
	// the deleted branch as lost work.
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDArchived || got.Landing == nil || got.Landing.State != LandingLanded {
		t.Fatalf("status=%s landing=%+v", got.Status, got.Landing)
	}
}

func TestArchivedCleanupKeepsALandingDecision(t *testing.T) {
	m, _, repo, _, prd := archivedPassFixture(t, config.CleanupModeOn)
	prd.Landing = &Landing{State: LandingDropped, Actor: "operator", At: old48h(), Reason: "not wanted"}
	if res := m.CleanupWorktreesPass(); len(res.Removed) != 1 || branchExists(t, repo, archivedTestBranch) {
		t.Fatalf("removed=%+v skipped=%+v", res.Removed, res.Skipped)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Landing.State != LandingDropped || got.Landing.Reason != "not wanted" {
		t.Fatalf("the operator's decision was replaced: %+v", got.Landing)
	}
}

func TestArchivedCleanupReportModeListsAndRemovesNothing(t *testing.T) {
	m, audit, repo, worktree, prd := archivedPassFixture(t, config.CleanupModeReport)
	res := m.CleanupWorktreesPass()
	if len(res.WouldRemove) != 1 || res.WouldRemove[0].Branch != archivedTestBranch {
		t.Fatalf("would=%+v skipped=%+v", res.WouldRemove, res.Skipped)
	}
	if !branchExists(t, repo, archivedTestBranch) {
		t.Fatal("report mode deleted the branch")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("report mode removed the worktree: %v", err)
	}
	if len(audit.events(worktreeCleanupAuditAction)) != 0 {
		t.Fatal("report mode audited a removal")
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Landing != nil {
		t.Fatalf("report mode wrote a landing record: %+v", got.Landing)
	}
}

func TestArchivedCleanupOffModeDoesNothing(t *testing.T) {
	m, audit, repo, _, _ := archivedPassFixture(t, config.CleanupModeOff)
	res := m.CleanupWorktreesPass()
	if len(res.Removed)+len(res.WouldRemove)+len(res.Skipped) != 0 || !branchExists(t, repo, archivedTestBranch) {
		t.Fatalf("off mode acted: %+v", res)
	}
	if len(audit.items) != 0 {
		t.Fatalf("off mode audited: %+v", audit.items)
	}
}

func TestArchivedCleanupSwitchOffKeepsArchivedWork(t *testing.T) {
	m, _, repo, worktree, _ := archivedPassFixture(t, config.CleanupModeOn)
	m.mu.Lock()
	m.cfg.Automation.CleanupArchived = boolPtr(false)
	m.mu.Unlock()
	res := m.CleanupWorktreesPass()
	if got := skipReasonFor(res, archivedTestBranch); got != cleanupSkipNotFinished {
		t.Fatalf("skip reason = %q, want %q (%+v)", got, cleanupSkipNotFinished, res)
	}
	if _, err := os.Stat(worktree); err != nil || !branchExists(t, repo, archivedTestBranch) {
		t.Fatalf("the switch is off and something was removed: %v", err)
	}
}

// Every keep-work rule holds for an archived record.
func TestArchivedCleanupPassKeepCases(t *testing.T) {
	type env struct {
		m              *Manager
		repo, worktree string
		prd            *PRD
	}
	commit := func(t *testing.T, e env, rel string) {
		t.Helper()
		keepWrite(t, e.worktree, rel, "work\n")
		gitCmd(t, e.worktree, "add", rel)
		gitCmd(t, e.worktree, "commit", "-q", "-m", "work "+rel)
	}
	cases := []struct {
		name, reason string
		setup        func(t *testing.T, e env)
	}{
		{"uncommitted file", cleanupSkipDirty, func(t *testing.T, e env) {
			keepWrite(t, e.worktree, "notes.txt", "the only copy\n")
		}},
		{"changed tracked file", cleanupSkipDirty, func(t *testing.T, e env) {
			commit(t, e, "a.txt")
			gitCmd(t, e.repo, "merge", "-q", "--ff-only", archivedTestBranch)
			keepWrite(t, e.worktree, "a.txt", "edited\n")
		}},
		// The removal-time comparison refuses it.
		{"commit not on the default branch", cleanupSkipCommits, func(t *testing.T, e env) {
			commit(t, e, "unmerged.txt")
		}},
		// With the default branch moved on, the report itself holds it.
		{"commit not on a default branch that moved on", cleanupSkipHeld, func(t *testing.T, e env) {
			commit(t, e, "unmerged.txt")
			gitCmd(t, e.repo, "commit", "-q", "--allow-empty", "-m", "elsewhere")
		}},
		{"file in a protected folder", cleanupSkipDirty, func(t *testing.T, e env) {
			keepWrite(t, e.repo, ".git/info/exclude", ".datawatch/\n")
			keepWrite(t, e.worktree, ".datawatch/reports/r.md", "the only copy\n")
		}},
		{"live session in the folder", cleanupSkipLiveSession, func(t *testing.T, e env) {
			e.m.SetSessionListerFn(func() []SessionInfo { return []SessionInfo{{Alive: true, ProjectDir: e.worktree}} })
		}},
		{"live lease on the folder", cleanupSkipLiveLease, func(t *testing.T, e env) {
			keepWrite(t, e.repo, ".git/info/exclude", ".datawatch/\n")
			if err := writeLockRecord(treeLockPath(treeKeyPrefix+e.worktree, e.worktree), LockRecord{HolderTaskID: "someone"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"younger than the retention window", cleanupSkipRetained, func(t *testing.T, e env) {
			e.prd.UpdatedAt = time.Now()
		}},
		{"a draft runs in the folder", cleanupSkipNeeded, func(t *testing.T, e env) {
			saveOtherPRD(t, e.m, "later001", PRDDraft, func(p *PRD) { p.ProjectDir = e.worktree })
		}},
		{"a held Automaton runs below the folder", cleanupSkipNeeded, func(t *testing.T, e env) {
			saveOtherPRD(t, e.m, "later002", PRDHeld, func(p *PRD) { p.ProjectDir = filepath.Join(e.worktree, "sub") })
		}},
		{"an older draft carries the branch", cleanupSkipNeeded, func(t *testing.T, e env) {
			d := saveOtherPRD(t, e.m, "later003", PRDDraft, func(p *PRD) { p.Git.Branch = archivedTestBranch })
			d.CreatedAt = e.prd.CreatedAt.Add(-time.Hour)
		}},
		{"a newer draft carries the branch", cleanupSkipNotFinished, func(t *testing.T, e env) {
			d := saveOtherPRD(t, e.m, "later006", PRDDraft, func(p *PRD) { p.Git.Branch = archivedTestBranch })
			d.CreatedAt = e.prd.CreatedAt.Add(time.Hour)
		}},
		{"an approved Automaton starts from the branch", cleanupSkipNeeded, func(t *testing.T, e env) {
			saveOtherPRD(t, e.m, "later004", PRDApproved, func(p *PRD) { p.Git.BaseBranch = archivedTestBranch })
		}},
		{"a blocked task runs in the folder", cleanupSkipNeeded, func(t *testing.T, e env) {
			saveOtherPRD(t, e.m, "later005", PRDBlocked, func(p *PRD) {
				p.Story = []Story{{ID: "s5", PRDID: p.ID, Tasks: []Task{{ID: "t5", StoryID: "s5", PRDID: p.ID, ProjectDir: e.worktree}}}}
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, audit, repo, worktree, prd := archivedPassFixture(t, config.CleanupModeOn)
			tc.setup(t, env{m: m, repo: repo, worktree: worktree, prd: prd})
			res := m.CleanupWorktreesPass()
			if len(res.Removed) != 0 || len(res.Failed) != 0 {
				t.Fatalf("removed=%+v failed=%+v", res.Removed, res.Failed)
			}
			if got := skipReasonFor(res, archivedTestBranch); got != tc.reason {
				t.Fatalf("skip reason = %q, want %q (%+v)", got, tc.reason, res.Skipped)
			}
			if _, err := os.Stat(filepath.Join(worktree, ".git")); err != nil {
				t.Fatalf("the worktree was removed: %v", err)
			}
			if !branchExists(t, repo, archivedTestBranch) {
				t.Fatal("the branch was deleted")
			}
			if len(audit.events(worktreeCleanupAuditAction)) != 0 {
				t.Fatal("a removal was audited")
			}
		})
	}
}

// A worktree git cannot read is unverifiable and stays.
func TestArchivedCleanupKeepsUnverifiableWorktree(t *testing.T) {
	m, _, repo, worktree, _ := archivedPassFixture(t, config.CleanupModeOn)
	keepWrite(t, worktree, ".git", "gitdir: /nowhere/at/all\n")
	res := m.CleanupWorktreesPass()
	if len(res.Removed) != 0 || !branchExists(t, repo, archivedTestBranch) {
		t.Fatalf("removed=%+v", res.Removed)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the folder was removed: %v", err)
	}
}

// Without the finished-record guard a completed or cancelled record's folder
// that a later run needs would go too.
func TestCompletedCleanupKeepsFolderALaterRunNeeds(t *testing.T) {
	repo, worktree := fixtureRepo(t)
	m, _ := buildManager(t, repo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, archivedTestBranch, PRDCompleted, "", old48h())
	saveOtherPRD(t, m, "later001", PRDDraft, func(p *PRD) { p.ProjectDir = worktree })
	m.ReleaseAutomatonWorktree(prd)
	if _, err := os.Stat(worktree); err != nil || !branchExists(t, repo, archivedTestBranch) {
		t.Fatalf("a folder a draft runs in was released: %v", err)
	}
}

// ---- task worktrees and task branches ----

// archiveKept archives the keep fixture as the operator, recording the work as
// dropped so an unmerged branch does not refuse the archive.
func (f *keepFixture) archive(t *testing.T) {
	t.Helper()
	f.prd.Status = PRDCancelled
	if _, err := f.m.ArchiveWithOutcome(f.prd.ID, "operator", OutcomeInput{Outcome: OutcomeDropped}); err != nil {
		t.Fatalf("archive: %v", err)
	}
}

// age moves the record's last change past the retention window.
func (f *keepFixture) age(t *testing.T) {
	t.Helper()
	f.m.store.WithLock(func() { f.prd.UpdatedAt = old48h() })
}

func (f *keepFixture) setArchivedSwitch(on bool) {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	f.m.cfg.Automation.CleanupArchived = &on
}

func TestArchiveReleasesCleanLandedTaskWorktreeAndBranch(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	tip := f.commit(t, "feature.txt")
	f.land(t)
	f.archive(t)
	f.mustNotHaveWorktree(t)
	if f.hasBranch(t) {
		t.Fatal("the landed task branch was not deleted at archive")
	}
	if got := f.backupSHAs(t); !strings.Contains(got, tip) {
		t.Fatalf("no backup of %s: %q", tip, got)
	}
}

func TestArchiveSwitchOffKeepsTaskWorktree(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	f.setArchivedSwitch(false)
	f.commit(t, "feature.txt")
	f.land(t)
	f.archive(t)
	f.mustHaveWorktree(t)
	if !f.hasBranch(t) {
		t.Fatal("branch deleted with the switch off")
	}
	if _, _, reasons := f.cleanupSummary(t); reasons[taskKeepByArchive] != 1 {
		t.Fatalf("reasons = %v", reasons)
	}
	f.age(t)
	f.m.CleanupWorktreesPass()
	f.mustHaveWorktree(t)
}

func TestArchivedTaskKeepCases(t *testing.T) {
	cases := []struct {
		name, reason string
		setup        func(t *testing.T, f *keepFixture)
	}{
		{"uncommitted file", taskKeepUncommitted, func(t *testing.T, f *keepFixture) {
			keepWrite(t, f.path, "notes.txt", "the only copy\n")
		}},
		{"file in a protected folder", taskKeepUncommitted, func(t *testing.T, f *keepFixture) {
			keepWrite(t, f.path, ".datawatch/reports/r.md", "the only copy\n")
		}},
		{"live session", taskKeepLiveSession, func(t *testing.T, f *keepFixture) {
			f.m.SetSessionListerFn(func() []SessionInfo { return []SessionInfo{{Alive: true, ProjectDir: f.path}} })
		}},
		{"folder without its git entry", taskKeepUnverified, func(t *testing.T, f *keepFixture) {
			if err := os.Remove(filepath.Join(f.path, ".git")); err != nil {
				t.Fatal(err)
			}
			keepWrite(t, f.path, "keep.txt", "x\n")
		}},
		{"a later run uses the folder", taskKeepNeeded, func(t *testing.T, f *keepFixture) {
			saveOtherPRD(t, f.m, "later001", PRDDraft, func(p *PRD) { p.ProjectDir = f.path })
		}},
		{"a later run uses the branch", taskKeepNeeded, func(t *testing.T, f *keepFixture) {
			saveOtherPRD(t, f.m, "later002", PRDNeedsReview, func(p *PRD) { p.Git.BaseBranch = f.branch })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newKeepFixture(t, config.CleanupModeOn)
			tc.setup(t, f)
			f.archive(t)
			f.age(t)
			f.m.CleanupWorktreesPass()
			if _, err := os.Stat(f.path); err != nil {
				t.Fatalf("the task folder was removed: %v", err)
			}
			if !f.hasBranch(t) {
				t.Fatal("the branch of a kept folder was deleted")
			}
			if _, _, reasons := f.cleanupSummary(t); reasons[tc.reason] != 1 {
				t.Fatalf("reasons = %v, want %s", reasons, tc.reason)
			}
		})
	}
}

// A clean task folder goes; its unmerged branch stays until the work lands,
// and the periodic pass releases it then.
func TestArchivedTaskBranchReleasedWhenWorkLandsLater(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	tip := f.commit(t, "feature.txt")
	f.archive(t)
	f.mustNotHaveWorktree(t)
	if !f.hasBranch(t) {
		t.Fatal("unmerged task branch deleted")
	}
	f.age(t)
	f.m.CleanupWorktreesPass()
	if !f.hasBranch(t) {
		t.Fatal("unmerged task branch deleted by the pass")
	}
	f.land(t)
	f.m.CleanupWorktreesPass()
	if f.hasBranch(t) {
		t.Fatal("the work landed after the archive and the pass did not release the branch")
	}
	if got := f.backupSHAs(t); !strings.Contains(got, tip) {
		t.Fatalf("no backup of %s: %q", tip, got)
	}
}

// The periodic pass keeps the retention window for archived task work.
func TestArchivedTaskPassWaitsForRetention(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	f.commit(t, "feature.txt")
	f.archive(t)
	f.m.mu.Lock()
	f.m.cfg.Automation.CleanupRetentionHours = 24
	f.m.mu.Unlock()
	f.land(t)
	f.m.CleanupWorktreesPass()
	if !f.hasBranch(t) {
		t.Fatal("a just-archived record was released by the periodic pass")
	}
}

// The per-pass limit bounds the task work the periodic pass removes.
func TestArchivedTaskPassHonoursRemovalLimit(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeOn)
	f.setArchivedSwitch(false) // archive without releasing, as records archived earlier are
	second := Task{ID: "task0002", StoryID: "s1", PRDID: f.prd.ID, Title: "second", Status: TaskCompleted}
	f.prd.Story[0].Tasks = append(f.prd.Story[0].Tasks, second)
	if err := f.m.Store().SavePRD(f.prd); err != nil {
		t.Fatal(err)
	}
	t2 := &f.prd.Story[0].Tasks[1]
	if err := f.m.ensureTaskWorktreeFor(f.prd, t2); err != nil {
		t.Fatal(err)
	}
	f.archive(t)
	f.age(t)
	f.setArchivedSwitch(true)
	f.m.mu.Lock()
	f.m.cfg.Automation.CleanupMaxRemovalsPerPass = 1
	f.m.cfg.WorktreeBaseRepo = f.repo
	f.m.mu.Unlock()
	paths := []string{f.prd.Story[0].Tasks[0].WorktreePath, t2.WorktreePath}
	left := func() int {
		n := 0
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				n++
			}
		}
		return n
	}
	// The Automaton's own work branch shares the limit; keep it out of the
	// count by giving it a file that holds it.
	keepWrite(t, f.prd.ProjectDir, "hold.txt", "held\n")
	f.m.CleanupWorktreesPass()
	if left() != 1 {
		t.Fatalf("one removal allowed, %d of 2 task folders left", left())
	}
	f.m.CleanupWorktreesPass()
	if left() != 0 {
		t.Fatalf("the next pass takes the next one, %d left", left())
	}
}

func TestArchivedTaskPassReportModeRecordsAndRemovesNothing(t *testing.T) {
	f := newKeepFixture(t, config.CleanupModeReport)
	f.archive(t)
	f.age(t)
	f.m.CleanupWorktreesPass()
	f.mustHaveWorktree(t)
	if !f.hasBranch(t) {
		t.Fatal("report mode deleted a branch")
	}
	if _, _, reasons := f.cleanupSummary(t); reasons[taskKeepReportMode] != 1 {
		t.Fatalf("reasons = %v", reasons)
	}
	if got := f.auditWith(auditTaskWorktreeWouldRemove); len(got) != 1 {
		t.Fatalf("would-remove audit = %v", got)
	}
}

// The step that releases the Automaton worktree at archive keeps a folder
// another Automaton still points at.
func TestArchiveKeepsAutomatonFolderALaterRunNeeds(t *testing.T) {
	f := newCompletionFixture(t, config.CleanupModeOn)
	saveOtherPRD(t, f.m, "later001", PRDDraft, func(p *PRD) { p.ProjectDir = f.path })
	f.m.cleanupOrphanedWorktree(f.prd)
	f.mustHaveWorktree(t)
	f.mustAuditOrphanKept(t, taskKeepNeeded)
}

// ---- a record's own missing worktree is created again ----

func recreateFixture(t *testing.T) (m *Manager, repo, wtDir string, prd *PRD) {
	t.Helper()
	repo, wtDir, fx := prdWorktreeFixture(t)
	m, _ = buildManager(t, repo, config.CleanupModeOff, 0)
	m.mu.Lock()
	m.cfg.WorktreeDir = wtDir
	m.mu.Unlock()
	fx.Status = PRDApproved
	if err := m.Store().SavePRD(fx); err != nil {
		t.Fatal(err)
	}
	return m, repo, wtDir, fx
}

func mustBeWorktree(t *testing.T, repo, dir, branch string) {
	t.Helper()
	if err := CheckProjectWorktree(dir, repo); err != nil {
		t.Fatalf("not a usable worktree: %v", err)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != branch {
		t.Fatalf("branch = %q, want %q", got, branch)
	}
}

func TestMissingOwnWorktreeIsCreatedAgain(t *testing.T) {
	cases := []struct {
		name  string
		clear func(t *testing.T, repo, dir, branch string)
	}{
		{"removed with git", func(t *testing.T, repo, dir, _ string) {
			gitRun(t, repo, "worktree", "remove", dir)
		}},
		{"removed with its branch", func(t *testing.T, repo, dir, branch string) {
			gitRun(t, repo, "worktree", "remove", dir)
			gitRun(t, repo, "branch", "-d", branch)
		}},
		{"folder deleted, git still lists it", func(t *testing.T, _, dir, _ string) {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, repo, _, prd := recreateFixture(t)
			dir, branch := prd.ProjectDir, prd.Git.Branch
			tc.clear(t, repo, dir, branch)
			created, err := m.ensureWorktreeIfConfigured(prd)
			if err != nil || !created {
				t.Fatalf("created=%v err=%v", created, err)
			}
			if prd.ProjectDir != dir || prd.Git.Branch != branch {
				t.Fatalf("dir=%q branch=%q changed", prd.ProjectDir, prd.Git.Branch)
			}
			mustBeWorktree(t, repo, dir, branch)
		})
	}
}

// The branch keeps its own commits when only the folder was cleared.
func TestRecreatedWorktreeKeepsBranchCommits(t *testing.T) {
	m, repo, _, prd := recreateFixture(t)
	keepWrite(t, prd.ProjectDir, "work.txt", "w\n")
	gitRun(t, prd.ProjectDir, "add", "work.txt")
	gitRun(t, prd.ProjectDir, "commit", "-q", "-m", "work")
	tip := gitOut(t, prd.ProjectDir, "rev-parse", "HEAD")
	gitRun(t, repo, "worktree", "remove", prd.ProjectDir)
	if _, err := m.ensureWorktreeIfConfigured(prd); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, prd.ProjectDir, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("HEAD = %s, want %s", got, tip)
	}
}

// A decompose of a record whose folder was cleared runs in the new folder.
func TestDecomposeRecreatesMissingOwnWorktree(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	var dirs []string
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Parallel.WorktreePerTask = boolPtr(false)
	cfg.WorktreeDir = t.TempDir()
	m, err := NewManager(t.TempDir(), cfg, func(req DecomposeRequest) (string, error) {
		if _, serr := os.Stat(filepath.Join(req.ProjectDir, ".git")); serr != nil {
			t.Errorf("decompose got a folder that is not there: %v", serr)
		}
		dirs = append(dirs, req.ProjectDir)
		return fakePlanResp, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	gitRun(t, baseRepo, "worktree", "remove", "--force", dirs[0])
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("second Decompose: %v", err)
	}
	if len(dirs) != 2 || dirs[0] != dirs[1] {
		t.Fatalf("dirs = %v", dirs)
	}
}

func TestMissingWorktreeIsNotRecreatedElsewhere(t *testing.T) {
	t.Run("a folder the daemon did not make", func(t *testing.T) {
		m, _, _, prd := recreateFixture(t)
		other := filepath.Join(t.TempDir(), "operator-folder")
		prd.ProjectDir = other
		created, err := m.ensureWorktreeIfConfigured(prd)
		if created || err != nil {
			t.Fatalf("created=%v err=%v", created, err)
		}
		if _, err := os.Stat(other); !os.IsNotExist(err) {
			t.Fatalf("a folder was made at an operator path: %v", err)
		}
	})
	t.Run("a folder that is there is left alone", func(t *testing.T) {
		m, repo, _, prd := recreateFixture(t)
		gitRun(t, repo, "worktree", "remove", prd.ProjectDir)
		keepWrite(t, prd.ProjectDir, "mine.txt", "x\n")
		created, err := m.ensureWorktreeIfConfigured(prd)
		if created || err != nil {
			t.Fatalf("created=%v err=%v", created, err)
		}
		if _, err := os.Stat(filepath.Join(prd.ProjectDir, "mine.txt")); err != nil {
			t.Fatalf("the folder was touched: %v", err)
		}
	})
	t.Run("a locked registration is not forced", func(t *testing.T) {
		m, repo, _, prd := recreateFixture(t)
		gitRun(t, repo, "worktree", "lock", prd.ProjectDir)
		if err := os.RemoveAll(prd.ProjectDir); err != nil {
			t.Fatal(err)
		}
		if _, err := m.ensureWorktreeIfConfigured(prd); err == nil {
			t.Fatal("a locked worktree was re-created")
		}
	})
}

// Lane fill does not refuse an Automaton whose own cleared worktree the run
// makes again.
func TestLaneFillDoesNotRefuseAClearedOwnWorktree(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	wtDir := t.TempDir()
	cfg := h.m.Config()
	cfg.WorktreeDir = wtDir
	h.m.SetConfig(cfg)
	p := h.addPRD("cleared", PRDApproved, "", "pi-prompt")
	p.ProjectDir = filepath.Join(wtDir, p.ID)
	p.Git.Branch = "automaton/" + p.ID
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	fsys.list = nil

	h.poll()

	if reason, text := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("refused %q: %s", reason, text)
	}
}

// A branch with nothing ahead is empty on the count alone: the patch and
// subject comparisons, which cost most of a cleanup pass, are not run.
func TestCompareBranchNothingAheadRunsOnlyTheCount(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "rev-list", "--count", "main.."+testBranch)] = "0\n"
	got := CompareBranchToMain(t.Context(), g, testRepo, testBranch, "main")
	if got.State != MergeStateEmpty || got.Ahead != 0 || !got.SubjectsOnBase || got.Error != "" {
		t.Fatalf("got %+v", got)
	}
	if g.hasCall("cherry") || g.hasCall("log") {
		t.Fatal("ran comparisons that cannot change the answer")
	}
}
