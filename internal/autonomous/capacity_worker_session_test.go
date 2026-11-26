package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

// A SpawnFn only starts the worker session and returns; the VerifyFn is the
// step that waits for that session to end (the daemon's wait loop). So the
// worker's lease must outlive spawn and the wait, and be gone once the session
// has ended. Channels decide when each session ends, so the order is fixed.
func TestCapacity_WorkerLeaseHeldWhileItsSessionRuns(t *testing.T) {
	m, api, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 2)

	spawned := make(chan string, 2) // task ids, in spawn order
	waiting := make(chan string, 2) // verify entered, session still running
	endSession := map[string]chan struct{}{}
	finishVerify := make(chan struct{})
	var mu sync.Mutex
	sessionEnd := func(taskID string) chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		if endSession[taskID] == nil {
			endSession[taskID] = make(chan struct{})
		}
		return endSession[taskID]
	}
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		sessionEnd(r.TaskID)
		spawned <- r.TaskID
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(ctx context.Context, _ *PRD, tk *Task) (VerificationResult, error) {
		waiting <- tk.ID
		select {
		case <-sessionEnd(tk.ID):
		case <-ctx.Done():
			return VerificationResult{}, ctx.Err()
		}
		// The daemon's session-end hook: free the lease bound to this session.
		for _, ls := range led.Leases() {
			if ls.SessionID == "s-"+tk.ID {
				led.ReleaseIf(ls.Holder, ls.Acquired)
			}
		}
		select {
		case <-finishVerify:
		case <-ctx.Done():
			return VerificationResult{}, ctx.Err()
		}
		return VerificationResult{OK: true}, nil
	}

	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	api.SetExecutors(spawn, verify)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	// Whatever fails below, let both tasks finish so the executor can exit.
	var once sync.Once
	finish := func() {
		once.Do(func() {
			mu.Lock()
			for _, ch := range endSession {
				select {
				case <-ch:
				default:
					close(ch)
				}
			}
			mu.Unlock()
			close(finishVerify)
		})
	}
	defer finish()

	recv := func(ch chan string, what string) string {
		t.Helper()
		select {
		case id := <-ch:
			return id
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %s", what)
			return ""
		}
	}
	first := recv(spawned, "the first spawn")
	recv(waiting, "the wait on the first session")
	if ls := led.LeaseOf(first); ls == nil || ls.SessionID != "s-"+first {
		t.Fatalf("worker session of %s is still running but its lease is gone or not bound to it: %+v", first, ls)
	}
	select {
	case id := <-spawned:
		t.Fatalf("node limit 1: task %s started while the session of %s was still running", id, first)
	default:
	}

	// The first session ends; its verification is still running. The lane is
	// free for the second worker.
	close(sessionEnd(first))
	second := recv(spawned, "the second spawn once the first session ended")
	if second == first {
		t.Fatalf("task %s spawned twice", first)
	}
	finish()
	if got := runWait(t, m, prd.ID); got.Status != PRDCompleted {
		t.Fatalf("want completed, got %s", got.Status)
	}
}
