package autonomous

import (
	"context"
	"path/filepath"
	"testing"
)

func brManager(t *testing.T, enabled bool, f *brFake) (*Manager, *PRD, *QuestionStore) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, Config{BranchRefreshEnabled: enabled}, nil)
	if err != nil {
		t.Fatal(err)
	}
	qs, err := NewQuestionStore(filepath.Join(dir, "questions.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.SetDecisionGate(qs, nil)
	m.branchRefreshGitRunner = f
	prd := &PRD{ID: "p1", Status: PRDRunning, ProjectDir: "/wt", Git: GitWorkflow{BaseBranch: "main"}}
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return m, prd, qs
}

func brGit(behind string) *brFake {
	return &brFake{out: map[string]string{"rev-list --count HEAD..main": behind, "rev-parse main": "abc\n"}, fail: map[string]bool{}}
}

func brResult(m *Manager) *BranchRefresh {
	p, _ := m.store.GetPRD("p1")
	return p.BranchRefresh
}

func TestMaybeRefreshBranchOffAndOverride(t *testing.T) {
	f := brGit("9\n")
	m, prd, _ := brManager(t, false, f)
	m.maybeRefreshBranch(context.Background(), prd)
	if len(f.calls) != 0 {
		t.Fatalf("off: git ran %v", f.calls)
	}
	on := true
	prd.BranchRefreshOverride = &on
	m.maybeRefreshBranch(context.Background(), prd)
	if brResult(m) == nil || brResult(m).LastResult != BranchRefreshClean {
		t.Fatalf("override on: %+v", brResult(m))
	}
	m2, prd2, _ := brManager(t, true, brGit("9\n"))
	off := false
	prd2.BranchRefreshOverride = &off
	m2.maybeRefreshBranch(context.Background(), prd2)
	if brResult(m2) != nil {
		t.Fatal("override off must win over config on")
	}
}

func TestMaybeRefreshBranchBelowLimitAndGuards(t *testing.T) {
	f := brGit("4\n")
	m, prd, _ := brManager(t, true, f)
	m.maybeRefreshBranch(context.Background(), prd)
	if f.called("merge --no-ff --no-edit main") || brResult(m) != nil {
		t.Fatal("below limit must not merge")
	}
	f2 := brGit("9\n")
	m2, prd2, _ := brManager(t, true, f2)
	prd2.BranchRefresh = &BranchRefresh{Stopped: true}
	m2.maybeRefreshBranch(context.Background(), prd2)
	if len(f2.calls) != 0 {
		t.Fatal("stopped must not touch git")
	}
	f3 := brGit("9\n")
	m3, prd3, _ := brManager(t, true, f3)
	prd3.Story = []Story{{Tasks: []Task{{ID: "t1", Status: TaskInProgress}}}}
	m3.maybeRefreshBranch(context.Background(), prd3)
	if len(f3.calls) != 0 {
		t.Fatal("running task must block refresh")
	}
}

func TestMaybeRefreshBranchCleanAndConflict(t *testing.T) {
	f := brGit("5\n")
	m, prd, _ := brManager(t, true, f)
	m.maybeRefreshBranch(context.Background(), prd)
	r := brResult(m)
	if r == nil || r.LastResult != BranchRefreshClean || r.CommitsBehind != 5 || r.LastMainCommit != "abc" || r.Stopped {
		t.Fatalf("clean: %+v", r)
	}

	f2 := brGit("6\n")
	f2.fail["merge --no-ff --no-edit main"] = true
	f2.out["diff --name-only --diff-filter=U"] = "a.go\n"
	m2, prd2, qs := brManager(t, true, f2)
	m2.maybeRefreshBranch(context.Background(), prd2)
	r2 := brResult(m2)
	if r2 == nil || r2.LastResult != BranchRefreshConflict || !r2.Stopped {
		t.Fatalf("conflict: %+v", r2)
	}
	if !f2.called("merge --abort") {
		t.Fatal("conflict must leave the branch as it was")
	}
	if n := len(qs.List(QuestionFilter{})); n != 1 {
		t.Fatalf("questions = %d, want 1", n)
	}
}
