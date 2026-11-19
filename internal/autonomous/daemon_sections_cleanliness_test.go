package autonomous

// This file is the one place the four rules-file cleanliness checks are
// tested together against one real temporary git repository:
//
//   - DetectUncommittedWork (uncommitted work after a task),
//   - pathspecWithoutDaemonOnlyChanges (the protected-paths daemon-section
//     filter),
//   - readOnlyViolation (a read-only task against an empty baseline),
//   - MergeTaskBranch's dirty-tree preflight (the merge gate).
//
// The fixture constants and file helpers are the shared ones from
// protected_paths_daemon_sections_test.go (baseRulesContent,
// daemonSectionsBlock, writeRulesFile) and the uncommitted-work tests
// (uwRepo, uwWrite, uwGit); nothing here is copied from them.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// csdManager makes a manager carrying only what these checks touch: a store
// for MergeTaskBranch to record its outcome on the task. The other three
// checks do not read the manager's state at all.
func csdManager(t *testing.T) *Manager {
	t.Helper()
	st, err := NewStore(filepath.Join(t.TempDir(), "prds.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &Manager{store: st}
}

// csdRepo builds one temporary git repository: a committed AGENT.md carrying
// the real rules body plus a source file, and two task branches — one per
// scenario — each holding its own commit, so a merge preflight reaches the
// dirty-tree gate instead of short-circuiting on an already-merged branch.
// The working tree is left on the base branch, clean.
func csdRepo(t *testing.T) (string, string, string) {
	t.Helper()
	dir := uwRepo(t)                         // git init plus the committed source file base.txt
	writeRulesFile(t, dir, baseRulesContent) // AGENT.md with the real rules body
	uwGit(t, dir, "add", "AGENT.md")
	uwGit(t, dir, "commit", "-q", "-m", "rules")
	baseBranch := uwGit(t, dir, "symbolic-ref", "--short", "HEAD")
	for _, branch := range []string{"task/first", "task/second"} {
		uwGit(t, dir, "checkout", "-q", "-b", branch)
		uwWrite(t, dir, "base.txt", "base\nwork of "+branch+"\n")
		uwGit(t, dir, "add", "base.txt")
		uwGit(t, dir, "commit", "-q", "-m", "task work")
		uwGit(t, dir, "checkout", "-q", baseBranch)
	}
	return dir, "task/first", "task/second"
}

// csdPRD binds the repo to a PRD whose story holds two completed tasks, each
// owning one of the task branches.
func csdPRD(t *testing.T, dir, branch1, branch2 string) (*PRD, *Task, *Task) {
	t.Helper()
	prd := &PRD{ID: "csd-p1", ProjectDir: dir, Story: []Story{{ID: "s1", Tasks: []Task{
		{ID: "t1", PRDID: "csd-p1", Title: "first", Status: TaskCompleted, WorktreeBranch: branch1},
		{ID: "t2", PRDID: "csd-p1", Title: "second", Status: TaskCompleted, WorktreeBranch: branch2},
	}}}}
	t1, t2 := &prd.Story[0].Tasks[0], &prd.Story[0].Tasks[1]
	return prd, t1, t2
}

// TestCleanlinessDaemonSectionOnlyChangeRaisesNothing is the daemon-only
// case: the daemon wrote its own sections into the working copy of AGENT.md
// and nothing was committed. All four checks must pass, and after a real
// operator edit outside the markers all four must report it.
func TestCleanlinessDaemonSectionOnlyChangeRaisesNothing(t *testing.T) {
	dir, b1, b2 := csdRepo(t)
	prd, t1, t2 := csdPRD(t, dir, b1, b2)
	m := csdManager(t)
	ctx := context.Background()

	// The daemon injected its sections into the working copy only.
	writeRulesFile(t, dir, baseRulesContent+daemonSectionsBlock)

	// 1. Uncommitted work: a daemon-section-only change is not the task's.
	files, err := DetectUncommittedWork(ctx, dir, nil)
	if err != nil || len(files) != 0 {
		t.Fatalf("DetectUncommittedWork = %v, %v; want no entries", files, err)
	}

	// 2. Protected paths: the daemon-only rules file leaves the pathspec.
	if kept := m.pathspecWithoutDaemonOnlyChanges(ctx, dir, []string{"AGENT.md"}); len(kept) != 0 {
		t.Fatalf("pathspec = %v; a daemon-section-only change must not hold", kept)
	}

	// 3. Read-only: an empty baseline means anything left after the
	// daemon-section filter is new dirt; nothing is left.
	if err := m.readOnlyViolation(ctx, prd, t1, ""); err != nil {
		t.Fatalf("readOnlyViolation = %v; a daemon-only change must not violate", err)
	}

	// 4. Merge preflight: the daemon-only rules file does not block.
	out, err := m.MergeTaskBranch(ctx, prd, t1)
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("merge = %s, %v; the preflight must pass", out, err)
	}

	// Now a real operator edit lands outside the markers.
	writeRulesFile(t, dir, baseRulesContent+
		"\nOperator edit: review the testing tracker before committing.\n"+daemonSectionsBlock)

	// 1. Uncommitted work lists AGENT.md again.
	files, err = DetectUncommittedWork(ctx, dir, nil)
	if err != nil || len(files) != 1 || files[0].Path != "AGENT.md" {
		t.Fatalf("DetectUncommittedWork = %v, %v; the real edit must be listed", files, err)
	}

	// 2. Protected paths hold AGENT.md again.
	if kept := m.pathspecWithoutDaemonOnlyChanges(ctx, dir, []string{"AGENT.md"}); !sameSet(kept, []string{"AGENT.md"}) {
		t.Fatalf("pathspec = %v; the real edit must stay in", kept)
	}

	// 3. Read-only reports a violation naming the file.
	if err := m.readOnlyViolation(ctx, prd, t1, ""); err == nil {
		t.Fatal("readOnlyViolation = nil; a real edit must violate")
	} else if !strings.Contains(err.Error(), "AGENT.md") {
		t.Fatalf("readOnlyViolation = %v; want the error to name AGENT.md", err)
	}

	// 4. The merge preflight refuses over the real edit.
	out, err = m.MergeTaskBranch(ctx, prd, t2)
	if err != nil || out != MergeOutcomeDeferred {
		t.Fatalf("merge = %s, %v; the real edit must defer the merge", out, err)
	}
	if t2.MergeBlock == nil || t2.MergeBlock.Kind != MergeBlockKindDirtyWorktree ||
		!strings.Contains(t2.MergeBlock.Reason, "AGENT.md") {
		t.Fatalf("merge block = %+v; want a dirty_worktree block naming AGENT.md", t2.MergeBlock)
	}
}
