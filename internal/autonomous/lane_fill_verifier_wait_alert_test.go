// The idle-lane alert for a lane that is free only because its occupant waits
// on a verifier slot: the same alert and the same once-per-episode rule as
// the plain idle alert, but the message names the verifier wait and its
// reason. A lane idle for any other reason keeps the original message.
package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

const verifierWaitAlertSlot = "node:solo"

// acquireSlot grants holder the lone verifier slot in the fixture ledger.
func acquireSlot(t *testing.T, led *capacity.Ledger, holder string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := led.Acquire(ctx, capacity.Request{Holder: holder, Pools: []string{verifierWaitAlertSlot}}, 2*time.Second, nil, nil); err != nil {
		t.Fatalf("acquire slot as %q: %v", holder, err)
	}
}

// queueSlotWaiter queues holder behind the fixture slot until it is in the
// wait queue, and keeps it queued for the life of the test (releasing the
// slot unblocks it: the waiter is then granted).
func queueSlotWaiter(t *testing.T, led *capacity.Ledger, holder, prdID string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: []string{verifierWaitAlertSlot}}, 0, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, w := range led.Snapshot().Waiting {
			if w.Holder == holder {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiter %q never queued", holder)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// slotHeldBy reports whether holder currently holds the fixture slot.
func slotHeldBy(led *capacity.Ledger, holder string) bool {
	for _, l := range led.Snapshot().Leases {
		if l.Holder == holder {
			return true
		}
	}
	return false
}

// verifierWaitIdleFixture builds a shadow harness in which the claude lane's
// occupant is blocked on the verifier slot — so that lane is free only
// because the occupant waits — and work from another Automaton is ready for
// the lane. The fixture returns the harness, the alert sink, the waiting
// Automaton and the blocked task.
//
// The slot itself is held by "outer-holder", standing in for the work that
// has it; the occupant's verification ("verify:<taskID>") is the waiter.
func verifierWaitIdleFixture(t *testing.T) (*laneFillHarness, *laneFillAlertSink, *PRD, string) {
	t.Helper()
	h := newLaneFillHarness(t, "shadow")
	sink := &laneFillAlertSink{}
	h.c.alertFn = sink.add
	h.c.SetReady()

	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit(verifierWaitAlertSlot, 1)
	h.m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:pool", verifierWaitAlertSlot}, "solo"
	})
	h.m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		return []capacity.NodeChoice{{Name: "solo", Priority: 50}}
	})

	waiting := h.addPRD("waiting occupant", PRDRunning, "claude", "claude-code")
	st, err := h.m.store.AddStory(waiting.ID, "story", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.m.store.AddTask(waiting.ID, st.ID, "task", "spec", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.m.store.MutateTask(task.ID, func(tk *Task) { tk.Status = TaskVerifying })

	acquireSlot(t, led, "outer-holder")
	queueSlotWaiter(t, led, VerifyHolder(task.ID), waiting.ID)

	h.addPRD("ready work", PRDApproved, "claude", "claude-code")
	return h, sink, waiting, task.ID
}

func TestVerifierWaitNoAlertBeforeThreshold(t *testing.T) {
	h, sink, _, _ := verifierWaitIdleFixture(t)
	h.poll()
	h.advancePoll(30*time.Minute - time.Millisecond)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("alerts before the threshold = %d, want 0: %+v", len(got), got)
	}
}

func TestVerifierWaitAlertsOnceAtThreshold(t *testing.T) {
	h, sink, _, _ := verifierWaitIdleFixture(t)
	h.poll()
	h.advancePoll(29 * time.Minute)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("alerts before the threshold = %d, want 0: %+v", len(got), got)
	}
	h.advancePoll(time.Minute)
	got := sink.all()
	if len(got) != 1 {
		t.Fatalf("alerts at the threshold = %d, want 1: %+v", len(got), got)
	}
	if got[0].level != "warn" {
		t.Errorf("level = %q, want warn", got[0].level)
	}
}

func TestVerifierWaitNoRepeatInSameEpisode(t *testing.T) {
	h, sink, _, _ := verifierWaitIdleFixture(t)
	h.poll()
	for i := 0; i < 6; i++ {
		h.advancePoll(10 * time.Minute)
	}
	got := sink.all()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want exactly one per episode: %+v", len(got), got)
	}
}

func TestVerifierWaitNewEpisodeAfterWorkingOccupant(t *testing.T) {
	h, sink, waiting, taskID := verifierWaitIdleFixture(t)
	led := h.m.Capacity()
	waiter := VerifyHolder(taskID)

	h.poll()
	h.advancePoll(30 * time.Minute)
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("first episode alerts = %d, want 1: %+v", len(got), got)
	}

	// The occupant starts advancing: the verifier slot frees up, its
	// verification takes it, the lane is occupied and working — the idle
	// episode ends.
	led.Release("outer-holder")
	waitSlotCondition(t, "grants the waiter", func() bool { return slotHeldBy(led, waiter) })
	for i := 0; i < 5; i++ {
		h.advancePoll(10 * time.Minute)
	}
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("alerts while the occupant works = %d, want 1: %+v", len(got), got)
	}

	// The occupant is blocked on the verifier again: its round ends and gives
	// the slot back, another verifier takes it, and the next round is queued
	// behind it. That is a new episode.
	led.Release(waiter)
	acquireSlot(t, led, "outer-holder")
	queueSlotWaiter(t, led, waiter, waiting.ID)

	h.advancePoll(time.Minute) // the new episode starts on this poll
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("alerts right after the new episode starts = %d, want 1: %+v", len(got), got)
	}
	h.advancePoll(29 * time.Minute)
	if got := sink.all(); len(got) != 1 {
		t.Fatalf("alerts before the new episode's threshold = %d, want 1: %+v", len(got), got)
	}
	h.advancePoll(time.Minute)
	got := sink.all()
	if len(got) != 2 {
		t.Fatalf("alerts = %d, want 2 (one per episode): %+v", len(got), got)
	}
}

func TestVerifierWaitNamesVerifierWaitAndReason(t *testing.T) {
	h, sink, waiting, _ := verifierWaitIdleFixture(t)
	h.poll()
	h.advancePoll(30 * time.Minute)
	body := alertBody(t, sink, 1)
	for _, want := range []string{
		"claude-1",            // the lane
		"waiting occupant",    // the Automaton whose verifier is waiting
		waiting.ID,            // its id
		"waits on a verifier", // the verifier wait, named
		"node solo",           // the reason, from the ledger wait queue
		"ready work",          // the work that could not be started on the lane
	} {
		if !strings.Contains(body, want) {
			t.Errorf("alert body %q does not have %q", body, want)
		}
	}
	for _, not := range []string{"**"} {
		if strings.Contains(body, not) {
			t.Errorf("alert body %q contains formatting marks %q — the message must be plain text", body, not)
		}
	}
}

func TestVerifierWaitOtherIdleKeepsOldMessage(t *testing.T) {
	h, sink, _ := newLaneFillAlertHarness(t)
	h.poll()
	h.advancePoll(30 * time.Minute)
	body := alertBody(t, sink, 1)
	for _, want := range []string{"has been idle for", "claude-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("alert body %q does not have %q", body, want)
		}
	}
	for _, not := range []string{"verifier", "waiting occupant"} {
		if strings.Contains(body, not) {
			t.Errorf("alert body %q mentions a verifier wait, but the lane is idle for another reason", body)
		}
	}
}

func alertBody(t *testing.T, sink *laneFillAlertSink, wantN int) string {
	t.Helper()
	got := sink.all()
	if len(got) != wantN {
		t.Fatalf("alerts = %d, want %d: %+v", len(got), wantN, got)
	}
	return got[0].body
}

// waitSlotCondition polls until f holds, or the test fails.
func waitSlotCondition(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the slot never %s", what)
}
