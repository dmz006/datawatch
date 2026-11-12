package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	dsmBaseRules = "Always run go test ./...\n"

	// The daemon's own sections, in the shape the daemon itself injects at
	// session start: the RTK marker block, then the Memory & Knowledge
	// section (which runs to the end of the file).
	dsmDaemon = "\n<!-- rtk-instructions -->\nAlways prefix commands with rtk.\n<!-- /rtk-instructions -->\n\n# Memory & Knowledge (datawatch)\n\n- Use the memory system proactively.\n"
)

func dsmWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dsm is a PRD worktree (with an optional base rules file), a per-task
// worktree on its own branch cut from the PRD head, and a manager wired for
// worktrees — the exact shape MergeTaskBranch works in.
type dsm struct {
	t      *testing.T
	m      *Manager
	prd    *PRD
	task   *Task
	dir    string // PRD worktree, on the PRD branch
	path   string // task worktree, HEAD on the task branch
	branch string
	base   string // commit the task branch was cut from
}

func dsmFixture(t *testing.T, baseRules string) *dsm {
	t.Helper()
	repo := newTestRepo(t)
	wtDir := t.TempDir()
	prd := &PRD{ID: "abcd1234", Title: "DSM"}
	p0, b0, err := EnsureWorktree(prd, repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	prd.ProjectDir, prd.Git.Branch = p0, b0
	if baseRules != "" {
		dsmWrite(t, p0, DefaultRulesFile, baseRules)
		gitRun(t, p0, "add", DefaultRulesFile)
		gitRun(t, p0, "commit", "-q", "-m", "base rules")
	}
	m := newTaskWorktreeManager(t, repo, wtDir, true)
	task := Task{ID: "t1", PRDID: prd.ID, Title: "rules work", Status: TaskCompleted}
	p1, b1, err := EnsureTaskWorktree(prd, &task, repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath, task.WorktreeBranch, task.BaseSHA = p1, b1, gitOut(t, p1, "rev-parse", "HEAD")
	if len(prd.Story) == 0 {
		prd.Story = []Story{{ID: "s1"}}
	}
	prd.Story[0].Tasks = append(prd.Story[0].Tasks, task)
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	f := &dsm{t: t, m: m, prd: prd, task: &prd.Story[0].Tasks[0], dir: p0, path: p1, branch: b1, base: task.BaseSHA}
	return f
}

// commit lands one task commit in the task worktree.
func (f *dsm) commit(name, body string) {
	f.t.Helper()
	dsmWrite(f.t, f.path, name, body)
	gitRun(f.t, f.path, "add", name)
	gitRun(f.t, f.path, "commit", "-q", "-m", "work: "+name)
}

func (f *dsm) headCount() int {
	f.t.Helper()
	n, err := strconv.Atoi(gitOut(f.t, f.path, "rev-list", "--count", "HEAD"))
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *dsm) branchFile(name string) string {
	f.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", f.path, "show", "HEAD:" + name}, "--")...).Output()
	if err != nil {
		f.t.Fatalf("git show HEAD:%s: %v", name, err)
	}
	return string(out) // no trimming: byte-exact blob comparison
}

func (f *dsm) prdFile(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func (f *dsm) merge(out MergeOutcome) {
	f.t.Helper()
	got, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task)
	if err != nil || got != out {
		f.t.Fatalf("MergeTaskBranch = %s, want %s: %v", got, out, err)
	}
}

// TestDaemonSectionsMergeDaemonOnlyCommitRestoredToBase: a task branch whose
// only commit to the rules file adds the daemon's own sections comes back
// cleaned to the base blob, and the merge leaves the base branch's rules file
// exactly as it was.
func TestDaemonSectionsMergeDaemonOnlyCommitRestoredToBase(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	f.commit(DefaultRulesFile, dsmBaseRules+dsmDaemon)

	before := f.headCount()
	if err := f.m.cleanDaemonSectionsBeforeMerge(context.Background(), f.path, f.base); err != nil {
		t.Fatal(err)
	}
	if got := f.headCount(); got != before+1 {
		t.Fatalf("cleanup commits = %d, want exactly 1", got-before)
	}
	if got := f.branchFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("branch rules file after cleanup = %q, want the base blob %q", got, dsmBaseRules)
	}

	f.merge(MergeOutcomeMerged)
	if got := f.prdFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("base branch rules file after merge = %q, want unchanged %q", got, dsmBaseRules)
	}
}

// TestDaemonSectionsMergeRealEditKeptSectionDropped: a branch with a real
// edit and the daemon sections merges the edit, not the sections.
func TestDaemonSectionsMergeRealEditKeptSectionDropped(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	f.commit(DefaultRulesFile, dsmBaseRules+"And run go vet ./...\n"+dsmDaemon)

	if err := f.m.cleanDaemonSectionsBeforeMerge(context.Background(), f.path, f.base); err != nil {
		t.Fatal(err)
	}
	want := dsmBaseRules + "And run go vet ./...\n"
	if got := f.branchFile(DefaultRulesFile); got != want {
		t.Fatalf("branch rules file after cleanup = %q, want the edit kept and the sections dropped %q", got, want)
	}

	f.merge(MergeOutcomeMerged)
	if got := f.prdFile(DefaultRulesFile); got != want {
		t.Fatalf("base branch rules file after merge = %q, want %q", got, want)
	}
	for _, marker := range []string{"Memory & Knowledge (datawatch)", "<!-- rtk-instructions", "prefix commands with rtk"} {
		if got := f.prdFile(DefaultRulesFile); strings.Contains(got, marker) {
			t.Fatalf("daemon section %q survived the merge: %q", marker, got)
		}
	}
}

// TestDaemonSectionsMergeUntouchedRulesFileNoCleanupCommit: a branch that
// left the rules file alone gets no cleanup commit at all, and its merge
// still lands.
func TestDaemonSectionsMergeUntouchedRulesFileNoCleanupCommit(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	f.commit("work.txt", "done\n")

	before := f.headCount()
	if err := f.m.cleanDaemonSectionsBeforeMerge(context.Background(), f.path, f.base); err != nil {
		t.Fatal(err)
	}
	if got := f.headCount(); got != before {
		t.Fatalf("cleanup commits = %d, want 0 (the branch never touched the rules file)", got-before)
	}

	f.merge(MergeOutcomeMerged)
	if got := f.prdFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("base branch rules file after merge = %q, want unchanged %q", got, dsmBaseRules)
	}
	if got := f.prdFile("work.txt"); got != "done\n" {
		t.Fatalf("task work not merged: %q", got)
	}
}
