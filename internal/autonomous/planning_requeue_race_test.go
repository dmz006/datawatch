package autonomous

import (
	"sync"
	"testing"
)

// TestPlannerPairForConcurrentWithSetPRDLLM: a queued planner re-resolves its
// pair at grant time (planningRequestAtGrant → plannerPairFor) on the
// decompose goroutine while the operator changes the decomposition profile
// with SetPRDLLM. Both touch the live PRD record the store holds; the read
// must go through the store lock, or -race reports a data race on
// DecompositionProfile / DecompositionModel. The resolved pair must always be
// one of the pairs actually written, never a torn mix of two.
func TestPlannerPairForConcurrentWithSetPRDLLM(t *testing.T) {
	m, _ := pairPlanFixture(t)
	prd, err := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	seedPlanningProfile(t, m, prd.ID, "pa", "m1")
	live, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("prd missing from the store")
	}

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(1)
	torn := make(chan string, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			backend, model := m.plannerPairFor(live)
			if !(backend == "pa" && model == "m1") && !(backend == "pb" && model == "m2") {
				select {
				case torn <- backend + "/" + model:
				default:
				}
			}
		}
	}()
	pairs := [][2]string{{"pb", "m2"}, {"pa", "m1"}}
	for i := 0; i < rounds; i++ {
		p := pairs[i%2]
		profile, model := p[0], p[1]
		if _, _, err := m.SetPRDLLM(prd.ID, nil, nil, nil, &profile, &model, "test"); err != nil {
			t.Fatalf("SetPRDLLM %s/%s: %v", profile, model, err)
		}
	}
	wg.Wait()
	select {
	case got := <-torn:
		t.Errorf("plannerPairFor resolved a pair that was never written: %s", got)
	default:
	}
}

// TestNotePlanningRequeueConcurrentWithWaitReason: a planning requeue records
// its decision (notePlanningRequeue, on the API goroutine) while the
// decompose goroutine updates the PRD's wait reason (setPRDWaitReason /
// clearPRDWaitReason → Store.MutatePRD, which encodes the record to disk
// under the store lock). The decision append must go through the store lock
// too, or -race reports a data race on PRD.Decisions between the append and
// the encode. Every decision must land: none lost to a concurrent save.
func TestNotePlanningRequeueConcurrentWithWaitReason(t *testing.T) {
	m, _ := pairPlanFixture(t)
	prd, err := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	before := 0
	if snap, ok := m.store.SnapshotPRD(prd.ID); ok {
		before = len(snap.Decisions)
	}

	const rounds = 50
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			m.setPRDWaitReason(prd.ID, "waiting for llm:pa")
			m.clearPRDWaitReason(prd.ID)
		}
	}()
	for i := 0; i < rounds; i++ {
		m.notePlanningRequeue(prd.ID, "test", &PlanningRequeueResult{
			Outcome: PlanningRequeued, OldBackend: "pa", NewBackend: "pb",
		})
	}
	wg.Wait()

	snap, ok := m.store.SnapshotPRD(prd.ID)
	if !ok {
		t.Fatal("prd missing from the store")
	}
	got := 0
	for _, d := range snap.Decisions {
		if d.Kind == "planning_requeue" {
			got++
		}
	}
	if want := len(trimDecisions(make([]Decision, before+rounds))) - before; got < want {
		t.Fatalf("planning_requeue decisions recorded = %d, want %d", got, want)
	}
}
