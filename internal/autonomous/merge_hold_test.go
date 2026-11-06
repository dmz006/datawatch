package autonomous

import (
	"context"
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
