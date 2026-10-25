package autonomous

import (
	"context"
	"testing"
	"time"
)

func TestReviewWakes_SignalAndTake(t *testing.T) {
	var w reviewWakes
	if w.signal("p") {
		t.Fatal("signal on an unarmed PRD reported a wake")
	}
	c := w.arm("p")
	if cap(c) != 1 {
		t.Fatalf("wake channel capacity = %d, want 1", cap(c))
	}
	if w.takePending("p") {
		t.Fatal("pending before any signal")
	}
	w.signal("p")
	w.signal("p") // a second signal must not block
	if len(c) != 1 {
		t.Fatalf("buffered nudges = %d, want 1", len(c))
	}
	if !w.takePending("p") {
		t.Fatal("takePending missed the signal")
	}
	if len(c) != 0 || w.takePending("p") {
		t.Fatal("takePending did not clear the flag and the nudge")
	}
	w.signal("p")
	w.disarm("p")
	if w.takePending("p") {
		t.Fatal("disarm kept the pending flag")
	}
}

func TestReviewWakes_ContextCarriesChannel(t *testing.T) {
	var w reviewWakes
	c := w.arm("p")
	ctx := withReviewWake(context.Background(), c)
	if reviewWakeFrom(ctx) == nil {
		t.Fatal("wake channel not found on the context")
	}
	if reviewWakeFrom(context.Background()) != nil {
		t.Fatal("bare context carried a wake channel")
	}
}

// With an executor slot live, a review decision wakes that executor and does
// not launch another one.
func TestResolveTaskReview_LiveExecutorIsSignalledNotRelaunched(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, spawns := heldTaskOf(t, m, api, prd)
	before := len(spawns.workers())

	if _, ok := api.claimRun(prd.ID); !ok {
		t.Fatal("could not claim the executor slot")
	}
	api.reviewWake.arm(prd.ID)
	defer api.releaseRun(prd.ID)

	if _, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionRetry}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview with a live executor: %v", err)
	}
	if !api.reviewWake.takePending(prd.ID) {
		t.Fatal("live executor was not signalled")
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(spawns.workers()); got != before {
		t.Fatalf("worker spawns went from %d to %d; a second executor ran", before, got)
	}
}
