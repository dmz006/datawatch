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

// waitingOccupant is a lane occupant waiting on a verifier slot.
func waitingOccupant(id string) *LaneOccupant {
	return &LaneOccupant{
		PRDID: id, Title: "waiting " + id, Backend: "claude-code",
		WaitingOnVerifier: true, WaitReason: "waiting for a capacity slot to finish verification",
	}
}

// TestVerifierWaitWorkingClaudeLaneStaysOccupied: a Claude lane with one
// working task (an occupant with no verifier wait) stays occupied and
// receives no start — the required proof that a lane with one working task is
// still counted occupied.
func TestVerifierWaitWorkingClaudeLaneStaysOccupied(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes = 0
	cfg.ClaudeLanes = 1
	lanes := testLanes(0, 1)
	lanes[0].Occupant = &LaneOccupant{PRDID: "w1", Title: "w1", Backend: "claude-code"}
	ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	if got := len(actions(ds, LaneFillActionStartPRD)); got != 0 {
		t.Fatalf("a Claude lane with one working occupant got %d start(s), want 0: %v", got, summarize(ds))
	}
	if lanes[0].Occupant == nil || lanes[0].Occupant.PRDID != "w1" {
		t.Fatalf("working lane no longer occupied: %+v", lanes[0].Occupant)
	}
}

// TestVerifierWaitWaitingClaudeLaneCountsFree: a Claude lane whose only
// occupant waits on a verifier slot counts free, and a ready Automaton is
// planned onto it.
func TestVerifierWaitWaitingClaudeLaneCountsFree(t *testing.T) {
	cfg := laneFillTestConfig() // count_verifier_wait unset means on
	cfg.GPULanes = 0
	cfg.ClaudeLanes = 1
	lanes := testLanes(0, 1)
	lanes[0].Occupant = waitingOccupant("w1")
	ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	d := byLane(t, ds, "claude-1")
	if d.Action != LaneFillActionStartPRD || d.PRDID != "r1" {
		t.Fatalf("waiting Claude lane planned %+v, want start of r1: %v", d, summarize(ds))
	}
}

// TestVerifierWaitingClaudeCapBoundsWorking: with one Claude lane, one working
// and one waiting occupant plan no start — and the waiting occupant, listed
// first, is the one that ends up in Extra, never the working one; with one
// waiting and none working exactly one start is planned.
func TestVerifierWaitClaudeCapBoundsWorking(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes = 0
	cfg.ClaudeLanes = 1
	running := []LaneOccupant{*waitingOccupant("wa"), {PRDID: "wk", Title: "wk", Backend: "claude-code"}}
	model := BuildLanes(cfg, running, LaneEvidence{})
	if model.Lanes[0].Occupant == nil || model.Lanes[0].Occupant.PRDID != "wk" {
		t.Fatalf("working occupant not placed first: lane %+v, extra %v", model.Lanes[0], model.Extra)
	}
	if len(model.Extra) != 1 || model.Extra[0].PRDID != "wa" {
		t.Fatalf("waiting occupant pushed the working one out: extra = %v", model.Extra)
	}
	ds := plan(cfg, model.Lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	if got := len(actions(ds, LaneFillActionStartPRD)); got != 0 {
		t.Fatalf("one working and one waiting occupant got %d start(s), want 0: %v", got, summarize(ds))
	}

	lanes := testLanes(0, 1)
	lanes[0].Occupant = waitingOccupant("w1")
	ds = plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	if got := len(actions(ds, LaneFillActionStartPRD)); got != 1 {
		t.Fatalf("one waiting and none working got %d start(s), want 1: %v", got, summarize(ds))
	}
}

// TestVerifierWaitClaudeCapAcrossPolls: across two simulated polls the working
// Claude occupants never exceed claude_lanes — poll one plans the start on the
// waiting lane, and once it lands as a working occupant poll two plans nothing
// behind the still-waiting occupant.
func TestVerifierWaitClaudeCapAcrossPolls(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes = 0
	cfg.ClaudeLanes = 1
	working := 0
	for i, wantStarts := range []int{1, 0} {
		poll := i + 1
		var running []LaneOccupant
		if working > 0 {
			running = append(running, LaneOccupant{PRDID: "wk", Title: "wk", Backend: "claude-code"})
		}
		running = append(running, *waitingOccupant("wa"))
		model := BuildLanes(cfg, running, LaneEvidence{})
		ds := plan(cfg, model.Lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
		got := len(actions(ds, LaneFillActionStartPRD))
		if got != wantStarts {
			t.Fatalf("poll %d (working=%d) planned %d start(s), want %d: %v",
				poll, working, got, wantStarts, summarize(ds))
		}
		if working+got > cfg.EffectiveClaudeLanes() {
			t.Fatalf("poll %d: working Claude occupants would be %d, cap is %d",
				poll, working+got, cfg.EffectiveClaudeLanes())
		}
		working += got // the poll's starts land as working occupants
	}
}

// TestVerifierWaitOffLeavesClaudeLaneBusy: with count_verifier_wait false the
// waiting occupant's lane is no longer free, so the ready Automaton plans
// nothing there.
func TestVerifierWaitOffLeavesClaudeLaneBusy(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes = 0
	cfg.ClaudeLanes = 1
	off := false
	cfg.CountVerifierWait = &off
	lanes := testLanes(0, 1)
	lanes[0].Occupant = waitingOccupant("w1")
	ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	if got := len(actions(ds, LaneFillActionStartPRD)); got != 0 {
		t.Fatalf("count_verifier_wait off still planned %d start(s) on the waiting lane, want 0: %v", got, summarize(ds))
	}
}

// TestVerifierWaitGPULaneStaysOccupied: the rule is about Claude usage, and
// GPU work costs no Claude usage — a GPU lane whose occupant waits on a
// verifier slot stays occupied.
func TestVerifierWaitGPULaneStaysOccupied(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes = 1
	cfg.ClaudeLanes = 0
	lanes := testLanes(1, 0)
	lanes[0].Occupant = &LaneOccupant{PRDID: "g1", Title: "g1", Backend: "pi-prompt",
		WaitingOnVerifier: true, WaitReason: "waiting for a capacity slot to finish verification"}
	ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	if got := len(actions(ds, LaneFillActionStartPRD)); got != 0 {
		t.Fatalf("a GPU lane with a waiting occupant got %d start(s), want 0: %v", got, summarize(ds))
	}
}
