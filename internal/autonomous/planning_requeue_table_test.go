// Table-driven coverage of the queued-planning requeue requirements, built
// on the existing capacity-layer fakes (fakeCapClock: a clock whose After
// channels never fire, so a queued request is admitted only when a lease is
// released — never because real time passed) and a decompose gate that holds
// a started run in place. No case sleeps for the system to catch up: every
// observation polls the ledger snapshot.
//
// The pool map here is model-sensitive ("pb" backend and "m2" model both
// resolve to llm:pb) on purpose, so BOTH a profile change and a model-only
// change can be observed moving the queue entry to a new pool — which is
// what "model-only change" requires to be testable at all.

package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// pairPlanFixture is twoPoolPlanningFixture's model-sensitive twin:
//
//	(profile backend, decomposition model)
//	"pa"/"m1"        -> llm:pa
//	"pa"/"m2"        -> llm:pb   (model-only change moves the pool)
//	"pb"/anything    -> llm:pb   (profile change moves the pool)
//
// Both pools are limit 1 and each is held by a busy consumer, so a planning
// run is always queued, and admitting it means releasing exactly the busy
// holder of the pool it is waiting on.
func pairPlanFixture(t *testing.T) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit("llm:pa", 1)
	led.SetLimit("llm:pb", 1)
	for _, p := range []string{"llm:pa", "llm:pb"} {
		if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy:" + p, PRDID: "busy", Pools: []string{p}}, 0, nil, nil); err != nil {
			t.Fatalf("holding %s: %v", p, err)
		}
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		switch {
		case backend == "pb" || model == "m2":
			return []string{"host", "llm:pb"}, ""
		default:
			return []string{"host", "llm:pa"}, ""
		}
	})
	return m, led
}

// runGate holds a started decompose so a test can observe "the run is
// running" (it holds its ledger lease and its planningRequestAtGrant pair),
// change the profile, and only then let the run finish. The capture of the
// requests that actually entered decomprove proves which pair each run used.
type runGate struct {
	mu          sync.Mutex
	releaseOnce sync.Once
	release     chan struct{}
	reqs        []DecomposeRequest // decompose requests that entered, in order
}

func installRunGate(t *testing.T, m *Manager) *runGate {
	t.Helper()
	g := &runGate{release: make(chan struct{})}
	orig := m.decompose
	m.decompose = func(req DecomposeRequest) (string, error) {
		g.mu.Lock()
		g.reqs = append(g.reqs, req)
		g.mu.Unlock()
		<-g.release // hold the run while it still owns its ledger lease
		return orig(req)
	}
	t.Cleanup(g.open)
	return g
}

func (g *runGate) open() {
	g.releaseOnce.Do(func() { close(g.release) })
}

func (g *runGate) reqsSeen() []DecomposeRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]DecomposeRequest(nil), g.reqs...)
}

type requeueStep struct {
	kind             string // "change" | "start" | "restart" | "finishAndRerun"
	profile          string // "change": new decomposition_profile ("" = leave as is)
	model            string // "change": new decomposition_model ("m2" moves the pool)
	want             PlanningOutcome
	wantPool         string // planner queue entry pools (or next run's pools) this step must leave, "" = don't check
	wantReason       string // substring the PRD wait reason must contain after the step, "" = don't check
	wantResultReason string // substring the result's own explanation must contain, "" = don't check
}

type requeueRow struct {
	name      string
	global    bool // planning pair inherited from the global planning backend (no per-PRD profile)
	gated     bool // a row that starts the run (holds it with runGate)
	steps     []requeueStep
	finalPool string
}

// TestPlanningRequeue_Table drives the seven queued-planning requeue
// requirements as data: each row lists the changes (or gate / restart
// events) and the outcome + pool each one must produce.
func TestPlanningRequeue_Table(t *testing.T) {
	rows := []requeueRow{
		// 1) Queued run + profile change: the OLD request is cancelled and the
		// pool's capacity is released (busy consumers untouched, exactly one
		// live request remains), the NEW request sits on the new pool, the
		// result says requeued.
		{
			name: "queued_profile_change_requeues_to_new_pool",
			steps: []requeueStep{
				{kind: "change", profile: "pb", want: PlanningRequeued, wantPool: "llm:pb", wantReason: "llm:pb"},
			},
			finalPool: "llm:pb",
		},
		// 2) Queued run + model-only change: the same move — the old request
		// on llm:pa is cancelled, one live request on llm:pb, requeued.
		{
			name: "queued_model_only_change_requeues_to_new_pool",
			steps: []requeueStep{
				{kind: "change", model: "m2", want: PlanningRequeued, wantPool: "llm:pb", wantReason: "llm:pb"},
			},
			finalPool: "llm:pb",
		},
		// 3) Started run + profile change: the run is untouched (it keeps its
		// lease and executes the OLD pair), the result defers to the next
		// run, and the next run is queued on the NEW pool.
		{
			name:  "started_profile_change_defers_and_next_run_uses_new_pool",
			gated: true,
			steps: []requeueStep{
				{kind: "start"},
				{kind: "change", profile: "pb", want: PlanningDeferred, wantResultReason: "left alone"},
				{kind: "finishAndRerun", wantPool: "llm:pb"},
			},
		},
		// 4) Changed twice while queued: each move cancels the previous live
		// request — exactly one remains, on the latest profile's pool, and no
		// capacity is leaked anywhere (busy consumers keep their slots,
		// nothing of the planner's).
		{
			name: "queued_changed_twice_leaves_one_live_request_no_leak",
			steps: []requeueStep{
				{kind: "change", profile: "pb", want: PlanningRequeued, wantPool: "llm:pb"},
				{kind: "change", profile: "pa", want: PlanningRequeued, wantPool: "llm:pa"},
			},
			finalPool: "llm:pa",
		},
		// 5) Changed twice with a start in between: the first change (run
		// still queued) requeues it; the run then starts on the new pair; the
		// second change (run now started) cannot requeue and defers, applying
		// to the next run instead.
		{
			name:  "changed_twice_with_start_in_between",
			gated: true,
			steps: []requeueStep{
				{kind: "change", profile: "pb", want: PlanningRequeued, wantPool: "llm:pb"},
				{kind: "start"},
				{kind: "change", profile: "pa", want: PlanningDeferred, wantResultReason: "left alone"},
				{kind: "finishAndRerun", wantPool: "llm:pa"},
			},
		},
		// 6) Wait reason always names the pool of the CURRENT profile: each
		// change requeues onto a different pool and the reason must follow.
		{
			name: "wait_reason_names_the_current_profile_pool_each_time",
			steps: []requeueStep{
				{kind: "change", profile: "pb", want: PlanningRequeued, wantPool: "llm:pb", wantReason: "llm:pb"},
				{kind: "change", profile: "pa", want: PlanningRequeued, wantPool: "llm:pa", wantReason: "llm:pa"},
				{kind: "change", model: "m2", want: PlanningRequeued, wantPool: "llm:pb", wantReason: "llm:pb"},
			},
			finalPool: "llm:pb",
		},
		// 7a) restart-decompose on a QUEUED planning run re-queues it onto the
		// pool the CURRENT pair resolves to — here the pair is inherited from
		// the global planning backend, which the operator changed, so the
		// restart itself performs the requeue (one live request, old pool's
		// entry gone, nothing leaked).
		{
			name:   "restart_decompose_while_queued_requeues_to_current_pool",
			global: true,
			steps: []requeueStep{
				{kind: "setGlobal", profile: "pb"},
				{kind: "restart", want: PlanningRequeued, wantPool: "llm:pb", wantReason: "llm:pb"},
			},
			finalPool: "llm:pb",
		},
		// 7b) restart-decompose on a STARTED planning run must not wait on it:
		// the run keeps its lease and pair, and the answer is the plain
		// explanation that the operator should wait for it to finish.
		{
			name:  "restart_decompose_while_started_explains_plainly",
			gated: true,
			steps: []requeueStep{
				{kind: "start"},
				{kind: "restart", want: PlanningDeferred, wantResultReason: "wait for it to finish"},
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			runRequeueRow(t, row)
		})
	}
}

func runRequeueRow(t *testing.T, row requeueRow) {
	t.Helper()
	m, led := pairPlanFixture(t)
	var gate *runGate
	if row.gated {
		gate = installRunGate(t, m)
	}

	seedProfile, seedModel := "pa", "m1"
	if row.global {
		m.mu.Lock()
		m.cfg.PlanningBackend = "pa"
		m.cfg.PlanningModel = ""
		m.mu.Unlock()
		seedProfile, seedModel = "", ""
	}
	prd, err := m.CreatePRD("s", "/w/proj", "pa", "pm", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if !row.global {
		seedPlanningProfile(t, m, prd.ID, seedProfile, seedModel)
	}

	done := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		_, _ = m.Decompose(prd.ID)
		close(firstDone)
		close(done)
	}()
	pollUntil(t, 3*time.Second, "planner queued on the starting pool", func() bool { return plannerWaiter(led, prd.ID) != nil })
	if got := plPoolJoin(waiterPools(t, led, prd.ID)); got != "llm:pa" {
		t.Fatalf("starting pools are %s; want llm:pa", got)
	}

	for _, step := range row.steps {
		switch step.kind {
		case "change":
			profile, model := (*string)(nil), (*string)(nil)
			if step.profile != "" {
				profile = &step.profile
			}
			if step.model != "" {
				model = &step.model
			}
			_, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, profile, model, "test")
			if err != nil {
				t.Fatalf("SetPRDLLM changed %q/%q: %v", step.profile, step.model, err)
			}
			checkRequeueResult(t, m, led, prd.ID, res, step)

		case "setGlobal":
			m.mu.Lock()
			m.cfg.PlanningBackend = step.profile
			m.mu.Unlock()

		case "restart":
			res := m.RestartPlanning(prd.ID)
			checkRequeueResult(t, m, led, prd.ID, res, step)

		case "start":
			// The planner is waiting on its pool: free that pool and the
			// queued run is admitted and holds its lease, blocked at the gate.
			for _, p := range waiterPools(t, led, prd.ID) {
				// Free the pool the run is waiting on; dispatch admits it and it
				// holds its lease at the gate from there on.
				led.Release("busy:" + p)
			}
			pollUntil(t, 3*time.Second, "planner run started (its lease is held)", func() bool { return led.Held(plannerHolder(prd.ID)) })

		case "finishAndRerun":
			if gate == nil {
				t.Fatal("finishAndRerun on an ungated row")
			}
			gate.open()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the gated run still holds the gate after open(); it never finished")
			}
			// The run that just finished must have executed the pair in force
			// when it was admitted — "untouched" by later changes.
			got := gate.reqsSeen()
			if len(got) != 1 {
				t.Fatalf("%d decompose requests entered; want 1", len(got))
			}
			done = make(chan struct{})
			go func() {
				_, _ = m.Decompose(prd.ID)
				close(done)
			}()
			pollUntil(t, 3*time.Second, "next run queued", func() bool { return plannerWaiter(led, prd.ID) != nil })
			if step.wantPool != "" {
				if gotPools := plPoolJoin(waiterPools(t, led, prd.ID)); gotPools != step.wantPool {
					t.Fatalf("next run queued on %s; want %s — the deferred change must apply from this run", gotPools, step.wantPool)
				}
			}
		default:
			t.Fatalf("unknown step kind %q", step.kind)
		}
	}

	if row.finalPool != "" {
		if got := plPoolJoin(waiterPools(t, led, prd.ID)); got != row.finalPool {
			t.Fatalf("final planner pools are %s; want %s", got, row.finalPool)
		}
	}

	// Deterministic teardown: if the row left a gated run sitting at the gate
	// let it finish BEFORE the test's temp dirs are removed — the run writes
	// to the manager's store on its own goroutine, and unlinking the store
	// dir under it fails the test's cleanup.
	if gate != nil {
		select {
		case <-firstDone:
		default:
			gate.open()
			select {
			case <-firstDone:
			case <-time.After(5 * time.Second):
				t.Fatal("gated run did not finish after the teardown open")
			}
		}
	}
}

// checkRequeueResult asserts a requeue outcome against the ledger snapshot:
// the right state machine result, and whichever of the four requeue
// guarantees the step cares about (live request count, pool, capacity,
// explanation).
func checkRequeueResult(t *testing.T, m *Manager, led *capacity.Ledger, prdID string, res *PlanningRequeueResult, step requeueStep) {
	t.Helper()
	if res == nil {
		t.Fatalf("%s step: no planning result reported; want %s", step.kind, step.want)
	}
	if res.Outcome != step.want {
		t.Fatalf("%s step: result %q (%s); want %q", step.kind, res.Outcome, res.Reason, step.want)
	}
	if step.wantResultReason != "" && !strings.Contains(res.Reason, step.wantResultReason) {
		t.Fatalf("%s step: result explanation %q does not contain %q", step.kind, res.Reason, step.wantResultReason)
	}
	holder := plannerHolder(prdID)
	switch res.Outcome {
	case PlanningRequeued:
		if step.wantPool != "" {
			if got := plPoolJoin(waiterPools(t, led, prdID)); got != step.wantPool {
				t.Fatalf("%s step: planner now queued on %s; want %s", step.kind, got, step.wantPool)
			}
		}
		// One live request on the new pool — the old request was cancelled,
		// not copied — and no leaked capacity.
		if n := countPlannerEntries(t, led, prdID); n != 1 {
			t.Fatalf("%s step: %d planner queue entries; want exactly 1 (old request cancelled, one new live request)", step.kind, n)
		}
		if heldBy(led, holder) {
			t.Fatalf("%s step: the queued planner holds a live lease — leaked capacity", step.kind)
		}
		if !heldBy(led, "busy:llm:pa") || !heldBy(led, "busy:llm:pb") {
			t.Fatalf("%s step: the pool-holding consumers were disturbed — capacity was leaked", step.kind)
		}
	case PlanningDeferred:
		// The started run is untouched: still there, still holding its lease,
		// and nothing was duplicated into the queue.
		if !led.Held(holder) {
			t.Fatalf("%s step: the started run lost its lease; the run must be left alone", step.kind)
		}
		if n := countPlannerEntries(t, led, prdID); n != 0 {
			t.Fatalf("%s step: %d planner queue entries; want 0 — a started run is deferred, not re-queued", step.kind, n)
		}
	default:
		t.Fatalf("%s step: unexpected outcome %q", step.kind, res.Outcome)
	}
	if step.wantReason != "" {
		// The operator-visible explanation names the pool of the CURRENT pair.
		reason := prdWaitReason(t, m, prdID)
		if !strings.Contains(reason, step.wantReason) {
			t.Fatalf("%s step: wait reason %q does not name the current pool %s", step.kind, reason, step.wantReason)
		}
	}
}
