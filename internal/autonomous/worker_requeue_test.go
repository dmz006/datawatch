package autonomous

// Worker requeue (queued_task_override.go, section 10 of the discovery
// notes): while an Automaton is running, a queued-mode set_story_llm /
// set_task_llm retargets a waiting task's live capacity request to the new
// backend's pool (one live request before, one live request after, on the
// latest pool) and leaves every started or finished task alone with a
// reason. All of it is observed against the ledger with a fixed clock.
//
// Requirements (one test each):
//   1. Running Automaton, a pending task plus a waiting-for-capacity task,
//      backend change: BOTH change; the waiting one requeues on the new pool.
//   2. Running task plus finished task in the same story: BOTH left alone,
//      each with a reason; neither record nor capacity state changes.
//   3. Per-task override on a waiting task: the task changes and its queued
//      request requeues, and once the new pool frees the task runs on it.
//   4. override_queued_tasks explicitly false: the classic refusal returns,
//      nothing is written, nil result.
//   5. task_override_statuses still governs which Automaton statuses go
//      classic vs queued (default list AND a custom list).
//   6. Changed twice while queued: exactly ONE live request, on the LATEST
//      pool, and no capacity leaked onto an earlier pool.
//   7. Change, task starts (holds a lease), change again: the second change
//      leaves the started task alone — record untouched, capacity untouched.
//
// Fixed clock throughout: the ledger runs on `capacity.Clock` (fakeCapClock)
// whose After channels never fire, so admission happens only on
// Release/Retarget dispatch — never on a wall-clock tick. Observations poll
// the ledger snapshot (pollUntil); no test sleeps for the system to catch up.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// The fixture's backend → pool map.
const (
	workerPoolSolo  = "node:solo"
	workerPoolFastb = "node:fastb"
	workerPoolQuick = "node:quick"
)

// workerRequeueFixture wires a ledger on a fixed clock with one slot per
// pool (each slot held by a busy consumer) and a keys function mapping
// backend → pool. A worker admission therefore always ends up queued, which
// makes Retarget moves and their consequences fully observable.
func workerRequeueFixture(t *testing.T) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	for _, pool := range []string{workerPoolSolo, workerPoolFastb, workerPoolQuick} {
		led.SetLimit(pool, 1)
	}
	for h, p := range map[string]string{
		"busy:solo":  workerPoolSolo,
		"busy:fastb": workerPoolFastb,
		"busy:quick": workerPoolQuick,
	} {
		if err := led.Acquire(context.Background(), capacity.Request{Holder: h, PRDID: "busy", Pools: []string{p}}, 0, nil, nil); err != nil {
			t.Fatalf("occupy %s: %v", p, err)
		}
	}
	m.SetCapacity(led, func(backend, _ string) ([]string, string) {
		switch backend {
		case "ollama":
			return []string{"host", "llm:pool", workerPoolFastb}, "fastb"
		case "openai":
			return []string{"host", "llm:pool", workerPoolQuick}, "quick"
		default:
			return []string{"host", "llm:pool", workerPoolSolo}, "solo"
		}
	})
	return m, led
}

// runPRDFrozen pins the PRD to running and every task (in order) to the
// given statuses, returning the task ids.
func runPRDFrozen(t *testing.T, m *Manager, prd *PRD, statuses ...TaskStatus) []string {
	t.Helper()
	ids := setTaskStatuses(t, m, prd.ID, statuses...)
	cur, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD: not found")
	}
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return ids
}

// startWorkerWaiter puts one queued capacity request on the ledger for the
// given holder (the task id, exactly as the executor does) and waits for it
// to be visible in the snapshot. The acquire context lives for the whole
// test; that request is admitted only by a Release or a Retarget dispatch —
// the fake clock never ticks.
func startWorkerWaiter(t *testing.T, led *capacity.Ledger, holder, prdID, pool string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	go func() {
		_ = led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: prdID, Pools: []string{pool}}, 0, nil, nil)
	}()
	waitersFor(t, led, holder, "waiter queued")
}

// waitersFor lists the pools of every queued request under holder.
func waitersFor(t *testing.T, led *capacity.Ledger, holder, what string) []string {
	t.Helper()
	var pools []string
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			pools = append(pools, w.Pools...)
		}
	}
	if len(pools) == 0 {
		pollUntil(t, time.Second, what, func() bool {
			for _, w := range led.Snapshot().Waiting {
				if w.Holder == holder {
					return true
				}
			}
			return false
		})
		pools = nil
		for _, w := range led.Snapshot().Waiting {
			if w.Holder == holder {
				pools = append(pools, w.Pools...)
			}
		}
	}
	return pools
}

func waitForLease(t *testing.T, led *capacity.Ledger, holder, what string) *capacity.Lease {
	t.Helper()
	lease := led.LeaseOf(holder)
	if lease == nil {
		pollUntil(t, time.Second, what, func() bool {
			l := led.LeaseOf(holder)
			return l != nil
		})
		lease = led.LeaseOf(holder)
		if lease == nil {
			t.Fatalf("no lease for %q despite %s", holder, what)
		}
	}
	return lease
}

// outcomeOf finds one task's outcome on a side (changed / left_alone) of a
// queued-mode result.
func outcomeOf(res *LLMOverrideResult, side, taskID string) (LLMOverrideOutcome, bool) {
	if res == nil {
		return LLMOverrideOutcome{}, false
	}
	var pool []LLMOverrideOutcome
	if side == "changed" {
		pool = res.Changed
	} else {
		pool = res.LeftAlone
	}
	for _, o := range pool {
		if o.TaskID == taskID {
			return o, true
		}
	}
	return LLMOverrideOutcome{}, false
}

// --- 1. Running Automaton, two tasks: both change; the waiting one requeues

func TestWorkerRequeue_RunningStory_ChangesAllAndRequeuesWaiting(t *testing.T) {
	m, led := workerRequeueFixture(t)
	prd := newPRDWithTasks(t, m, 2)
	ids := runPRDFrozen(t, m, prd, TaskPending, TaskWaitingCapacity)
	pend, wait := ids[0], ids[1]
	storyID := prd.Story[0].ID
	// The waiting task already has its live request queued on the current
	// pool; the pending task does not admit at all.
	startWorkerWaiter(t, led, wait, prd.ID, workerPoolSolo)

	out, res, err := m.SetStoryLLM(prd.ID, storyID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("SetStoryLLM: %v", err)
	}
	if res == nil {
		t.Fatalf("queued-mode call must return an LLMOverrideResult, got nil")
	}
	if len(res.Changed) != 2 || len(res.LeftAlone) != 0 {
		t.Fatalf("want both tasks changed and none left alone; got %+v", res)
	}
	op, _ := outcomeOf(res, "changed", pend)
	if op.CapacityMoved {
		t.Errorf("pending task has no live request; want no capacity move: %+v", op)
	}
	ow, _ := outcomeOf(res, "changed", wait)
	if !ow.CapacityMoved {
		t.Errorf("waiting task must be retargeted, got %+v", ow)
	}
	if cur := out.Story; len(cur) == 0 || cur[0].Backend != "ollama" || cur[0].Model != "m2" {
		t.Errorf("story record not written: %+v", cur)
	}
	// Exactly one live request for the waiting task, now on the new pool.
	pools := waitersFor(t, led, wait, "waiting task queued")
	if !hasPool(pools, workerPoolFastb) || hasPool(pools, workerPoolSolo) {
		t.Errorf("waiter pools = %v; want node:fastb and no node:solo", pools)
	}
	if n := countWaiters(led, pend); n != 0 {
		t.Errorf("pending task must hold no request, holds %d", n)
	}
	if led.Held(wait) {
		t.Fatalf("waiting task must still be queued, holds a lease")
	}
	// The new pool frees up: the single live request resolves to a lease on
	// it — the requeue actually takes effect on the new pair's pool.
	led.Release("busy:fastb")
	lease := waitForLease(t, led, wait, "waiting task admitted on the new pool")
	if !hasPool(lease.Pools, workerPoolFastb) {
		t.Errorf("lease pools = %v; want node:fastb", lease.Pools)
	}
	if led.Held(pend) {
		t.Errorf("pending task was falsely admitted")
	}
}

// --- 2. Running + finished task: both left alone, records and capacity intact

func TestWorkerRequeue_RunningStory_StartedAndFinishedLeftAlone(t *testing.T) {
	m, led := workerRequeueFixture(t)
	prd := newPRDWithTasks(t, m, 2)
	tIDs := runPRDFrozen(t, m, prd, TaskInProgress, TaskCompleted)
	tRun, tDone := tIDs[0], tIDs[1]
	storyID := prd.Story[0].ID

	_, res, err := m.SetStoryLLM(prd.ID, storyID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("SetStoryLLM: %v", err)
	}
	if res == nil {
		t.Fatalf("queued-mode call must return an LLMOverrideResult, got nil")
	}
	if len(res.Changed) != 0 {
		t.Errorf("no task may be changed; got %+v", res.Changed)
	}
	if len(res.LeftAlone) != 2 {
		t.Fatalf("both tasks must be left alone; got %+v", res.LeftAlone)
	}
	or, _ := outcomeOf(res, "left_alone", tRun)
	if !strings.Contains(or.Reason, "running") {
		t.Errorf("started-task reason = %q; want it to say the task is running", or.Reason)
	}
	od, _ := outcomeOf(res, "left_alone", tDone)
	if od.Reason == "" || strings.Contains(od.Reason, "override") {
		t.Errorf("finished-task reason should just state completion: %q", od.Reason)
	}
	// Records of the two tasks are untouched (the story record carries the
	// new pair — only unstarted tasks with no task-level override inherit).
	cur, _ := m.Store().GetPRD(prd.ID)
	i := findTaskOf(cur, tRun)
	j := findTaskOf(cur, tDone)
	if i.Backend != "" || i.Model != "" {
		t.Errorf("started task record modified: %+v", i)
	}
	if j.Backend != "" || j.Model != "" {
		t.Errorf("finished task record modified: %+v", j)
	}
	// And capacity state is untouched: nobody holds a lease, nothing queued.
	for _, h := range []string{tRun, tDone} {
		if led.Held(h) {
			t.Errorf("task %s holds a lease", h)
		}
		if n := countWaiters(led, h); n != 0 {
			t.Errorf("task %s holds %d queued requests", h, n)
		}
	}
}

// --- 3. Per-task override on a waiting task: changed and requeued

func TestWorkerRequeue_TaskLevel_WaitingTaskRequeues(t *testing.T) {
	m, led := workerRequeueFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	tID := runPRDFrozen(t, m, prd, TaskWaitingCapacity)[0]
	startWorkerWaiter(t, led, tID, prd.ID, workerPoolSolo)

	out, res, err := m.SetTaskLLM(prd.ID, tID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if res == nil {
		t.Fatalf("queued-mode call must return an LLMOverrideResult, got nil")
	}
	if len(res.Changed) != 1 || len(res.LeftAlone) != 0 {
		t.Fatalf("want {changed:%s, leftAlone:-}; got %+v", tID, res)
	}
	o, _ := outcomeOf(res, "changed", tID)
	if !o.CapacityMoved {
		t.Errorf("queued task must be retargeted: %+v", o)
	}
	if got := findTaskOf(out, tID); got.Backend != "ollama" || got.Model != "m2" {
		t.Errorf("task record not written: %+v", got)
	}
	pools := waitersFor(t, led, tID, "task queued")
	if !hasPool(pools, workerPoolFastb) || hasPool(pools, workerPoolSolo) {
		t.Errorf("waiter pools = %v; want node:fastb and no node:solo", pools)
	}
	if countWaiters(led, tID) != 1 {
		t.Errorf("want exactly one live request, got %d", countWaiters(led, tID))
	}
	// Pool frees → exactly that request resolves to a lease on the new pool.
	led.Release("busy:fastb")
	lease := waitForLease(t, led, tID, "task admitted on the new pool")
	if !hasPool(lease.Pools, workerPoolFastb) {
		t.Errorf("lease pools = %v; want node:fastb", lease.Pools)
	}
}

// --- 4. override_queued_tasks false: the classic refusal returns

func TestWorkerRequeue_Disabled_RestoresClassicRefusal(t *testing.T) {
	off := false
	cfg := DefaultConfig()
	cfg.OverrideQueuedTasks = &off
	m, prd := overrideFixture(t, PRDRunning, &cfg)
	storyID := prd.Story[0].ID
	tID := prd.Story[0].Tasks[0].ID

	_, res, err := m.SetTaskLLM(prd.ID, tID, "ollama", "", "m2", "op")
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("disabled + running: want classic refusal, got err=%v res=%+v", err, res)
	}
	if res != nil {
		t.Errorf("refused call must return a nil result, got %+v", res)
	}
	_, res, err = m.SetStoryLLM(prd.ID, storyID, "ollama", "", "m2", "op")
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("story-level: want classic refusal, got err=%v res=%+v", err, res)
	}
	if res != nil {
		t.Errorf("refused call must return a nil result, got %+v", res)
	}
	// Nothing was written.
	cur, _ := m.Store().GetPRD(prd.ID)
	if cur.Story[0].Backend != "" || cur.Story[0].Model != "" {
		t.Errorf("story record must be untouched: %+v", cur.Story[0])
	}
	if cur.Story[0].Tasks[0].Backend != "" || cur.Story[0].Tasks[0].Model != "" {
		t.Errorf("task record must be untouched: %+v", cur.Story[0].Tasks[0])
	}
}

// --- 5. task_override_statuses governs classic vs queued

func TestWorkerRequeue_StatusGate_GovernsClassicVersusQueued(t *testing.T) {
	// Default gate: running + ON → queued path (result present).
	mOn, prdOn := overrideFixture(t, PRDRunning, nil)
	if _, res, err := mOn.SetTaskLLM(prdOn.ID, prdOn.Story[0].Tasks[0].ID, "ollama", "", "m2", "op"); err != nil {
		t.Fatalf("running+ON should accept (queued mode): %v", err)
	} else if res == nil {
		t.Fatalf("queued-mode accept must return a result")
	}

	// Default gate: needs_review + ON → classic path, nil result, written.
	mCR, prdCR := overrideFixture(t, PRDNeedsReview, nil)
	tCR := prdCR.Story[0].Tasks[0].ID
	if _, res, err := mCR.SetTaskLLM(prdCR.ID, tCR, "ollama", "", "m2", "op"); err != nil {
		t.Fatalf("needs_review accepts classic: %v", err)
	} else if res != nil {
		t.Errorf("classic accept must return a nil result, got %+v", res)
	}
	if cur, ok := mCR.Store().GetPRD(prdCR.ID); !ok {
		t.Fatalf("GetPRD: not found")
	} else if got := findTaskOf(cur, tCR); got.Backend != "ollama" {
		t.Errorf("classic path must write the record: %+v", got)
	}

	// Custom list ["blocked"]: cancelled is NOT in it → with ON, queued path.
	cfgQ := DefaultConfig()
	cfgQ.TaskOverrideStatuses = []string{"blocked"}
	mQ, prdQ := overrideFixture(t, PRDCancelled, &cfgQ)
	if _, res, err := mQ.SetTaskLLM(prdQ.ID, prdQ.Story[0].Tasks[0].ID, "ollama", "", "m2", "op"); err != nil {
		t.Fatalf("cancelled not in custom list, ON: want queued accept, got %v", err)
	} else if res == nil {
		t.Fatalf("queued-mode accept must return a result")
	}

	// Custom list ["cancelled"]: cancelled IS in it → classic accept, nil result.
	cfgC := DefaultConfig()
	cfgC.TaskOverrideStatuses = []string{"cancelled"}
	mC, prdC := overrideFixture(t, PRDCancelled, &cfgC)
	if _, res, err := mC.SetTaskLLM(prdC.ID, prdC.Story[0].Tasks[0].ID, "ollama", "", "m2", "op"); err != nil {
		t.Fatalf("cancelled in custom list: want classic accept, got %v", err)
	} else if res != nil {
		t.Errorf("classic accept must return a nil result, got %+v", res)
	}
}

// --- 6. Two changes while queued: one live request on the latest pool

func TestWorkerRequeue_TwoChanges_SingleLiveRequestOnLatestPool(t *testing.T) {
	m, led := workerRequeueFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	tID := runPRDFrozen(t, m, prd, TaskWaitingCapacity)[0]
	startWorkerWaiter(t, led, tID, prd.ID, workerPoolSolo)

	if _, res, err := m.SetTaskLLM(prd.ID, tID, "ollama", "", "m", "op"); err != nil {
		t.Fatalf("first change: %v", err)
	} else if o, _ := outcomeOf(res, "changed", tID); !o.CapacityMoved {
		t.Fatalf("first change must retarget: %+v", o)
	}
	if _, res, err := m.SetTaskLLM(prd.ID, tID, "openai", "", "m", "op"); err != nil {
		t.Fatalf("second change: %v", err)
	} else if o, _ := outcomeOf(res, "changed", tID); !o.CapacityMoved {
		t.Fatalf("second change must retarget: %+v", o)
	}

	if n := countWaiters(led, tID); n != 1 {
		t.Fatalf("want exactly ONE live request after two changes, got %d", n)
	}
	pools := waitersFor(t, led, tID, "task queued")
	if !hasPool(pools, workerPoolQuick) {
		t.Errorf("waiter must sit on the LATEST pool node:quick; pools=%v", pools)
	}
	if hasPool(pools, workerPoolSolo) || hasPool(pools, workerPoolFastb) {
		t.Errorf("waiter pools %v still name an earlier pool", pools)
	}
	if led.Held(tID) {
		t.Fatalf("task must still be queued (all pools busy)")
	}
	// No capacity leaked: every busy holder still holds its single slot.
	for _, h := range []string{"busy:solo", "busy:fastb", "busy:quick"} {
		if !led.Held(h) {
			t.Errorf("holder %s lost its lease — capacity leaked", h)
		}
	}
	_ = m
}

// --- 7. Change, task starts, change again: second change leaves it alone

func TestWorkerRequeue_SecondChangeAfterStart_LeavesStartedTaskAlone(t *testing.T) {
	m, led := workerRequeueFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	tID := runPRDFrozen(t, m, prd, TaskWaitingCapacity)[0]
	startWorkerWaiter(t, led, tID, prd.ID, workerPoolSolo)

	// First change: the queued request moves to the new pool…
	if _, res, err := m.SetTaskLLM(prd.ID, tID, "ollama", "", "m2", "op"); err != nil {
		t.Fatalf("first change: %v", err)
	} else if o, _ := outcomeOf(res, "changed", tID); !o.CapacityMoved {
		t.Fatalf("first change must retarget: %+v", o)
	}
	// …admits, and the task starts (holds its lease).
	led.Release("busy:fastb")
	lease := waitForLease(t, led, tID, "task admitted on the new pool")
	if !hasPool(lease.Pools, workerPoolFastb) {
		t.Fatalf("lease pools = %v; want node:fastb", lease.Pools)
	}
	startIt(t, m, prd.ID, TaskInProgress)

	// Second change: the started task is left alone — record untouched,
	// capacity untouched, one left_alone entry with a reason.
	out, res, err := m.SetTaskLLM(prd.ID, tID, "openai", "", "m3", "op")
	if err != nil {
		t.Fatalf("second change: %v", err)
	}
	if res == nil {
		t.Fatalf("queued-mode call must return an LLMOverrideResult, got nil")
	}
	if len(res.Changed) != 0 {
		t.Errorf("a started task must never be in changed: %+v", res.Changed)
	}
	o, ok := outcomeOf(res, "left_alone", tID)
	if !ok {
		t.Fatalf("started task must be in left_alone: %+v", res)
	}
	if !strings.Contains(o.Reason, "running") {
		t.Errorf("reason = %q; want it to say the task is running", o.Reason)
	}
	got := findTaskOf(out, tID)
	if got.Backend != "ollama" || got.Model != "m2" {
		t.Errorf("started task record modified: %+v", got)
	}
	// Capacity: still exactly the one lease from step one; no new request.
	if !led.Held(tID) {
		t.Errorf("started task lost its lease")
	}
	if n := countWaiters(led, tID); n != 0 {
		t.Errorf("second change must not create a queued request; holds %d", n)
	}
	if !led.Held("busy:quick") {
		t.Errorf("busy:quick lost its lease — capacity leaked onto node:quick")
	}
}

// --- small local helpers (package-scoped ones live in the sibling tests) ---

func countWaiters(led *capacity.Ledger, holder string) int {
	n := 0
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			n++
		}
	}
	return n
}

func hasPool(pools []string, want string) bool {
	for _, p := range pools {
		if p == want {
			return true
		}
	}
	return false
}

func findTaskOf(prd *PRD, taskID string) Task {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				return prd.Story[si].Tasks[ti]
			}
		}
	}
	panic("task not found")
}

func startIt(t *testing.T, m *Manager, prdID string, s TaskStatus) {
	t.Helper()
	cur, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("GetPRD: not found")
	}
	for si := range cur.Story {
		cur.Story[si].Tasks[0].Status = s
	}
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
}
