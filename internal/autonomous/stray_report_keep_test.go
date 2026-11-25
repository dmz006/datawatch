// Tests that no report and no moved-aside file is deleted or overwritten
// while its content exists nowhere else.
package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type strayAudit struct {
	mu     sync.Mutex
	events []string
}

func (a *strayAudit) hook(m *Manager) {
	m.SetAuditFn(func(action string, d map[string]any) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.events = append(a.events, action+" "+strayDetails(d))
	})
}

func strayDetails(d map[string]any) string {
	b, _ := json.Marshal(d)
	return string(b)
}

// has reports whether one audit event holds every part.
func (a *strayAudit) has(parts ...string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
next:
	for _, e := range a.events {
		for _, p := range parts {
			if !strings.Contains(e, p) {
				continue next
			}
		}
		return true
	}
	return false
}

// strayContents returns, for every regular file under dir, how many files
// hold each content.
func strayContents(t *testing.T, dir string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[string(b)] = append(out[string(b)], p)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

func mustSurvive(t *testing.T, dir string, contents ...string) {
	t.Helper()
	got := strayContents(t, dir)
	for _, c := range contents {
		if len(got[c]) == 0 {
			t.Errorf("content %q is gone from %s", c, dir)
		}
	}
}

type settleFixture struct {
	t     *testing.T
	repo  string
	mgr   *Manager
	prd   *PRD
	task  *Task
	base  string
	audit *strayAudit
}

func newSettleFixture(t *testing.T) *settleFixture {
	t.Helper()
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/sub/summary.md")
	tk, ok := mgr.Store().GetTask(prd.Story[0].Tasks[0].ID)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}
	base, ok := mgr.readOnlyTreeStatus(context.Background(), prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	a := &strayAudit{}
	a.hook(mgr)
	return &settleFixture{t: t, repo: repo, mgr: mgr, prd: prd, task: tk, base: base, audit: a}
}

func (f *settleFixture) write(rel, body string) {
	f.t.Helper()
	roWriteFile(f.t, filepath.Join(f.repo, filepath.FromSlash(rel)), body)
}

func (f *settleFixture) settle() {
	f.mgr.settleReadOnlyReportFiles(context.Background(), f.prd, f.task, f.base)
}

func (f *settleFixture) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(f.repo, filepath.FromSlash(rel)))
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

func TestStraySettleKeepsBothDifferingReports(t *testing.T) {
	f := newSettleFixture(t)
	f.write(".datawatch/reports/sub/extra.md", "first report\n")
	f.write("docs/plans/reports/extra.md", "second report\n")
	f.settle()

	if got := f.read(".datawatch/reports/sub/extra.md"); got != "first report\n" {
		t.Errorf("the report already there changed: %q", got)
	}
	mustSurvive(t, f.repo, "first report\n", "second report\n")
	if _, err := os.Lstat(filepath.Join(f.repo, "docs/plans/reports/extra.md")); err == nil {
		t.Error("the stray copy was not moved out of docs/plans/reports/")
	}
	if err := f.mgr.readOnlyViolation(context.Background(), f.prd, f.task, f.base); err != nil {
		t.Errorf("the settled tree must not violate: %v", err)
	}
	if !f.audit.has("docs/plans/reports/extra.md", ".datawatch/reports/sub/extra.stray-1.md") {
		t.Errorf("the audit log does not say where the stray copy went: %v", f.audit.events)
	}
	cur, _ := f.mgr.Store().GetPRD(f.prd.ID)
	if n := cur.Decisions[len(cur.Decisions)-1].Note; !strings.Contains(n, ".datawatch/reports/sub/extra.stray-1.md") {
		t.Errorf("the decision does not say where the stray copy went: %s", n)
	}
}

func TestStraySettleEveryDifferingCopyGetsItsOwnName(t *testing.T) {
	f := newSettleFixture(t)
	f.write(".datawatch/reports/sub/extra.md", "one\n")
	for _, body := range []string{"two\n", "three\n", "two\n"} {
		f.write("docs/plans/reports/extra.md", body)
		f.settle()
	}
	// Two files with one base name in different folders, in a single pass.
	f.write("docs/plans/reports/a/same.md", "from a\n")
	f.write("docs/plans/reports/b/same.md", "from b\n")
	f.settle()
	mustSurvive(t, f.repo, "one\n", "two\n", "three\n", "from a\n", "from b\n")
	if n := len(strayContents(t, f.repo)["two\n"]); n != 1 {
		t.Errorf("an identical stray copy should not be stored twice, found %d", n)
	}
}

func TestStraySettleRemovesIdenticalCopyAndAuditsIt(t *testing.T) {
	f := newSettleFixture(t)
	f.write(".datawatch/reports/sub/extra.md", "same\n")
	f.write("docs/plans/reports/extra.md", "same\n")
	f.settle()
	if _, err := os.Lstat(filepath.Join(f.repo, "docs/plans/reports/extra.md")); err == nil {
		t.Error("an identical stray copy may be removed")
	}
	if got := f.read(".datawatch/reports/sub/extra.md"); got != "same\n" {
		t.Errorf("the kept report changed: %q", got)
	}
	if n := len(strayContents(t, f.repo)["same\n"]); n != 1 {
		t.Errorf("want one copy, found %d", n)
	}
	if !f.audit.has("docs/plans/reports/extra.md", ".datawatch/reports/sub/extra.md") {
		t.Errorf("the removal is not in the audit log: %v", f.audit.events)
	}
}

// A name that is taken by something that is not a regular file proves nothing
// about the stray copy's content.
func TestStraySettleKeepsCopyWhenNameIsALink(t *testing.T) {
	f := newSettleFixture(t)
	link := filepath.Join(f.repo, ".datawatch/reports/sub/extra.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.repo, "nowhere"), link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	f.write("docs/plans/reports/extra.md", "the only copy\n")
	f.settle()
	mustSurvive(t, f.repo, "the only copy\n")
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link already there was replaced: %v", err)
	}
}

// Worktree cleanup treats the default gate report as disposable and the next
// gate run writes over it, so a stray report never takes that name.
func TestStraySettleNeverTakesTheDefaultGateReportName(t *testing.T) {
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/summary.md")
	tk, _ := mgr.Store().GetTask(prd.Story[0].Tasks[0].ID)
	base, _ := mgr.readOnlyTreeStatus(context.Background(), prd, tk)
	roWriteFile(t, filepath.Join(repo, "docs/plans/reports/final-gate.txt"), "a worker's own verdict\n")
	mgr.settleReadOnlyReportFiles(context.Background(), prd, tk, base)
	if _, err := os.Lstat(filepath.Join(repo, defaultGateReportPath)); err == nil {
		t.Error("the stray report took the default gate report name")
	}
	mustSurvive(t, repo, "a worker's own verdict\n")
}

func TestStrayDeleteAutomatonKeepsMovedAsideFolder(t *testing.T) {
	repo := scratchGitRepo(t)
	wtDir := t.TempDir()
	m := newRoManager(t, nil)
	m.cfg.WorktreeDir = wtDir
	a := &strayAudit{}
	a.hook(m)
	prd, err := m.Store().CreatePRD("Stray", repo, "claude-code", "", Effort("normal"))
	if err != nil {
		t.Fatal(err)
	}
	// A plain folder at the worktree path is moved aside when the worktree
	// is made.
	at := filepath.Join(wtDir, prd.ID)
	roWriteFile(t, filepath.Join(at, "notes/work.md"), "unsaved work\n")
	roWriteFile(t, filepath.Join(at, ".datawatch/verify/old.sh"), "old script\n")
	if _, _, err := EnsureWorktree(prd, repo, wtDir); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	saved := savedVerifyScriptsDir(wtDir, prd.ID)
	roWriteFile(t, filepath.Join(saved, "plain.sh"), "a saved script\n")

	if err := m.DeletePRD(prd.ID); err != nil {
		t.Fatalf("DeletePRD: %v", err)
	}
	mustSurvive(t, saved, "unsaved work\n", "old script\n")
	if _, err := os.Lstat(filepath.Join(saved, "plain.sh")); !os.IsNotExist(err) {
		t.Errorf("the saved script of the deleted Automaton should be removed: %v", err)
	}
	if !a.has(prd.ID, saved, "notes/work.md", "stray-") {
		t.Errorf("the audit log does not list the kept folder and its files: %v", a.events)
	}
}

func TestStrayMergeAsideNeverOverwritesAnEarlierCopy(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	a := &strayAudit{}
	a.hook(f.m)
	f.addTask("t1", nil, "report.md", "the task's report\n")
	f.seal()
	dir := f.prd.ProjectDir
	earlier := filepath.Join(dir, ".datawatch/merge-aside/t1/report.md")
	roWriteFile(t, earlier, "aside from the first try\n")
	writeMergeFile(t, dir, "report.md", "a newer draft\n")
	if out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1")); err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	mustSurvive(t, dir, "aside from the first try\n", "a newer draft\n")
	if !a.has("t1", "report.md", ".datawatch/merge-aside/t1/report.stray-1.md") {
		t.Errorf("the audit log does not say where the file went: %v", a.events)
	}
}

func TestStrayMergeAsideDropsCopyIdenticalToTheOneKept(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "report.md", "the task's report\n")
	f.seal()
	dir := f.prd.ProjectDir
	roWriteFile(t, filepath.Join(dir, ".datawatch/merge-aside/t1/report.md"), "a draft\n")
	writeMergeFile(t, dir, "report.md", "a draft\n")
	if out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1")); err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if n := len(strayContents(t, dir)["a draft\n"]); n != 1 {
		t.Errorf("want one copy, found %d", n)
	}
}

func TestStrayGateReportDoesNotOverwriteAnotherWritersFile(t *testing.T) {
	for _, tc := range []struct {
		name, before string
		keep         bool
	}{
		{"a file the daemon did not write", "a worker's own verdict\n", true},
		{"an earlier gate report", "== check 1 of 1: script x.sh ==\nboom\nverdict: failed: exit 1\n\nfinal verdict: failed\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			m := newRoManager(t, nil)
			a := &strayAudit{}
			a.hook(m)
			prd := makeReportPathPRD(t, m, dir, true, ".datawatch/reports/gate.md")
			tk, _ := m.Store().GetTask(prd.Story[0].Tasks[0].ID)
			at := filepath.Join(dir, ".datawatch/reports/gate.md")
			roWriteFile(t, at, tc.before)
			after := "== check 1 of 1: script x.sh ==\nverdict: passed (exit 0)\n\nfinal verdict: passed\n"
			m.finishScriptGateTask(prd, tk, after, "ran", "", true)
			if got, err := os.ReadFile(at); err != nil || string(got) != after {
				t.Fatalf("gate report not written: %v %q", err, got)
			}
			files := strayContents(t, filepath.Dir(at))
			if tc.keep {
				mustSurvive(t, filepath.Dir(at), tc.before)
				if !a.has(".datawatch/reports/gate.md", ".datawatch/reports/gate.stray-1.md") {
					t.Errorf("the audit log does not say where the file went: %v", a.events)
				}
			} else if len(files) != 1 {
				t.Errorf("a rerun replaces the gate's own report, found %v", files)
			}
		})
	}
}

// unlandedWorktreeWithIgnoredFiles checks out the unlanded branch in a work
// tree that holds ignored reports and moved-aside files.
func unlandedWorktreeWithIgnoredFiles(t *testing.T) (repo, base, wt string) {
	t.Helper()
	repo = unlandedActionsRepo(t)
	ignoreDatawatch(t, repo)
	base = t.TempDir()
	wt = filepath.Join(base, "eeee0005")
	gitT(t, repo, "worktree", "add", wt, "automaton/eeee0005")
	return repo, base, wt
}

func TestStrayUnlandedDeleteBacksUpReportsAndAsideFiles(t *testing.T) {
	repo, base, wt := unlandedWorktreeWithIgnoredFiles(t)
	roWriteFile(t, filepath.Join(wt, ".datawatch/reports/audit.md"), "audit report\n")
	roWriteFile(t, filepath.Join(wt, ".datawatch/merge-aside/t1/pkg/notes.txt"), "moved aside\n")
	roWriteFile(t, filepath.Join(wt, "docs/plans/reports/unsettled.md"), "stray, not yet settled\n")
	_, err := DeleteWorktree(context.Background(), NewMergeGitRunner(), DeleteWorktreeOptions{
		Branch: UnlandedBranch{Branch: "automaton/eeee0005", Recommendation: UnlandedNeedsDecision}, Confirm: true, BaseRepo: repo, WorktreeBase: base})
	if err != nil {
		t.Fatal(err)
	}
	mustSurvive(t, filepath.Join(base, UnlandedBackupsDir), "audit report\n", "moved aside\n", "stray, not yet settled\n")
}

// A folder that cannot be copied stops the delete with nothing removed.
func TestStrayUnlandedDeleteAbortsWhenTheBackupFails(t *testing.T) {
	repo, base, wt := unlandedWorktreeWithIgnoredFiles(t)
	nested := filepath.Join(wt, ".datawatch/reports/nested")
	roWriteFile(t, filepath.Join(nested, "r.md"), "in a nested repository\n")
	gitT(t, nested, "init", "-q")
	_, err := DeleteWorktree(context.Background(), NewMergeGitRunner(), DeleteWorktreeOptions{
		Branch: UnlandedBranch{Branch: "automaton/eeee0005", Recommendation: UnlandedNeedsDecision}, Confirm: true, BaseRepo: repo, WorktreeBase: base})
	if err == nil {
		t.Error("the delete should be aborted")
	}
	mustSurvive(t, wt, "in a nested repository\n")
	if !actionsBranchExists(t, repo, "automaton/eeee0005") {
		t.Error("the branch was removed")
	}
}

func TestStrayLeakedFileRepairNeverRemovesReports(t *testing.T) {
	for _, rel := range []string{
		"docs/plans/reports/x.md", ".datawatch/reports/x.md", ".datawatch/merge-aside/t1/a.txt",
		".datawatch/reports", ".datawatch", "docs/plans", "docs",
	} {
		t.Run(rel, func(t *testing.T) {
			ops := &fakeChainOps{
				tasks: []ChainTask{{AutomatonID: "a1", TaskID: "t1", Title: "x", Status: TaskFailed, Worktree: "/wt",
					FailureDetail: "stray file: " + rel}},
				paths: map[string]ChainPathState{"/wt:" + rel: {Exists: true}},
			}
			h := newChainHarness(t, "on", ops)
			h.r.Run(context.Background())
			h.r.Run(context.Background())
			if len(ops.cleans) != 0 {
				t.Errorf("the repair removed a report or moved-aside path: %v", ops.cleans)
			}
		})
	}
}

// The CLI and chat reply names where the copies are.
func TestStrayUnlandedDeleteReplyNamesTheFileBackup(t *testing.T) {
	got := RenderUnlandedActionResult("delete-worktree", []byte(`{"branch":"b","bundle_path":"p","files_backup_path":"p.files"}`))
	if !strings.Contains(got, "p.files") {
		t.Errorf("reply does not name the file backup: %s", got)
	}
}

func TestStrayDotFileKeepsItsNameWhenRenamed(t *testing.T) {
	dir := t.TempDir()
	roWriteFile(t, filepath.Join(dir, "to/.mark"), "old\n")
	roWriteFile(t, filepath.Join(dir, "from/.mark"), "new\n")
	to, identical, err := moveKeepingBoth(filepath.Join(dir, "from/.mark"), filepath.Join(dir, "to/.mark"), nil)
	if err != nil || identical || filepath.Base(to) != ".mark.stray-1" {
		t.Fatalf("to %s identical %v err %v", to, identical, err)
	}
	mustSurvive(t, dir, "old\n", "new\n")
}

// Where the filesystem has no hard links the move copies, and the copy never
// replaces a file either.
func TestStrayCopyNoReplaceRefusesATakenName(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	roWriteFile(t, src, "new\n")
	roWriteFile(t, dst, "old\n")
	if err := copyNoReplace(src, dst); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want exists", err)
	}
	free := filepath.Join(dir, "free")
	if err := copyNoReplace(src, free); err != nil {
		t.Fatal(err)
	}
	mustSurvive(t, dir, "old\n", "new\n")
	if !sameFileContent(src, free) || sameFileContent(src, dst) {
		t.Error("content comparison is wrong")
	}
}
