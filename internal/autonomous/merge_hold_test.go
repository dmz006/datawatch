package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeHoldReason(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "a\n")
	f.prd.Story[0].Tasks = append(f.prd.Story[0].Tasks,
		Task{ID: "t2", PRDID: f.prd.ID, Title: "next", Status: TaskPending},
		Task{ID: "t3", PRDID: f.prd.ID, Title: "ro", Status: TaskPending, ReadOnly: true})
	f.seal()
	next := f.task("t2")

	// Completed and unmerged (empty state): the next task is held.
	got := f.m.mergeHoldReason(f.prd, next)
	if !strings.Contains(got, "waiting for the merge of task t1") {
		t.Fatalf("reason = %q", got)
	}
	// The earlier task is never held by itself.
	if r := f.m.mergeHoldReason(f.prd, f.task("t1")); r != "" {
		t.Fatalf("t1 held: %q", r)
	}
	// The hold records a note and leaves Status alone.
	if !f.m.mergeHold(f.prd, next) || next.WaitReason != got || next.Status != TaskPending {
		t.Fatalf("note %q status %q", next.WaitReason, next.Status)
	}
	// Conflict and held merge states do not hold.
	for _, st := range []string{MergeStateConflict, MergeStateHeld} {
		f.task("t1").MergeState = st
		if r := f.m.mergeHoldReason(f.prd, next); r != "" {
			t.Fatalf("%s held: %q", st, r)
		}
	}
	f.task("t1").MergeState = MergeStatePending
	if f.m.mergeHoldReason(f.prd, next) == "" {
		t.Fatal("pending state must hold")
	}
	// The merge landing clears the hold and the note.
	if _, err := f.m.MergeCompletedTasks(context.Background(), f.prd); err != nil {
		t.Fatal(err)
	}
	if f.m.mergeHold(f.prd, next) || next.WaitReason != "" {
		t.Fatalf("still held: %q", next.WaitReason)
	}
}

// holdFixture is one completed, unmerged task t1 and one pending task t2.
func holdFixture(t *testing.T) *mergeFixture {
	t.Helper()
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "a\n")
	f.prd.Story[0].Tasks = append(f.prd.Story[0].Tasks,
		Task{ID: "t2", PRDID: f.prd.ID, Title: "next", Status: TaskPending})
	f.seal()
	return f
}

func TestMergeHoldDelaysNextTaskWhileEarlierPending(t *testing.T) {
	f := holdFixture(t)
	next := f.task("t2")
	if !f.m.mergeHold(f.prd, next) {
		t.Fatal("second task not held while the first is unmerged")
	}
	if !f.m.mergeHoldPending(f.prd) {
		t.Fatal("mergeHoldPending false with a held task")
	}
	if next.Status != TaskPending || !strings.Contains(next.WaitReason, "task t1") {
		t.Fatalf("status %q note %q", next.Status, next.WaitReason)
	}
	if next.WorktreePath != "" {
		t.Fatal("held task got a worktree")
	}
}

func TestMergeHoldReleasesAfterMerge(t *testing.T) {
	f := holdFixture(t)
	next := f.task("t2")
	if !f.m.mergeHold(f.prd, next) {
		t.Fatal("not held before the merge")
	}
	if n, err := f.m.MergeCompletedTasks(context.Background(), f.prd); err != nil || n != 1 {
		t.Fatalf("merged %d err %v", n, err)
	}
	if f.m.mergeHold(f.prd, next) || next.WaitReason != "" || f.m.mergeHoldPending(f.prd) {
		t.Fatalf("still held after merge: %q", next.WaitReason)
	}
}

func TestMergeHoldIgnoresConflictAndHeldStates(t *testing.T) {
	f := holdFixture(t)
	next := f.task("t2")
	for _, st := range []string{MergeStateConflict, MergeStateHeld, MergeStateMerged} {
		f.task("t1").MergeState = st
		if r := f.m.mergeHoldReason(f.prd, next); r != "" {
			t.Fatalf("%s state held the next task: %q", st, r)
		}
	}
	for _, st := range []string{"", MergeStatePending} {
		f.task("t1").MergeState = st
		if f.m.mergeHoldReason(f.prd, next) == "" {
			t.Fatalf("state %q must hold", st)
		}
	}
}

func TestMergeHoldSkipsReadOnlyTask(t *testing.T) {
	f := holdFixture(t)
	f.prd.Story[0].Tasks = append(f.prd.Story[0].Tasks,
		Task{ID: "ro", PRDID: f.prd.ID, Title: "read only", Status: TaskPending, ReadOnly: true})
	f.seal()
	on := true
	f.m.mu.Lock()
	f.m.cfg.Parallel.ReadOnlyTasks = &on
	f.m.mu.Unlock()
	if r := f.m.mergeHoldReason(f.prd, f.task("ro")); r != "" {
		t.Fatalf("read-only task held: %q", r)
	}
	if f.m.mergeHoldReason(f.prd, f.task("t2")) == "" {
		t.Fatal("writing task must still be held")
	}
}

func TestMergeHoldOffWithoutTaskWorktrees(t *testing.T) {
	f := holdFixture(t)
	next := f.task("t2")
	off := false
	f.m.mu.Lock()
	f.m.cfg.Parallel.WorktreePerTask = &off
	f.m.mu.Unlock()
	if f.m.mergeHold(f.prd, next) || f.m.mergeHoldPending(f.prd) {
		t.Fatal("held with worktree_per_task off")
	}
	on := true
	f.m.mu.Lock()
	f.m.cfg.Parallel.WorktreePerTask = &on
	f.m.mu.Unlock()
	f.prd.Git.Branch = ""
	if r := f.m.mergeHoldReason(f.prd, next); r != "" {
		t.Fatalf("held with no Automaton branch: %q", r)
	}
}

func TestMergeHoldDocumentationTaskSeesEarlierCode(t *testing.T) {
	f := holdFixture(t)
	doc := f.task("t2")
	doc.Title = "document the feature"
	if !f.m.mergeHold(f.prd, doc) {
		t.Fatal("documentation task not held")
	}
	// Held: no worktree is cut from the stale head.
	if doc.WorktreePath != "" {
		t.Fatal("worktree created while held")
	}
	if n, err := f.m.MergeCompletedTasks(context.Background(), f.prd); err != nil || n != 1 {
		t.Fatalf("merged %d err %v", n, err)
	}
	if f.m.mergeHold(f.prd, doc) {
		t.Fatal("still held after the merge")
	}
	if err := f.m.ensureTaskWorktreeFor(f.prd, doc); err != nil {
		t.Fatal(err)
	}
	if doc.WorktreePath == "" {
		t.Fatal("no worktree for the released task")
	}
	b, err := os.ReadFile(filepath.Join(doc.WorktreePath, "a.txt"))
	if err != nil || string(b) != "a\n" {
		t.Fatalf("later worktree lacks the earlier task's file: %q %v", b, err)
	}
}
