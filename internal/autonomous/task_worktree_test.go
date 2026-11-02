package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// prdWorktreeFixture builds a real repo with a PRD worktree on its branch.
func prdWorktreeFixture(t *testing.T) (repo, wtDir string, prd *PRD) {
	t.Helper()
	repo = newTestRepo(t)
	wtDir = t.TempDir()
	prd = &PRD{ID: "abcd1234", Title: "Parallel work"}
	path, branch, err := EnsureWorktree(prd, repo, wtDir)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	prd.ProjectDir, prd.Git.Branch = path, branch
	return
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestEnsureTaskWorktreeBasedOnPRDHead(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	// A dependency's merged work lands on the PRD branch before the task starts.
	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "dep.txt"), []byte("dep"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, prd.ProjectDir, "config", "user.email", "t@datawatch")
	gitRun(t, prd.ProjectDir, "config", "user.name", "test")
	gitRun(t, prd.ProjectDir, "add", "dep.txt")
	gitRun(t, prd.ProjectDir, "commit", "-q", "-m", "dep merged")

	task := &Task{ID: "t1", Title: "Do the thing"}
	path, branch, err := EnsureTaskWorktree(prd, task, repo, wtDir)
	if err != nil {
		t.Fatalf("EnsureTaskWorktree: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join(wtDir, prd.ID+"-tasks")+string(filepath.Separator)) {
		t.Fatalf("path %s not under the PRD task root", path)
	}
	if !strings.HasPrefix(branch, prd.Git.Branch) || branch == prd.Git.Branch {
		t.Fatalf("branch %q should extend PRD branch %q", branch, prd.Git.Branch)
	}
	if _, err := os.Stat(filepath.Join(path, "dep.txt")); err != nil {
		t.Fatalf("task worktree missing the dependency's work: %v", err)
	}
	if got, want := gitOut(t, path, "rev-parse", "HEAD"), gitOut(t, prd.ProjectDir, "rev-parse", "HEAD"); got != want {
		t.Fatalf("task HEAD %s != PRD HEAD %s", got, want)
	}
	if got := gitOut(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
		t.Fatalf("checked-out branch %q != %q", got, branch)
	}
	// Idempotent.
	p2, b2, err := EnsureTaskWorktree(prd, task, repo, wtDir)
	if err != nil || p2 != path || b2 != branch {
		t.Fatalf("second call = %q %q %v", p2, b2, err)
	}
}

func TestEnsureTaskWorktreeRejectsBadInput(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	if _, _, err := EnsureTaskWorktree(prd, &Task{}, repo, wtDir); err == nil {
		t.Error("task with no id or title must be refused")
	}
	if _, _, err := EnsureTaskWorktree(&PRD{ID: "x"}, &Task{ID: "t"}, repo, wtDir); err == nil {
		t.Error("PRD without a branch must be refused")
	}
	if _, _, err := EnsureTaskWorktree(prd, &Task{ID: "t"}, "", wtDir); err == nil {
		t.Error("missing base repo must be refused")
	}
	// Traversal in the id/title cannot leave the task root.
	task := &Task{ID: "../../evil", Title: "../../escape"}
	path, _, err := EnsureTaskWorktree(prd, task, repo, wtDir)
	if err != nil {
		t.Fatalf("sanitised name should work: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join(wtDir, prd.ID+"-tasks")+string(filepath.Separator)) {
		t.Fatalf("path %s escaped the task root", path)
	}
}

func TestTreeKeyPerTaskWorktreeDoesNotContend(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	a := &Task{ID: "ta", Title: "a"}
	b := &Task{ID: "tb", Title: "b"}
	for _, task := range []*Task{a, b} {
		p, br, err := EnsureTaskWorktree(prd, task, repo, wtDir)
		if err != nil {
			t.Fatal(err)
		}
		task.WorktreePath, task.WorktreeBranch = p, br
	}
	ka, ra, err := ResolveTreeKey(prd, a)
	if err != nil {
		t.Fatal(err)
	}
	kb, _, _ := ResolveTreeKey(prd, b)
	kp, _, _ := ResolveTreeKey(prd, &Task{ID: "plain"})
	if ka == kb || ka == kp || kb == kp {
		t.Fatalf("keys must differ: %s %s %s", ka, kb, kp)
	}
	if ra != absClean(a.WorktreePath) || ka != treeKeyPrefix+absClean(a.WorktreePath) {
		t.Fatalf("task key %s root %s not on its own path", ka, ra)
	}
	if got := ResolveTaskProjectDir(prd, a); got != filepath.Clean(a.WorktreePath) {
		t.Fatalf("project dir = %s", got)
	}
}

func newTaskWorktreeManager(t *testing.T, repo, wtDir string, perTask bool) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.cfg.WorktreeBaseRepo, m.cfg.WorktreeDir = repo, wtDir
	m.cfg.Parallel.WorktreePerTask = &perTask
	m.mu.Unlock()
	return m
}

func TestWorktreePerTaskOffLeavesTaskInPRDWorktree(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	m := newTaskWorktreeManager(t, repo, wtDir, false)
	task := &Task{ID: "t1", Title: "x"}
	if err := m.ensureTaskWorktreeFor(prd, task); err != nil {
		t.Fatal(err)
	}
	if task.WorktreePath != "" {
		t.Fatalf("worktree created with the setting off: %s", task.WorktreePath)
	}
	if got := ResolveTaskProjectDir(prd, task); got != prd.ProjectDir {
		t.Fatalf("dir = %s, want PRD worktree", got)
	}
	k1, _, _ := ResolveTreeKey(prd, task)
	k2, _, _ := ResolveTreeKey(prd, nil)
	if k1 != k2 {
		t.Fatal("with the setting off the tree key must be the PRD's")
	}
}

func TestCleanupTaskWorktreesRespectsLiveSession(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	m := newTaskWorktreeManager(t, repo, wtDir, true)
	task := &Task{ID: "t1", Title: "x", Status: TaskCompleted}
	if err := m.ensureTaskWorktreeFor(prd, task); err != nil {
		t.Fatal(err)
	}
	if task.WorktreePath == "" {
		t.Fatal("expected a task worktree")
	}
	prd.Story = []Story{{Tasks: []Task{*task}}}
	path, br := task.WorktreePath, task.WorktreeBranch
	ctx := context.Background()

	// Live session in the worktree: kept, even for an ended PRD.
	prd.Status = PRDCompleted
	m.SetSessionListerFn(func() []SessionInfo { return []SessionInfo{{Alive: true, ProjectDir: path}} })
	if n := m.CleanupTaskWorktrees(ctx, prd); n != 0 {
		t.Fatalf("removed %d with a live session", n)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("worktree removed under a live session")
	}

	// Session gone and branch merged (no commits): removed with its branch.
	m.SetSessionListerFn(func() []SessionInfo { return nil })
	if n := m.CleanupTaskWorktrees(ctx, prd); n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("worktree still present")
	}
	if out := gitOut(t, repo, "branch", "--list", br); out != "" {
		t.Fatalf("branch %s still present", br)
	}
}

func TestCleanupTaskWorktreesKeepsUnmergedWhilePRDRuns(t *testing.T) {
	repo, wtDir, prd := prdWorktreeFixture(t)
	m := newTaskWorktreeManager(t, repo, wtDir, true)
	task := &Task{ID: "t1", Title: "x", Status: TaskCompleted}
	if err := m.ensureTaskWorktreeFor(prd, task); err != nil {
		t.Fatal(err)
	}
	gitRun(t, task.WorktreePath, "config", "user.email", "t@datawatch")
	gitRun(t, task.WorktreePath, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(task.WorktreePath, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, task.WorktreePath, "add", "f.txt")
	gitRun(t, task.WorktreePath, "commit", "-q", "-m", "work")
	prd.Status = PRDRunning
	prd.Story = []Story{{Tasks: []Task{*task}}}
	if n := m.CleanupTaskWorktrees(context.Background(), prd); n != 0 {
		t.Fatalf("removed unmerged worktree of a running PRD")
	}
	// Merge it into the PRD branch: now removable.
	gitRun(t, prd.ProjectDir, "config", "user.email", "t@datawatch")
	gitRun(t, prd.ProjectDir, "config", "user.name", "test")
	gitRun(t, prd.ProjectDir, "merge", "-q", "--no-edit", task.WorktreeBranch)
	if n := m.CleanupTaskWorktrees(context.Background(), prd); n != 1 {
		t.Fatalf("removed %d after merge, want 1", n)
	}
}
