package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// verifierWaitFixture wires a manager with a one-slot pool ("solo") and a PRD
// with n tasks. The tasks start in their creation status; tests set the
// statuses they need.
func verifierWaitFixture(t *testing.T, n int) (*Manager, *capacity.Ledger, *PRD) {
	t.Helper()
	m, _ := poolFixture(t, false)
	prd := newPRDWithTasks(t, m, n)
	return m, m.Capacity(), prd
}

// setTaskStatuses stores the PRD with each task in the given status, in order.
func setTaskStatuses(t *testing.T, m *Manager, prdID string, statuses ...TaskStatus) []string {
	t.Helper()
	p, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("GetPRD: not found")
	}
	var ids []string
	for si := range p.Story {
		s := &p.Story[si]
		for ti := range s.Tasks {
			tk := &s.Tasks[ti]
			if len(statuses) > 0 {
				tk.Status = statuses[0]
				statuses = statuses[1:]
			}
			ids = append(ids, tk.ID)
		}
	}
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return ids
}

// queueWaiter fills the pool slot (best-effort — a test that already fills
// the pool lets the occupant wait instead) and then enqueues an admission
// under holder. It returns once the waiter for holder is visible in the
// ledger's wait list; a held slot keeps the waiter queued for the rest of
// the test.
func queueWaiter(t *testing.T, led *capacity.Ledger, holder, prdID string) {
	t.Helper()
	occupant := "occupant-" + holder
	req := capacity.Request{Holder: occupant, PRDID: "holder", Pools: []string{"node:solo"}}
	_ = led.Acquire(context.Background(), req, 200*time.Millisecond, nil, nil)
	t.Cleanup(func() { led.Release(occupant) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel) // the waiter must stay queued for the whole test, not until this helper returns
	go func() {
		_ = led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: []string{"node:solo"}}, 0, nil, nil)
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

// TestVerifierWaitAllTasksWaitingIsFlagged: a running Automaton whose only
// running task is queued on the verifier holder id is occupied but not
// advancing, and the surface reason is the waiter's text.
func TestVerifierWaitAllTasksWaitingIsFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	ids := setTaskStatuses(t, m, prd.ID, TaskVerifying)
	queueWaiter(t, led, VerifyHolder(ids[0]), prd.ID)
	f, reason := m.prdVerifierWait(prd.ID)
	if !f {
		t.Fatalf("prdVerifierWait = false, want true")
	}
	if strings.TrimSpace(reason) == "" {
		t.Errorf("reason = empty, want the waiter's text reason")
	}
}

// TestVerifierWaitMixedRunningNotFlagged: the same Automaton plus one running
// (in-progress) task that is not waiting must not be flagged, even though
// the other task is queued on the verifier.
func TestVerifierWaitMixedRunningNotFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 2)
	ids := setTaskStatuses(t, m, prd.ID, TaskVerifying, TaskInProgress)
	queueWaiter(t, led, VerifyHolder(ids[0]), prd.ID)
	if f, _ := m.prdVerifierWait(prd.ID); f {
		t.Fatalf("prdVerifierWait = true with one task still progressing, want false")
	}
}

// TestVerifierWaitOnlyInProgressNotFlagged: a running task with no
// verifier-slot wait is working, so the lane is not occupied-but-waiting —
// neither with no waiter at all, nor with a wait queue for another task's
// verifier.
func TestVerifierWaitOnlyInProgressNotFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	_ = setTaskStatuses(t, m, prd.ID, TaskInProgress)
	if f, _ := m.prdVerifierWait(prd.ID); f {
		t.Fatalf("prdVerifierWait = true with an in-progress task and no wait, want false")
	}
	queueWaiter(t, led, VerifyHolder("foreign-task"), "foreign-prd")
	if f, _ := m.prdVerifierWait(prd.ID); f {
		t.Fatalf("prdVerifierWait = true from another task's verifier wait, want false")
	}
}

// TestVerifierWaitJudgeHolderNoTaskNotFlagged: a judge-held waiter on a unit
// that is not this Automaton's task (a story, another PRD's task) waits on
// the judge's own slot, not on this task's verification, so it does not count
// as the task waiting on a verifier slot.
func TestVerifierWaitJudgeHolderNoTaskNotFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	_ = setTaskStatuses(t, m, prd.ID, TaskVerifying)
	for _, holder := range []string{
		JudgeHolder(prd.ID + "/s"),    // judge unit is a story, not the task
		JudgeHolder("other-prd-task"), // judge unit is another PRD's task
		JudgeHolder("foreign-task"),   // judge unit has no task at all
	} {
		queueWaiter(t, led, holder, "other-prd")
		if f, reason := m.prdVerifierWait(prd.ID); f {
			t.Fatalf("prdVerifierWait = true with a judge waiter on %q (no task), want false; reason = %q", holder, reason)
		}
	}
}

// TestVerifierWaitJudgeOnTaskIsFlagged covers the judge holder id on this
// Automaton's own task: the guardrail judge is part of finishing the task's
// verification, so it queued for a slot is a verifier-slot wait.
func TestVerifierWaitJudgeOnTaskIsFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	ids := setTaskStatuses(t, m, prd.ID, TaskVerifying)
	queueWaiter(t, led, JudgeHolder(ids[0]), prd.ID)
	if f, _ := m.prdVerifierWait(prd.ID); !f {
		t.Fatalf("prdVerifierWait = false with the task's judge queued, want true")
	}
}

// TestVerifierWaitTaskIDWaiterIsFlagged covers the bare task-id holder: the
// task's own admission requeued while its verification's slot is still held.
func TestVerifierWaitTaskIDWaiterIsFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	ids := setTaskStatuses(t, m, prd.ID, TaskVerifying)
	queueWaiter(t, led, ids[0], prd.ID)
	if f, _ := m.prdVerifierWait(prd.ID); !f {
		t.Fatalf("prdVerifierWait = false with the task id queued, want true")
	}
}

// TestVerifierWaitAnnotationIsIdempotentOnTheOccupant is a property test
// over LaneOccupant: the annotation reads no shared state, so annotating the
// same Automaton repeatedly, in any order against other PRDs' occupants,
// never changes the flagged outcome or the reason it reports.
func TestVerifierWaitAnnotationIsIdempotentOnTheOccupant(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	ids := setTaskStatuses(t, m, prd.ID, TaskVerifying)
	queueWaiter(t, led, VerifyHolder(ids[0]), prd.ID)
	c := &Coordinator{m: m}
	first := c.laneFillVerifierWait(LaneOccupant{PRDID: prd.ID, Title: "T", Backend: "pi"})
	if !first.WaitingOnVerifier {
		t.Fatalf("first annotation not flagged")
	}
	for i := 0; i < 8; i++ {
		again := c.laneFillVerifierWait(LaneOccupant{PRDID: prd.ID, Title: "T", Backend: "pi"})
		if again.WaitingOnVerifier != first.WaitingOnVerifier || again.WaitReason != first.WaitReason {
			t.Fatalf("annotation %d = %+v, first = %+v", i+1, again, first)
		}
		other := c.laneFillVerifierWait(LaneOccupant{PRDID: "some-other-prd", Title: "O", Backend: "pi"})
		if other.WaitingOnVerifier || other.WaitReason != "" {
			t.Fatalf("annotation for another PRD = %+v, want clean", other)
		}
	}
}

// TestVerifierWaitUnflaggedOccupantCarriesNoWaitReason is a property test
// over the shape of an unflagged LaneOccupant: when the Automaton has a
// running task that is not blocked, neither field may be set on it.
func TestVerifierWaitUnflaggedOccupantCarriesNoWaitReason(t *testing.T) {
	m, _, prd := verifierWaitFixture(t, 2)
	_ = setTaskStatuses(t, m, prd.ID, TaskInProgress, TaskVerifying)
	c := &Coordinator{m: m}
	a := c.laneFillVerifierWait(LaneOccupant{PRDID: prd.ID, Title: "T", Backend: "pi"})
	if a.WaitingOnVerifier || a.WaitReason != "" {
		t.Fatalf("unflagged LaneOccupant = %+v, want neither field set", a)
	}
}

// TestVerifierWaitAnnotationOnSnapshotWiring is a property test over the
// snapshot wiring: annotating the same occupant agrees with
// prdVerifierWait — it sets WaitingOnVerifier exactly when prdVerifierWait
// reports true, and no reason otherwise — while the wait state changes
// underneath.
func TestVerifierWaitAnnotationOnSnapshotWiring(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 2)
	ids := setTaskStatuses(t, m, prd.ID, TaskVerifying, TaskVerifying)
	c := &Coordinator{m: m}
	occupant := LaneOccupant{PRDID: prd.ID, Title: "T", Backend: "pi"}

	check := func(why string) {
		f, reason := m.prdVerifierWait(prd.ID)
		a := c.laneFillVerifierWait(occupant)
		if a.WaitingOnVerifier != f {
			t.Fatalf("%s: LaneOccupant.flagged = %v, prdVerifierWait = %v", why, a.WaitingOnVerifier, f)
		}
		if f && a.WaitReason == "" {
			t.Fatalf("%s: flagged LaneOccupant with empty WaitReason; prdVerifierWait reason = %q", why, reason)
		}
		if !f && (a.WaitReason != "" || a.WaitingOnVerifier) {
			t.Fatalf("%s: unflagged LaneOccupant = %+v", why, a)
		}
	}

	check("no waiter yet")
	queueWaiter(t, led, VerifyHolder(ids[0]), prd.ID)
	check("one task waiting")
	queueWaiter(t, led, JudgeHolder(ids[1]), prd.ID)
	check("both tasks waiting (verify + judge)")
}

// TestVerifierWaitNoRunningTaskNotFlagged: an Automaton with no running task
// (all queued, all waiting) is not occupied-but-waiting, even if a verifier
// is queued for one of its tasks.
func TestVerifierWaitNoRunningTaskNotFlagged(t *testing.T) {
	m, led, prd := verifierWaitFixture(t, 1)
	ids := setTaskStatuses(t, m, prd.ID, TaskQueued)
	queueWaiter(t, led, VerifyHolder(ids[0]), prd.ID)
	if f, reason := m.prdVerifierWait(prd.ID); f {
		t.Fatalf("prdVerifierWait = true with no running task, want false; reason = %q", reason)
	}
}
