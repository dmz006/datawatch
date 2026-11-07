package autonomous

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// TestResetTaskWaitingCapacityReadmit: a task stuck in the capacity queue can
// be reset without force — the reset clears the recorded admitted worker and
// keeps (or retargets) the task's live ledger waiter, so when capacity frees
// the re-admission grants and the task runs and completes. (The sibling test
// TestResetTaskActiveStillRefused covers the other side: active, non-terminal
// tasks are still refused.)
func TestResetTaskWaitingCapacityReadmit(t *testing.T) {
	m, api, led := capFixture(t, 1)
	held, cancelHeld := context.WithCancel(context.Background())
	defer cancelHeld()
	if err := led.Acquire(held, capacity.Request{Holder: "holder-x", PRDID: "other", Pools: []string{"node:n1"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	spawned := int32(0)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawned, 1)
		return SpawnResult{SessionID: "s" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	// SetExecutors kicks an async boot-resume scan; let it finish before the
	// PRD exists (same ordering as TestCapacity_WaitingTaskShowsStatusAndNeverFails).
	api.SetExecutors(spawn, verify)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	prd := newPRDWithTasks(t, m, 1)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	taskID := p.Story[0].Tasks[0].ID
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, 3*time.Second, "task waiting_capacity", func() bool {
		cur, _ := m.Store().SnapshotPRD(prd.ID)
		return cur.Story[0].Tasks[0].Status == TaskWaitingCapacity
	})
	if atomic.LoadInt32(&spawned) != 0 {
		t.Fatalf("task must not spawn while the pool is held")
	}

	// The reset of a waiting-capacity task must be accepted without force.
	out, err := m.ResetTaskWithOptions(prd.ID, taskID, "operator", false, false)
	if err != nil {
		t.Fatalf("reset of a waiting-capacity task must not be refused: %v", err)
	}
	tk := out.Story[0].Tasks[0]
	if tk.SessionID != "" || tk.BaseSHA != "" || len(tk.AttemptCommitSHAs) != 0 {
		t.Fatalf("reset must clear the recorded worker/anchors, got %+v", tk)
	}
	if tk.Status != "" && tk.Status != TaskPending {
		t.Fatalf("reset must leave the task unstarted (pending), got %q", tk.Status)
	}
	if tk.AdmittedBackend != "" || tk.AdmittedModel != "" || tk.AdmittedTaskBackend != "" || tk.AdmittedTaskModel != "" {
		t.Fatalf("reset must clear the admitted worker pair, got %+v", tk)
	}
	// The single live capacity request must survive the reset at its queue
	// position: exactly one waiter for the task, and it is still queued while
	// the pool is held.
	snap := led.Snapshot()
	n := 0
	for _, w := range snap.Waiting {
		if w.Holder == taskID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("reset must keep the task's single live capacity request (queue position), got %d waiters for it", n)
	}

	// Capacity frees: the re-admission grants and the task runs to completion.
	led.Release("holder-x")
	got := runWait(t, m, prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("want completed after capacity frees, got %s", got.Status)
	}
	if at := got.Story[0].Tasks[0]; at.Status != TaskCompleted {
		t.Fatalf("task must complete after re-admission, got %+v", at)
	}
	if atomic.LoadInt32(&spawned) != 1 {
		t.Fatalf("exactly one spawn expected after the reset's re-admission, got %d", int(atomic.LoadInt32(&spawned)))
	}
}

// TestResetTaskActiveStillRefused: the relaxed acceptance is
// waiting-capacity only — an active (verifying) task is still refused
// both without force and with it. (With this fixture's fake worker the
// active state after spawn is verifying; the gate is the same for any
// unstarted-again state that is not retryable and not force-requeueable.)
func TestResetTaskActiveStillRefused(t *testing.T) {
	m, api, _ := capFixture(t, 1)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		time.Sleep(500 * time.Millisecond) // keep the task in-progress long enough to reset
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, verify)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	prd := newPRDWithTasks(t, m, 1)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	taskID := p.Story[0].Tasks[0].ID
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, 3*time.Second, "task verifying (active)", func() bool {
		cur, _ := m.Store().SnapshotPRD(prd.ID)
		return cur.Story[0].Tasks[0].Status == TaskVerifying
	})
	if _, err := m.ResetTaskWithOptions(prd.ID, taskID, "operator", false, false); err == nil {
		t.Fatal("reset of an active (verifying) task without force must still be refused")
	}
	if _, err := m.ResetTaskWithOptions(prd.ID, taskID, "operator", false, true); err == nil {
		t.Fatal("reset of an active (verifying) task with force must still be refused")
	}
}
