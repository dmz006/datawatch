package autonomous

// Planning requeue (shared helper, planning_requeue.go):
//
//   a. a set_llm that changes the decomposition profile/model while a
//      planning run is QUEUED retargets the queue entry in place to the pool
//      the CURRENT profile resolves to — one live request before, one live
//      request after, on the latest pool;
//   b. the reply carries the structured outcome (requeued |
//      deferred_to_next_run | unchanged) with old and new pools;
//   c. the surfaced wait reason always names the pool the run is CURRENTLY
//      waiting for;
//   d. a decompose re-requested while already planning either re-queues the
//      queued run (restart) or — when the run already started and holds a
//      lease — returns the explanation instead of double-spawning;
//   e. changing the profile twice leaves exactly one queued request on the
//      latest pool and no leaked capacity.
//
// Started (lease-holding) planning runs are left entirely alone: the change
// is stored and applies from the next decompose.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// twoPoolPlanningFixture wires the manager to two always-busy llm pools, one
// per decomposition profile, so a planner admission never clears and a
// retarget is directly observable on the ledger. The profile names ("pa",
// "pb") double as the planner backends for the fixture.
func twoPoolPlanningFixture(t *testing.T) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 5 * time.Millisecond})
	// One slot per pool, each busy-held by another consumer, so a planner
	// admission always ends up queued and retargets are observable.
	led.SetLimit("llm:pa", 1)
	led.SetLimit("llm:pb", 1)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy:pa", PRDID: "busy", Pools: []string{"llm:pa"}}, 0, nil, nil); err != nil {
		t.Fatalf("busy:pa: %v", err)
	}
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy:pb", PRDID: "busy", Pools: []string{"llm:pb"}}, 0, nil, nil); err != nil {
		t.Fatalf("busy:pb: %v", err)
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		switch backend {
		case "pb":
			return []string{"host", "llm:pb"}, ""
		default:
			return []string{"host", "llm:pa"}, ""
		}
	})
	return m, led
}

func waiterPools(t *testing.T, led *capacity.Ledger, prdID string) []string {
	t.Helper()
	w := plannerWaiter(led, prdID)
	if w == nil {
		t.Fatal("planner has no queued entry")
	}
	return w.Pools
}

func countPlannerEntries(t *testing.T, led *capacity.Ledger, prdID string) int {
	t.Helper()
	n := 0
	snap := led.Snapshot()
	for i := range snap.Waiting {
		if snap.Waiting[i].Holder == plannerHolder(prdID) {
			n++
		}
	}
	return n
}

func plPoolJoin(pools []string) string { return strings.Join(pools, ",") }

// seedPlanningProfile sets the PRD's decomposition pair (and stores it) the
// way a set_llm call would.
func seedPlanningProfile(t *testing.T, m *Manager, prdID, profile, model string) {
	t.Helper()
	sp := func(s string) *string { return &s }
	if _, _, err := m.SetPRDLLM(prdID, nil, nil, nil, sp(profile), sp(model), "seed"); err != nil {
		t.Fatal(err)
	}
}

// a — set_llm with a new decomposition_profile while the planning run is
// queued retargets it to the new pool; c — the wait reason then names the
// new pool; b — the structured result carries both pool sets.
func TestPlanningRequeue_QueuedRun_RetargetsToNewPool(t *testing.T) {
	m, led := twoPoolPlanningFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	seedPlanningProfile(t, m, prd.ID, "pa", "pm")

	go func() { _, _ = m.Decompose(prd.ID) }()
	pollUntil(t, 3*time.Second, "planner queued on the old pool", func() bool {
		return plannerWaiter(led, prd.ID) != nil
	})

	sp := func(s string) *string { return &s }
	updated, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, sp("pb"), nil, "test")
	if err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}
	if updated.DecompositionProfile != "pb" {
		t.Fatalf("DecompositionProfile: got %q, want pb", updated.DecompositionProfile)
	}
	if res == nil {
		t.Fatal("planning requeue result missing")
	}
	if res.Outcome != PlanningRequeued {
		t.Fatalf("outcome=%q want requeued (reason=%q)", res.Outcome, res.Reason)
	}
	if plPoolJoin(res.OldPools) != "llm:pa" {
		t.Errorf("old pools=%v; want llm:pa", res.OldPools)
	}
	if plPoolJoin(res.NewPools) != "llm:pb" {
		t.Errorf("new pools=%v; want llm:pb", res.NewPools)
	}
	// The queue entry itself moved, and exactly one entry remains.
	if got := plPoolJoin(waiterPools(t, led, prd.ID)); got != "llm:pb" {
		t.Errorf("waiter pools=%s; want llm:pb", got)
	}
	if n := countPlannerEntries(t, led, prd.ID); n != 1 {
		t.Errorf("planner queue entries=%d; want 1", n)
	}
	// c — the wait reason names the pool the run waits for now.
	if reason := prdWaitReason(t, m, prd.ID); !strings.Contains(reason, "llm:pb") {
		t.Errorf("wait reason=%q; must name the current pool llm:pb", reason)
	}
}

// e — changing the profile twice leaves exactly one queued request on the
// latest pool; the retarget moves the same queue entry in place.
func TestPlanningRequeue_TwoChanges_SingleQueueEntry(t *testing.T) {
	m, led := twoPoolPlanningFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	seedPlanningProfile(t, m, prd.ID, "pa", "pm")

	go func() { _, _ = m.Decompose(prd.ID) }()
	pollUntil(t, 3*time.Second, "planner queued", func() bool { return plannerWaiter(led, prd.ID) != nil })

	sp := func(s string) *string { return &s }
	update := func(profile, model *string) *PlanningRequeueResult {
		t.Helper()
		_, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, profile, model, "test")
		if err != nil {
			t.Fatalf("SetPRDLLM: %v", err)
		}
		return res
	}
	if res := update(sp("pb"), nil); res.Outcome != PlanningRequeued {
		t.Fatalf("first change: res=%v", res)
	}
	if res := update(sp("pa"), nil); res.Outcome != PlanningRequeued {
		t.Fatalf("second change: res=%v", res)
	}
	if got := plPoolJoin(waiterPools(t, led, prd.ID)); got != "llm:pa" {
		t.Errorf("waiter pools=%s; want llm:pa (the latest profile's pool)", got)
	}
	if n := countPlannerEntries(t, led, prd.ID); n != 1 {
		t.Errorf("planner queue entries=%d; want exactly 1 (no leaked capacity)", n)
	}
	if reason := prdWaitReason(t, m, prd.ID); !strings.Contains(reason, "llm:pa") {
		t.Errorf("wait reason=%q; must name the current pool llm:pa", reason)
	}
}

// Same-pool change is an unchanged no-op; and with no live run there is
// nothing to move.
func TestPlanningRequeue_NoMoveIsUnchanged(t *testing.T) {
	m, led := twoPoolPlanningFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	sp := func(s string) *string { return &s }

	if errRes, res := func() (error, *PlanningRequeueResult) {
		_, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, sp("pa"), sp("pm"), "test")
		return err, res
	}(); errRes != nil {
		t.Fatalf("SetPRDLLM: %v", errRes)
	} else if res == nil || res.Outcome != PlanningUnchanged {
		t.Fatalf("res=%v; want unchanged (no planning live)", res)
	}
	if updated, _ := m.store.GetPRD(prd.ID); updated.DecompositionProfile != "pa" {
		t.Fatalf("stored profile=%q; want pa", updated.DecompositionProfile)
	}

	// Queued on llm:pa, then re-set to the SAME pool: nothing to move.
	go func() { _, _ = m.Decompose(prd.ID) }()
	pollUntil(t, 3*time.Second, "planner queued", func() bool { return plannerWaiter(led, prd.ID) != nil })
	if errRes, res := func() (error, *PlanningRequeueResult) {
		_, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, nil, sp("pm"), "test")
		return err, res
	}(); errRes != nil {
		t.Fatalf("SetPRDLLM: %v", errRes)
	} else if res == nil || res.Outcome != PlanningUnchanged {
		t.Fatalf("res=%v; want unchanged (entry already on the pool)", res)
	}
	if n := countPlannerEntries(t, led, prd.ID); n != 1 {
		t.Errorf("planner queue entries=%d; want 1", n)
	}
}

// Started (lease-holding) planning runs are left alone: the change is stored
// and applies from the next decompose, with the explanation the requirement
// asks for (d — RestartPlanning returns it too instead of double-spawning).
func TestPlanningRequeue_StartedRunDeferred(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 5 * time.Millisecond})
	led.SetLimit("llm:pa", 1)
	led.SetLimit("llm:pb", 0)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		switch backend {
		case "pb":
			return []string{"host", "llm:pb"}, ""
		default:
			return []string{"host", "llm:pa"}, ""
		}
	})
	prd, _ := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	seedPlanningProfile(t, m, prd.ID, "pa", "pm")

	// Hold the only llm:pa slot, then let the planner admit: the wrapped
	// decompose holds the lease while the test inspects the state.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "o", Pools: []string{"llm:pa"}}, 0, nil, nil); err != nil {
		t.Fatalf("holding llm:pa: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	orig := m.decompose
	m.decompose = func(req DecomposeRequest) (string, error) {
		close(started)
		<-release
		return orig(req)
	}
	go func() { _, _ = m.Decompose(prd.ID) }()
	led.Release("other")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("planner never acquired its lease")
	}

	if !led.Held(plannerHolder(prd.ID)) {
		t.Fatal("planner must hold a lease here")
	}

	sp := func(s string) *string { return &s }
	updated, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, sp("pb"), nil, "test")
	if err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}
	// The value IS stored (it applies to the next run)…
	if updated.DecompositionProfile != "pb" {
		t.Errorf("DecompositionProfile=%q; want pb (change applies from the next run)", updated.DecompositionProfile)
	}
	// …but the running decompose is untouched and the outcome explains it.
	if res == nil || res.Outcome != PlanningDeferred {
		t.Fatalf("res=%v; want deferred_to_next_run", res)
	}
	if !led.Held(plannerHolder(prd.ID)) {
		t.Error("started planning run must keep its lease")
	}
	if n := countPlannerEntries(t, led, prd.ID); n != 0 {
		t.Errorf("queued entries=%d; want 0 (no double request)", n)
	}

	// d — a decompose re-request while the run started returns the
	// explanation too.
	if r := m.RestartPlanning(prd.ID); r == nil || r.Outcome != PlanningDeferred || !strings.Contains(r.Reason, "wait for it to finish") {
		t.Fatalf("RestartPlanning res=%v; want a deferred explanation", r)
	}
	// Let the (wrapped) planner finish so the goroutine ends.
	close(release)
}

// d — a decompose re-request while the run is QUEUED reports the entry's
// state (already on the current pool) instead of double-spawning, and after
// a profile change the restart sees the run on the NEW pool.
func TestPlanningRequeue_RestartWhileQueued(t *testing.T) {
	m, led := twoPoolPlanningFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	seedPlanningProfile(t, m, prd.ID, "pa", "pm")

	go func() { _, _ = m.Decompose(prd.ID) }()
	pollUntil(t, 3*time.Second, "planner queued", func() bool { return plannerWaiter(led, prd.ID) != nil })

	// No profile change yet: the run already sits on the current pool.
	res := m.RestartPlanning(prd.ID)
	if res == nil {
		t.Fatal("RestartPlanning returned nil for a queued run")
	}
	if res.Outcome == PlanningDeferred {
		t.Fatalf("queued run must not be reported as started: %s", res.Reason)
	}
	if n := countPlannerEntries(t, led, prd.ID); n != 1 {
		t.Errorf("planner queue entries=%d; want 1 (no double spawn)", n)
	}

	// Change the profile, then restart: the run must remain the single
	// queued entry on the new pool.
	sp := func(s string) *string { return &s }
	if _, _, err := m.SetPRDLLM(prd.ID, nil, nil, nil, sp("pb"), nil, "test"); err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}
	if got := plPoolJoin(waiterPools(t, led, prd.ID)); got != "llm:pb" {
		t.Fatalf("waiter pools=%s; want llm:pb", got)
	}
	if r2 := m.RestartPlanning(prd.ID); r2 == nil {
		t.Fatal("RestartPlanning returned nil")
	}
	if n := countPlannerEntries(t, led, prd.ID); n != 1 {
		t.Errorf("after restart: planner queue entries=%d; want 1 (no double spawn)", n)
	}
}

func TestSplitSetLLMOut_PlanningRoundsTrip(t *testing.T) {
	out := SetLLMOut{
		PRD:      &PRD{ID: "x1", Status: PRDPlanning},
		Planning: &PlanningRequeueResult{Outcome: PlanningRequeued, NewPools: []string{"llm:pb"}},
	}
	p, _, planning := SplitSetLLMOut(out)
	if p == nil || planning == nil || planning.Outcome != PlanningRequeued {
		t.Fatalf("SplitSetLLMOut lost the planning result: p=%v planning=%v", p, planning)
	}

	// A test double's bare non-SetLLMOut reply passes through unchanged.
	bare, _, planningBare := SplitSetLLMOut(map[string]any{"id": "y"})
	if bare.(map[string]any)["id"] != "y" {
		t.Fatalf("bare reply not passed through: %v", bare)
	}
	if planningBare != nil {
		t.Fatalf("bare reply carried a planning result (must stay nil): %v", planningBare)
	}
}
