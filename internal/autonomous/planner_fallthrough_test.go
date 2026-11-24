package autonomous

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// plannerFallFixture is fallthroughFixture (llm:a full, llm:b free, a -> b)
// with a decompose function that records the pair it was asked to run on.
func plannerFallFixture(t *testing.T, enabled bool) (*Manager, *PRD, func() (string, string, int)) {
	t.Helper()
	m, _, prd, _, _ := fallthroughFixture(t, enabled)
	var mu sync.Mutex
	var backend, model string
	var calls int
	m.decompose = func(r DecomposeRequest) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		backend, model, calls = r.Backend, r.Model, calls+1
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	return m, prd, func() (string, string, int) {
		mu.Lock()
		defer mu.Unlock()
		return backend, model, calls
	}
}

// admitPlanner runs decomposeAdmitted for the pair a/m1 in the background.
func admitPlanner(m *Manager, prd *PRD) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := m.decomposeAdmitted(prd, "a", "m1", DecomposeRequest{Spec: "s", Backend: "a", Model: "m1"})
		done <- err
	}()
	return done
}

func TestPlannerFallthrough_PlannerMovesToTheAllowedPool(t *testing.T) {
	m, prd, ran := plannerFallFixture(t, true)
	done := admitPlanner(m, prd)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("decomposeAdmitted: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the planner stayed queued on the full pool; it must fall through to the allowed pool")
	}
	backend, model, calls := ran()
	if calls != 1 || backend != "b" || model != "m2" {
		t.Fatalf("planner ran %d time(s) on %s/%s, want once on b/m2", calls, backend, model)
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	found := false
	for _, d := range cur.Decisions {
		if d.Kind == "planner_fallthrough" && strings.Contains(d.Note, "a/m1") && strings.Contains(d.Note, "b/m2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no planner_fallthrough decision naming both pools: %+v", cur.Decisions)
	}
}

func TestPlannerFallthrough_OffKeepsThePlannerWaiting(t *testing.T) {
	m, prd, ran := plannerFallFixture(t, false)
	done := admitPlanner(m, prd)
	m.mu.Lock()
	led := m.capacity
	m.mu.Unlock()
	pollUntil(t, 2*time.Second, "planner queued on the full pool", func() bool { return plannerQueued(led, prd.ID) })
	select {
	case err := <-done:
		t.Fatalf("planner returned %v with its pool full and fall-through off", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, _, calls := ran(); calls != 0 {
		t.Fatalf("planner ran %d time(s) with its pool full and fall-through off", calls)
	}
	led.Release("other") // let the waiting planner finish
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if backend, _, _ := ran(); backend != "a" {
		t.Fatalf("planner ran on %s after the pool freed, want a", backend)
	}
}

func TestPlannerFallthrough_FreePoolKeepsTheRequestedPair(t *testing.T) {
	m, prd, ran := plannerFallFixture(t, true)
	m.mu.Lock()
	led := m.capacity
	m.mu.Unlock()
	led.Release("other") // the requested pool has room: no fall-through
	if err := <-admitPlanner(m, prd); err != nil {
		t.Fatal(err)
	}
	if backend, model, _ := ran(); backend != "a" || model != "m1" {
		t.Fatalf("planner ran on %s/%s, want a/m1", backend, model)
	}
}
