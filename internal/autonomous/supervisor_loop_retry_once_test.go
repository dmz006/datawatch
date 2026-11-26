package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// holdForInfra puts the one task of the rig's first Automaton on the hold
// that follows used-up infrastructure retries, keeping what the record already
// holds about the task.
func holdForInfra(t *testing.T, h *coordHarness) {
	t.Helper()
	p, ok := h.m.Store().GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatal("prd gone")
	}
	h.m.Store().WithLock(func() {
		p.Status = PRDBlocked
		if len(p.Story) == 0 {
			p.Story = []Story{{ID: "s1", PRDID: p.ID, Title: "s", Tasks: []Task{{ID: "t1", StoryID: "s1", PRDID: p.ID, Title: "held", Spec: "do it"}}}}
		}
		tk := &p.Story[0].Tasks[0]
		tk.Status = TaskReviewNeeded
		tk.FailureReason = &FailureReason{Category: infraRetryFailureCategory, Source: infraRetryHoldSource}
	})
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
}

func retryPass(t *testing.T, steps *SupervisorLoopSteps) string {
	t.Helper()
	r := resultFor(t, steps.RunPass(context.Background()), stepRetryFailedInfra)
	if r.Err != nil {
		t.Fatalf("retry step: %v", r.Err)
	}
	return r.Detail
}

// The loop lifts an infrastructure hold once. A pass that runs while the task
// is being retried, or a daemon restart, does not give it another go: a task
// held again stays for the operator.
func TestSupervisorLoopRetriesAHeldTaskOnceAcrossPasses(t *testing.T) {
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{})
	holdForInfra(t, h)
	if got := retryPass(t, steps); got != "retried 1 of 1 held tasks" {
		t.Fatalf("first hold: %s", got)
	}
	// A pass while the retried task is at work.
	if got := retryPass(t, steps); got != "retried 0 of 0 held tasks" {
		t.Fatalf("pass during the retry: %s", got)
	}
	holdForInfra(t, h)
	if got := retryPass(t, steps); got != "retried 0 of 1 held tasks" {
		t.Errorf("held again after a pass in between: %s", got)
	}
	// The same record read by a daemon that has just started.
	fresh := NewSupervisorLoopSteps(h.c, steps.engine, SupervisorLoopHooks{})
	if got := retryPass(t, fresh); got != "retried 0 of 1 held tasks" {
		t.Errorf("held again after a daemon restart: %s", got)
	}
	// It is on disk, not only in this process.
	reread, err := NewStore(strings.TrimSuffix(h.m.Store().dir, "/autonomous"))
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := reread.GetPRD(h.prds[0].ID); !ok || !p.Story[0].Tasks[0].LoopRetriedInfraHold {
		t.Error("the loop's retry of the task was not saved")
	}
	// The operator's own retry starts over: a later hold is a new one.
	if _, err := h.m.ResolveTaskReview(h.prds[0].ID, "t1", ReviewResolution{Action: reviewActionRetry}, operatorActor); err != nil {
		t.Fatal(err)
	}
	retryPass(t, fresh)
	holdForInfra(t, h)
	if got := retryPass(t, fresh); got != "retried 1 of 1 held tasks" {
		t.Errorf("hold after the operator's retry: %s", got)
	}
}

// The loop's retry takes the same path as the operator's, so the requeued task
// is run: lifting the hold and starting nothing would only hide it.
func TestSupervisorLoopRetryStartsTheRun(t *testing.T) {
	var api *API
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{
		ResolveTaskReview: func(prdID, taskID string, req ReviewResolution, actor string) error {
			_, err := api.ResolveTaskReview(prdID, taskID, req, actor)
			return err
		},
	})
	api = NewAPI(h.m)
	spawned := make(chan string, 8)
	api.SetExecutors(func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned <- req.TaskID
		return SpawnResult{SessionID: "made-up-session"}, nil
	}, func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "made up", VerifiedAt: time.Now()}, nil
	})
	t.Cleanup(func() {
		if err := api.WaitIdle(30 * time.Second); err != nil {
			t.Error(err)
		}
	})
	// Let the boot-resume scan finish first, so only the retry can start a run.
	if err := api.WaitIdle(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	p, _ := h.m.Store().GetPRD(h.prds[0].ID)
	h.m.Store().WithLock(func() { p.ProjectDir = t.TempDir() })
	holdForInfra(t, h)
	if got := retryPass(t, steps); !strings.HasPrefix(got, "retried 1 of 1") {
		t.Fatalf("pass: %s", got)
	}
	select {
	case id := <-spawned:
		if id != "t1" {
			t.Errorf("spawned %q, want t1", id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the hold was lifted and nothing ran the task")
	}
}
