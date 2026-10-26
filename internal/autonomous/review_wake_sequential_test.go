package autonomous

import (
	"context"
	"testing"
	"time"
)

const (
	seqFirstTitle = "First"
	seqHeldTitle  = "Held"
)

// sequentialWakeRig is a single-slot Automaton of two tasks. The held task comes
// first in the order, so the walk skips it and moves on. The first task then
// runs and holds its verifier until release, so the pass is still live when the
// operator acts on the held task, which the walk has already passed.
type sequentialWakeRig struct {
	m       *Manager
	prdID   string
	heldID  string
	started chan struct{}
	release chan struct{}
	spawns  escalationSpawnLog
	wake    chan struct{}
}

func newSequentialWakeRig(t *testing.T) *sequentialWakeRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	// Each task gets its own directory so no task queues behind another.
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: []Task{
		{Title: seqHeldTitle, Spec: "do the held task", ProjectDir: t.TempDir()},
		{Title: seqFirstTitle, Spec: "do the first task", ProjectDir: t.TempDir()},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	held := &prd.Story[0].Tasks[0]
	held.Status = TaskReviewNeeded
	held.ReviewSummary = &ReviewSummary{Text: "held for the sequential wake test"}
	prd.MaxConcurrentTasks = 1
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if err := m.Store().SaveTask(held); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	r := &sequentialWakeRig{
		m:       m,
		prdID:   prd.ID,
		heldID:  held.ID,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		wake:    make(chan struct{}, 1),
	}
	t.Cleanup(func() {
		select {
		case <-r.release:
		default:
			close(r.release)
		}
	})
	return r
}

// verify passes every task. The first task reports that it has started, then
// holds its verifier until release.
func (r *sequentialWakeRig) verify(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
	if task.Title == seqFirstTitle {
		r.started <- struct{}{}
		<-r.release
	}
	return VerificationResult{OK: true, Summary: "ok"}, nil
}

// setHeldStatus is the operator's decision on the held task: a retry sets it
// pending, a cancel sets it cancelled.
func (r *sequentialWakeRig) setHeldStatus(t *testing.T, s TaskStatus) {
	t.Helper()
	prd, _ := r.m.Store().GetPRD(r.prdID)
	task := lookupTask(prd, r.heldID)
	r.m.Store().WithLock(func() {
		task.Status = s
		task.ReviewSummary = nil
	})
	if err := r.m.Store().SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
}

// nudge sends one wake without blocking, as a review decision does.
func (r *sequentialWakeRig) nudge(t *testing.T) {
	t.Helper()
	select {
	case r.wake <- struct{}{}:
	default:
		t.Fatal("wake channel already holds an unread nudge")
	}
}

// runWhileFirstHeld runs the Automaton, applies the operator decision to the
// held task while the first task is still running, then lets the pass finish.
func (r *sequentialWakeRig) runWhileFirstHeld(t *testing.T, decision TaskStatus) error {
	t.Helper()
	done := make(chan error, 1)
	ctx := withReviewWake(context.Background(), r.wake)
	go func() { done <- r.m.Run(ctx, r.prdID, r.spawns.spawn, r.verify) }()
	select {
	case <-r.started:
	case <-time.After(10 * time.Second):
		t.Fatal("first task never started")
	}
	r.setHeldStatus(t, decision)
	r.nudge(t)
	close(r.release)
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// A retry made while a task runs is picked up by the same sequential pass, at
// the next task boundary, without a second Run.
func TestReviewWake_SequentialWalkRunsRetriedTask(t *testing.T) {
	r := newSequentialWakeRig(t)
	if err := r.runWhileFirstHeld(t, TaskPending); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := len(r.spawns.workers()); n != 2 {
		t.Fatalf("spawns = %d, want 2 (first task, then the retried task)", n)
	}
	prd, _ := r.m.Store().GetPRD(r.prdID)
	if got := lookupTask(prd, r.heldID).Status; got != TaskCompleted {
		t.Fatalf("retried task status = %q, want %q", got, TaskCompleted)
	}
}

// A cancel made while a task runs is honoured at the next task boundary: the
// cancelled task never starts.
func TestReviewWake_SequentialWalkSkipsCancelledTask(t *testing.T) {
	r := newSequentialWakeRig(t)
	if err := r.runWhileFirstHeld(t, TaskCancelled); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := len(r.spawns.workers()); n != 1 {
		t.Fatalf("spawns = %d, want 1 (only the first task)", n)
	}
	prd, _ := r.m.Store().GetPRD(r.prdID)
	if got := lookupTask(prd, r.heldID).Status; got != TaskCancelled {
		t.Fatalf("cancelled task status = %q, want %q", got, TaskCancelled)
	}
}
