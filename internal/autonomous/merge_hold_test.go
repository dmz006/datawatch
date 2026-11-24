package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pause"
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

// A merge hold pass is a new piece of work the engine starts on its own, so it
// stays quiet while the operator has paused the project and resumes when the
// pause ends (item 17, item 22).
func TestMergeHoldPass_SkippedWhilePaused(t *testing.T) {
	f := holdFixture(t)
	ps := newPauseStore(t)
	f.m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeGlobal, "", "freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	wake := make(chan struct{})
	close(wake)
	if !f.m.awaitMergeLanding(context.Background(), f.prd, wake) {
		t.Fatal("awaitMergeLanding reported a stopped context")
	}
	if st := f.task("t1").MergeState; st == MergeStateMerged {
		t.Fatalf("a paused project ran a merge pass: t1 merge state %q", st)
	}
	if _, err := ps.Resume(pause.ScopeGlobal, "", "operator", ""); err != nil {
		t.Fatal(err)
	}
	if !f.m.awaitMergeLanding(context.Background(), f.prd, wake) {
		t.Fatal("awaitMergeLanding reported a stopped context")
	}
	if st := f.task("t1").MergeState; st != MergeStateMerged {
		t.Fatalf("after the resume the pass must land the merge, t1 merge state %q", st)
	}
}

func TestMergeHoldPass_BacksOffWhileMergeStaysBlocked(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.prd.Story[0].Tasks = append(f.prd.Story[0].Tasks,
		Task{ID: "t2", PRDID: f.prd.ID, Title: "next", Status: TaskPending})
	f.seal()
	f.dirtyTree()
	now := time.Unix(1_000_000, 0)
	old := mergeHoldClock
	mergeHoldClock = func() time.Time { return now }
	t.Cleanup(func() { mergeHoldClock = old })
	ctx := context.Background()

	if !f.m.runMergeHoldPass(ctx, f.prd) {
		t.Fatal("the first pass must run")
	}
	if f.task("t1").MergeBlock == nil {
		t.Fatal("the dirty project tree must leave a merge block")
	}
	if st := f.m.mergeHoldBack[f.prd.ID]; st == nil || st.fails != 1 {
		t.Fatalf("backoff state after one blocked pass = %+v", st)
	}
	// Inside the wait no pass runs.
	now = now.Add(mergeHoldPoll / 2)
	if f.m.runMergeHoldPass(ctx, f.prd) {
		t.Fatal("a pass ran inside the backoff")
	}
	// After the wait it runs, stays blocked and the wait doubles.
	now = now.Add(mergeHoldPoll)
	if !f.m.runMergeHoldPass(ctx, f.prd) {
		t.Fatal("the pass must run once the backoff ended")
	}
	if st := f.m.mergeHoldBack[f.prd.ID]; st.fails != 2 || !st.next.Equal(now.Add(2*mergeHoldPoll)) {
		t.Fatalf("second blocked pass: %+v, want the next try in %s", st, 2*mergeHoldPoll)
	}
	// The wait never passes the cap.
	for i := 0; i < 20; i++ {
		now = now.Add(mergeHoldBackoffMax + time.Second)
		f.m.runMergeHoldPass(ctx, f.prd)
	}
	if st := f.m.mergeHoldBack[f.prd.ID]; st.next.Sub(now) > mergeHoldBackoffMax {
		t.Fatalf("backoff %s passes the cap %s", st.next.Sub(now), mergeHoldBackoffMax)
	}
	// Once the tree is clean the pass merges and the backoff is cleared.
	f.cleanTree()
	now = now.Add(mergeHoldBackoffMax + time.Second)
	f.m.runMergeHoldPass(ctx, f.prd)
	if st := f.m.mergeHoldBack[f.prd.ID]; st != nil || f.task("t1").MergeState != MergeStateMerged {
		t.Fatalf("a working pass must clear the backoff: state %+v, merge %q", st, f.task("t1").MergeState)
	}
}

func TestMergeRetryPass_SkipsPausedProject(t *testing.T) {
	f := holdFixture(t)
	ps := newPauseStore(t)
	f.m.SetPauseGate(ps)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	if n := f.m.MergeRetryPass(context.Background()); n != 0 || f.task("t1").MergeState == MergeStateMerged {
		t.Fatalf("a paused project merged %d task(s)", n)
	}
	_, _ = ps.Resume(pause.ScopeGlobal, "", "operator", "")
	if n := f.m.MergeRetryPass(context.Background()); n != 1 {
		t.Fatalf("after the resume the retry pass merged %d task(s), want 1", n)
	}
}
