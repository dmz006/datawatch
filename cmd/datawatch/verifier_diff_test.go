package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.test")
	run("config", "user.name", "t")
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", name).CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
}

// Reproduces PRD 0fb4e302's failure mode: a worker edits a pre-existing
// TRACKED file without committing (session.auto_git_commit=false, the
// operator default) — the commit-range diff the verifier used to rely on
// exclusively stays empty because HEAD never moves.
func TestVerifierDiff_UncommittedEditToTrackedFile_CommitRangeDiffIsEmpty(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "spec.md", "stub\n")
	preTaskSHA := gitHead(t, dir)
	taskStart := time.Now()
	time.Sleep(20 * time.Millisecond) // clear filesystem mtime resolution vs time.Now() skew

	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("stub\nreal content written by the worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	committedDiff, err := exec.CommandContext(context.Background(), "git", "-C", dir, "diff", preTaskSHA+"..HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(committedDiff) != 0 {
		t.Fatalf("setup invariant broken: commit-range diff should be empty pre-fix, got %q", committedDiff)
	}

	workingDiff, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatalf("gitWorkingTreeDiffSince: %v", err)
	}
	if len(workingDiff) == 0 {
		t.Fatal("gitWorkingTreeDiffSince must see the uncommitted edit made during this task's run")
	}
	if taskProducedNoOutput(committedDiff, workingDiff, nil, nil) {
		t.Fatal("a real uncommitted edit must not be reported as no output")
	}
}

// Reproduces the contamination this scoping fix closes, observed live on PRD
// 0fb4e302: task 4 and task 5 both passed verification within seconds of
// spawning, against an uncommitted tracked-file edit that had been sitting in
// the working tree since BEFORE either task started (an unrelated concurrent
// source edit to the same project_dir). An unscoped `git diff HEAD` cannot
// tell that edit apart from the worker's own; scoping by mtime-since-start can.
func TestVerifierDiff_LeftoverUncommittedEditFromBeforeTaskStart_IsExcluded(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "unrelated.go", "package x\n")

	// Someone (an earlier task, or a concurrent unrelated editor) leaves an
	// uncommitted change BEFORE this task starts.
	if err := os.WriteFile(filepath.Join(dir, "unrelated.go"), []byte("package x\n// stale edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	taskStart := time.Now()

	workingDiff, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(workingDiff) != 0 {
		t.Fatalf("a change made before task start must not count as this task's evidence, got %q", workingDiff)
	}
	if !taskProducedNoOutput(nil, workingDiff, nil, nil) {
		t.Fatal("with only a pre-existing leftover diff, this task must still be reported as having produced no output")
	}
}

// A change made before start (leftover) AND a change made after start (real
// work) coexist: only the post-start file is picked up.
func TestVerifierDiff_MixOfLeftoverAndRealChange_OnlyRecentCounted(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "leftover.md", "a\n")
	commitFile(t, dir, "target.md", "stub\n")
	os.WriteFile(filepath.Join(dir, "leftover.md"), []byte("a\nstale\n"), 0o644) //nolint:errcheck

	time.Sleep(20 * time.Millisecond)
	taskStart := time.Now()
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\nreal work\n"), 0o644) //nolint:errcheck

	workingDiff, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workingDiff), "target.md") {
		t.Fatal("the real, recent change must be included")
	}
	if strings.Contains(string(workingDiff), "leftover.md") {
		t.Fatal("the pre-existing leftover change must be excluded")
	}
}

func TestVerifierDiff_NoOutputWhenNothingChanged(t *testing.T) {
	if !taskProducedNoOutput(nil, nil, nil, nil) {
		t.Fatal("all-empty signals must mean no output")
	}
}

func TestVerifierDiff_AnySingleSignalCounts(t *testing.T) {
	cases := []struct {
		name                      string
		committed, working        []byte
		newUntracked, overwritten []string
	}{
		{"committed diff", []byte("+x"), nil, nil, nil},
		{"working diff", nil, []byte("+x"), nil, nil},
		{"new untracked file", nil, nil, []string{"a.md"}, nil},
		{"overwritten pre-existing file", nil, nil, nil, []string{"File: a.md\ncontent"}},
	}
	for _, c := range cases {
		if taskProducedNoOutput(c.committed, c.working, c.newUntracked, c.overwritten) {
			t.Errorf("%s: must count as output", c.name)
		}
	}
}

// Auto-commit still works: when session.auto_git_commit=true and the worker's
// changes get committed during the session, the commit-range diff alone is
// non-empty and the working-tree diff is clean.
func TestVerifierDiff_CommittedChangeAloneCounts(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "spec.md", "stub\n")
	preTaskSHA := gitHead(t, dir)
	taskStart := time.Now()
	commitFile(t, dir, "spec.md", "stub\nreal content\n")

	committedDiff, err := exec.CommandContext(context.Background(), "git", "-C", dir, "diff", preTaskSHA+"..HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	workingDiff, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(committedDiff) == 0 {
		t.Fatal("committed change must be visible in the commit-range diff")
	}
	if len(workingDiff) != 0 {
		t.Fatal("nothing uncommitted should remain after the auto-commit")
	}
	if taskProducedNoOutput(committedDiff, workingDiff, nil, nil) {
		t.Fatal("committed change alone must count as output")
	}
}
