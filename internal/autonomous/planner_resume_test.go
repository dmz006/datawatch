package autonomous

import (
	"sync"
	"testing"
	"time"
)

func TestResolvePlannerResume(t *testing.T) {
	if a, _ := ResolvePlannerResume(true); a != PlannerResumeReattach {
		t.Fatalf("alive: action = %s, want %s", a, PlannerResumeReattach)
	}
	if a, _ := ResolvePlannerResume(false); a != PlannerResumeRestart {
		t.Fatalf("gone: action = %s, want %s", a, PlannerResumeRestart)
	}
}

// plannerRestartRig builds a throwaway data directory holding several
// Automata in planning with different priorities, then "restarts" by opening a
// second Manager on the same directory with fake planner-session and
// decomposer seams. Decomposes block until release is closed, so a started
// decompose keeps its Automaton in planning while the test asserts.
type plannerRestartRig struct {
	ids       []string
	api       *API
	m         *Manager
	mu        sync.Mutex
	decomps   map[string]int
	reattach  []string
	release   chan struct{}
	liveByPRD map[string][]string
}

func newPlannerRestartRig(t *testing.T, sessionAlive bool) *plannerRestartRig {
	t.Helper()
	dir := t.TempDir()
	m1, err := NewManager(dir, DefaultConfig(), func(DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r := &plannerRestartRig{decomps: map[string]int{}, release: make(chan struct{}), liveByPRD: map[string][]string{}}
	for i, prio := range []int{1, 5, 10} {
		prd, err := m1.CreatePRD("spec", "/w/proj", "opencode", "", EffortNormal)
		if err != nil {
			t.Fatal(err)
		}
		if err := m1.SetPRDPriority(prd.ID, prio); err != nil {
			t.Fatal(err)
		}
		p, _ := m1.Store().GetPRD(prd.ID)
		p.Status = PRDPlanning
		p.PlannerSessionID = "planner-" + string(rune('a'+i))
		p.PlannerOutputFile = "" // nothing usable on disk
		if err := m1.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
		r.ids = append(r.ids, prd.ID)
		if sessionAlive {
			r.liveByPRD[prd.ID] = []string{p.PlannerSessionID}
		}
	}
	m1.Stop()

	// Restart: a fresh Manager over the same directory.
	m2, err := NewManager(dir, DefaultConfig(), func(req DecomposeRequest) (string, error) {
		r.mu.Lock()
		r.decomps[req.PRDID]++
		r.mu.Unlock()
		<-r.release
		return "", nil
	})
	if err != nil {
		t.Fatalf("restart NewManager: %v", err)
	}
	m2.SetDecomposeSessionsFn(func(id string) []string { return r.liveByPRD[id] })
	m2.SetSessionKillerFn(func(string, string, string) error { return nil })
	m2.SetPlanningReattachFn(func(prdID, _, _ string) {
		r.mu.Lock()
		r.reattach = append(r.reattach, prdID)
		r.mu.Unlock()
	})
	r.m = m2
	r.api = NewAPI(m2)
	t.Cleanup(func() {
		select {
		case <-r.release:
		default:
			close(r.release)
		}
		if err := r.api.WaitIdle(10 * time.Second); err != nil {
			t.Errorf("WaitIdle: %v", err)
		}
		m2.Stop()
	})
	return r
}

func (r *plannerRestartRig) assertAllPlanningNoFailure(t *testing.T) {
	t.Helper()
	for _, id := range r.ids {
		p, ok := r.m.Store().GetPRD(id)
		if !ok {
			t.Fatalf("automaton %s missing after restart", id)
		}
		if st := statusOf(r.m, p); st != PRDPlanning {
			t.Errorf("automaton %s status = %s, want %s", id, st, PRDPlanning)
		}
		r.m.mu.Lock()
		fails, last := p.DecomposeSessionFailures, p.LastDecomposeFailure
		r.m.mu.Unlock()
		if fails != 0 || last != nil {
			t.Errorf("automaton %s counted a failure: failures=%d last=%+v", id, fails, last)
		}
	}
}

func TestPlannerResumeAtBootKeepsPlanning(t *testing.T) {
	r := newPlannerRestartRig(t, true)
	r.api.resumeRunningPRDs()

	r.assertAllPlanningNoFailure(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reattach) != len(r.ids) {
		t.Fatalf("re-attached %v, want one per automaton (%d)", r.reattach, len(r.ids))
	}
	for _, id := range r.ids {
		if r.decomps[id] != 0 {
			t.Errorf("automaton %s: decompose started %d times with a live planner, want 0", id, r.decomps[id])
		}
	}
}

func TestPlannerResumeAtBootPlannerGone(t *testing.T) {
	r := newPlannerRestartRig(t, false)
	r.api.resumeRunningPRDs()

	for _, id := range r.ids {
		id := id
		pollUntil(t, 5*time.Second, "decompose started for "+id, func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.decomps[id] >= 1
		})
	}
	r.assertAllPlanningNoFailure(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.ids {
		if r.decomps[id] != 1 {
			t.Errorf("automaton %s: decompose started %d times, want exactly 1", id, r.decomps[id])
		}
	}
	if len(r.reattach) != 0 {
		t.Errorf("unexpected re-attach %v for a gone planner", r.reattach)
	}
}
