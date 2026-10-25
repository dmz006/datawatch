package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

const (
	liveBlockerTitle = "Blocker"
	liveHeldTitle    = "Held"
)

// liveReviewRig is an Automaton of two independent tasks. The blocker runs
// first and holds its verifier until unblock is called, so a pass is still
// live when the operator acts. The held task starts in TaskReviewNeeded, so a
// pass begins without it, the way a run started after the hold would.
type liveReviewRig struct {
	m       *Manager
	api     *API
	prd     *PRD
	heldID  string
	started chan struct{}
	release chan struct{}
	once    sync.Once
	spawns  escalationSpawnLog
}

func newLiveReviewRig(t *testing.T, concurrency int) *liveReviewRig {
	t.Helper()
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	// Each task gets its own directory: tasks sharing a tree queue on its
	// lock, which would hold the held task back for a reason other than the
	// wake under test.
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: []Task{
		{Title: liveBlockerTitle, Spec: "do the blocker", ProjectDir: t.TempDir()},
		{Title: liveHeldTitle, Spec: "do the held task", ProjectDir: t.TempDir()},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	held := &prd.Story[0].Tasks[1]
	held.Status = TaskReviewNeeded
	held.ReviewSummary = &ReviewSummary{Text: "held for the live review test"}
	prd.MaxConcurrentTasks = concurrency
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	// SaveTask indexes the task, as a held task is indexed when it is held.
	if err := m.Store().SaveTask(held); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	r := &liveReviewRig{
		m: m, api: api, prd: prd, heldID: held.ID,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	t.Cleanup(r.unblock)
	return r
}

// verify passes every task. The blocker reports that it has started, then
// holds its verifier until unblock.
func (r *liveReviewRig) verify(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
	if task.Title == liveBlockerTitle {
		r.started <- struct{}{}
		<-r.release
	}
	return VerificationResult{OK: true, Summary: "ok"}, nil
}

func (r *liveReviewRig) unblock() {
	r.once.Do(func() { close(r.release) })
}

func (r *liveReviewRig) waitBlockerStarted(t *testing.T) {
	t.Helper()
	select {
	case <-r.started:
	case <-time.After(10 * time.Second):
		t.Fatal("blocker task never started")
	}
}

// requeueHeld is the operator's retry of the held task, made directly on the
// Manager so a test controls when the decision lands.
func (r *liveReviewRig) requeueHeld(t *testing.T) {
	t.Helper()
	if _, err := r.m.ResolveTaskReview(r.prd.ID, r.heldID, ReviewResolution{Action: reviewActionRetry}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview retry: %v", err)
	}
}

// waitHeldCompleted polls until the held task completes. It fails at the
// deadline, which is how a late pickup shows.
func (r *liveReviewRig) waitHeldCompleted(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		task, _ := r.m.Store().TaskSnapshot(r.heldID)
		if task.Status == TaskCompleted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("requeued task status = %q, want %q within %s", normTaskStatus(task.Status), TaskCompleted, timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *liveReviewRig) wantPRDCompleted(t *testing.T) {
	t.Helper()
	got, ok := r.m.Store().GetPRD(r.prd.ID)
	if !ok {
		t.Fatalf("PRD %s not found", r.prd.ID)
	}
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %q, want %q", got.Status, PRDCompleted)
	}
}

func waitRunDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// A decision that lands during a sequential pass is run by that same pass once
// the task in progress finishes. Run is called once, so nothing relaunches it.
func TestReviewWake_SequentialPassRunsRequeuedTask(t *testing.T) {
	r := newLiveReviewRig(t, 1)
	var wakes reviewWakes
	ctx := withReviewWake(context.Background(), wakes.arm(r.prd.ID))
	done := make(chan error, 1)
	go func() { done <- r.m.Run(ctx, r.prd.ID, r.spawns.spawn, r.verify) }()
	r.waitBlockerStarted(t)

	r.requeueHeld(t)
	wakes.signal(r.prd.ID)
	r.unblock()

	if err := waitRunDone(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.waitHeldCompleted(t, 10*time.Second)
	r.wantPRDCompleted(t)
}

// With another task in flight, a decision starts the requeued task at once.
// The pass does not wait for the in-flight task to finish first.
func TestReviewWake_ConcurrentPassStartsRequeuedTaskWhileAnotherRuns(t *testing.T) {
	r := newLiveReviewRig(t, 2)
	var wakes reviewWakes
	ctx := withReviewWake(context.Background(), wakes.arm(r.prd.ID))
	done := make(chan error, 1)
	go func() { done <- r.m.Run(ctx, r.prd.ID, r.spawns.spawn, r.verify) }()
	r.waitBlockerStarted(t)

	r.requeueHeld(t)
	wakes.signal(r.prd.ID)
	r.waitHeldCompleted(t, 10*time.Second)

	r.unblock()
	if err := waitRunDone(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.wantPRDCompleted(t)
}

// The production path: the executor the API started takes the decision, and
// ResolveTaskReview neither waits for the slot nor starts a second executor.
func TestResolveTaskReview_LiveExecutorRunsRequeuedTask(t *testing.T) {
	r := newLiveReviewRig(t, 2)
	r.api.SetExecutors(r.spawns.spawn, r.verify)
	if err := r.api.Run(r.prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.waitBlockerStarted(t)

	if _, err := r.api.ResolveTaskReview(r.prd.ID, r.heldID, ReviewResolution{Action: reviewActionRetry}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview with a live executor: %v", err)
	}
	r.waitHeldCompleted(t, 10*time.Second)

	r.unblock()
	waitExecutorGone(t, r.api, r.prd.ID, 10*time.Second)
	r.wantPRDCompleted(t)
}
