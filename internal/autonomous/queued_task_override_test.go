package autonomous

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// The tests in this file pin the override_queued_tasks contract:
//   - while the PRD is in none of task_override_statuses (here: running) and
//     override_queued_tasks is enabled (nil = on), an unstarted task is
//     allowed and every started/finished task is left alone with a reason
//     (c);
//   - an explicitly disabled setting restores the exact classic refusal (b);
//   - the classic accepted paths never return an LLMOverrideResult (a/c);
//   - a queued capacity request is retargeted to the new backend's pool
//     (d) and retargeting twice leaves exactly one live request on the
//     latest pool (e).

func TestQueuedTaskOverride_TaskLevelDefaultOn(t *testing.T) {
	m, prd := overrideFixture(t, PRDRunning, nil) // running is not in the default accepted list; nil = enabled
	taskID := prd.Story[0].Tasks[0].ID

	_, res, err := m.SetTaskLLM(prd.ID, taskID, "opencode", "", "m", "op")
	if err != nil {
		t.Fatalf("SetTaskLLM on unlocked-by-queued mode: %v", err)
	}
	if res == nil {
		t.Fatal("queued-mode call must return a non-nil LLMOverrideResult")
	}
	if len(res.Changed) != 1 || res.Changed[0].TaskID != taskID {
		t.Fatalf("changed = %+v, want the one task changed", res.Changed)
	}
	if len(res.LeftAlone) != 0 {
		t.Fatalf("left_alone = %+v, want none", res.LeftAlone)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	task, _ := findTaskInPRD(got, taskID)
	if task.Backend != "opencode" {
		t.Fatalf("task backend = %q, want opencode", task.Backend)
	}
}

func TestQueuedTaskOverride_TaskLevelDisabledRefuses(t *testing.T) {
	m, prd := overrideFixture(t, PRDRunning, nil)
	off := false
	m.cfg.OverrideQueuedTasks = &off
	taskID := prd.Story[0].Tasks[0].ID

	_, res, err := m.SetTaskLLM(prd.ID, taskID, "opencode", "", "m", "op")
	if err == nil {
		t.Fatal("disabled override_queued_tasks must keep the classic refusal")
	}
	if !strings.Contains(err.Error(), "is locked") {
		t.Errorf("error should say the status is locked, got: %v", err)
	}
	if res != nil {
		t.Errorf("refused call must have a nil result, got %+v", res)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	task, _ := findTaskInPRD(got, taskID)
	if task.Backend != "" {
		t.Errorf("refused call must not touch the task, backend = %q", task.Backend)
	}
}

func TestQueuedTaskOverride_TaskLevelCustomListStaysLocked(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TaskOverrideStatuses = []string{"blocked"} // cancelled is locked by this list
	m, prd := overrideFixture(t, PRDCancelled, &cfg)
	taskID := prd.Story[0].Tasks[0].ID

	// Cancelled is rejected by the [blocked] list, so with override_queued_tasks
	// enabled (the default) the queued path must apply.
	_, res, err := m.SetTaskLLM(prd.ID, taskID, "opencode", "", "m", "op")
	if err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if res == nil {
		t.Fatal("queued-mode call must return a non-nil LLMOverrideResult")
	}
	if len(res.Changed) != 1 {
		t.Fatalf("changed = %+v, want the unstarted task", res.Changed)
	}
}

func TestQueuedTaskOverride_ClassicPathNilResult(t *testing.T) {
	m, prd := overrideFixture(t, PRDBlocked, nil) // blocked is in the default accepted list
	taskID := prd.Story[0].Tasks[0].ID

	_, res, err := m.SetTaskLLM(prd.ID, taskID, "opencode", "", "m", "op")
	if err != nil {
		t.Fatalf("classic accepted path: %v", err)
	}
	if res != nil {
		t.Errorf("classic path must return a nil LLMOverrideResult, got %+v", res)
	}
}

func TestQueuedStoryOverride_SplitsChangedAndLeftAlone(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd := newPRDWithTasks(t, m, 3)
	ids := setTaskStatuses(t, m, prd.ID, TaskPending, TaskInProgress, TaskCompleted)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	storyID := cur.Story[0].ID

	_, res, err := m.SetStoryLLM(prd.ID, storyID, "opencode", "", "m", "op")
	if err != nil {
		t.Fatalf("SetStoryLLM on running PRD (queued mode): %v", err)
	}
	if res == nil {
		t.Fatal("nil LLMOverrideResult, want per-task split")
	}
	if len(res.Changed) != 1 || res.Changed[0].TaskID != ids[0] {
		t.Fatalf("changed = %+v, want only task %s (the unstarted one)", res.Changed, ids[0])
	}
	if len(res.LeftAlone) != 2 {
		t.Fatalf("left_alone = %+v, want the two started tasks", res.LeftAlone)
	}
	byID := map[string]*LLMOverrideOutcome{}
	for i := range res.LeftAlone {
		byID[res.LeftAlone[i].TaskID] = &res.LeftAlone[i]
	}
	if got := byID[ids[1]]; got == nil || !strings.Contains(got.Reason, "running") {
		t.Errorf("left_alone[%s] = %+v, want a running-task reason", ids[1], got)
	}
	if got := byID[ids[2]]; got == nil || !strings.Contains(got.Reason, "completed") {
		t.Errorf("left_alone[%s] = %+v, want a finished-task reason", ids[2], got)
	}
	// The story record carries the new pair ...
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Backend != "opencode" {
		t.Errorf("story backend = %q, want opencode", got.Story[0].Backend)
	}
	// ... while the started task record is untouched.
	task, _ := findTaskInPRD(got, ids[1])
	if task.Backend != "" || task.Model != "" {
		t.Errorf("started task must stay untouched, got backend %q model %q", task.Backend, task.Model)
	}
}

// TestQueuedTaskOverride_RetargetsWaitingCapacity pins (d): a task holding a
// queued request on the old pool is retargeted to the new backend's pool,
// and the ledger grants exactly one live request for it there.
func TestQueuedTaskOverride_RetargetsWaitingCapacity(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"solo", "fastb"} {
		led.SetLimit("node:"+n, 1)
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if backend == "ollama" {
			return []string{"host", "llm:pool", "node:fastb"}, "fastb"
		}
		return []string{"host", "llm:pool", "node:solo"}, "solo"
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := cur.Story[0].Tasks[0].ID
	cur.Story[0].Tasks[0].Status = TaskWaitingCapacity
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	queueWaiter(t, led, taskID, prd.ID)
	if countTaskWaiters(led, taskID) != 1 {
		t.Fatalf("waiters = %d, want 1 on the old pool before the override", countTaskWaiters(led, taskID))
	}

	_, res, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if res == nil || len(res.Changed) != 1 {
		t.Fatalf("res = %+v, want one changed task", res)
	}
	if !res.Changed[0].CapacityMoved {
		t.Fatalf("CapacityMoved = false, want true (task was queued on the old pool)")
	}
	// fastb is empty, so the retargeted wait resolves to a single lease on it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ls := led.LeaseOf(taskID); ls != nil {
			if ls.Node != "fastb" {
				t.Fatalf("lease node = %q, want fastb", ls.Node)
			}
			break
		}
		if time.Now().After(deadline) {
			snap := led.Snapshot()
			t.Fatalf("task never granted after retarget; waiting = %+v", snap.Waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestQueuedTaskOverride_RetargetTwiceKeepsOneLiveRequest pins (e): the same
// task overridden twice ends with exactly one live capacity request, on the
// pool of the latest backend.
func TestQueuedTaskOverride_RetargetTwiceKeepsOneLiveRequest(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"solo", "fastb", "quick"} {
		led.SetLimit("node:"+n, 1)
	}
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "occ-fastb", PRDID: "occ", Pools: []string{"node:fastb"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy fastb: %v", err)
	}
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "occ-quick", PRDID: "occ", Pools: []string{"node:quick"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy quick: %v", err)
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		switch backend {
		case "ollama":
			return []string{"host", "llm:pool", "node:fastb"}, "fastb"
		case "openai":
			return []string{"host", "llm:pool", "node:quick"}, "quick"
		default:
			return []string{"host", "llm:pool", "node:solo"}, "solo"
		}
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := cur.Story[0].Tasks[0].ID
	cur.Story[0].Tasks[0].Status = TaskWaitingCapacity
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	queueWaiter(t, led, taskID, prd.ID)

	first, res, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("first override: %v", err)
	}
	if res == nil || len(res.Changed) != 1 || !res.Changed[0].CapacityMoved {
		t.Fatalf("first override res = %+v, want the task changed and capacity moved", res)
	}
	if w := taskWaiterPools(led, taskID); w == nil || !slices.Contains(w, "node:fastb") {
		t.Fatalf("after first override waiter pools = %v, want the fastb pool", w)
	}
	_, res, err = m.SetTaskLLM(first.ID, taskID, "openai", "", "gpt", "op")
	if err != nil {
		t.Fatalf("second override: %v", err)
	}
	if res == nil || len(res.Changed) != 1 || !res.Changed[0].CapacityMoved {
		t.Fatalf("second override res = %+v, want the task changed and capacity moved", res)
	}
	if n := countTaskWaiters(led, taskID); n != 1 {
		t.Fatalf("live requests after two overrides = %d, want exactly one", n)
	}
	w := taskWaiterPools(led, taskID)
	if w == nil || !slices.Contains(w, "node:quick") {
		t.Fatalf("waiter pools = %v, want the latest (openai/quick) pool", w)
	}
	if slices.Contains(w, "node:solo") || slices.Contains(w, "node:fastb") {
		t.Fatalf("waiter pools = %v, an old pool must not remain on the request", w)
	}
	if ls := led.LeaseOf(taskID); ls != nil {
		t.Fatalf("lease = %+v, want the task still queued (every pool full)", ls)
	}
}

// TestRetargetRewritesCurrentWaitPools pins the wait-reason pool naming after
// a retarget: the manager's per-task "currently waiting on" record, which the
// wait-reason annotator (nameRequestedPool) reads so the operator sees the
// pool the holder moved TO, must drop the entry-time pool and carry the new
// new one. The closure used to capture the entry-time pool once and keep
// naming it even after the held request moved.
func TestRetargetRewritesCurrentWaitPools(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:solo", 1)
	led.SetLimit("node:fastb", 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if backend == "ollama" {
			return []string{"host", "llm:pool", "node:fastb"}, "fastb"
		}
		return []string{"host", "llm:pool", "node:solo"}, "solo"
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := cur.Story[0].Tasks[0].ID
	cur.Story[0].Tasks[0].Status = TaskWaitingCapacity
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	queueWaiter(t, led, taskID, prd.ID)

	// Seed the current-wait record exactly as admit does (the entry-time pool).
	m.setWaitReqPools(taskID, []string{"host", "llm:pool", "node:solo"})
	if got := m.waitReqPoolsNow(taskID); !slices.Contains(got, "node:solo") {
		t.Fatalf("seeded current wait pools = %v, want the entry-time pool node:solo", got)
	}

	_, res, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m2", "op")
	if err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if res == nil || len(res.Changed) != 1 || !res.Changed[0].CapacityMoved {
		t.Fatalf("res = %+v, want the retarget to move the queued capacity", res)
	}

	// After the retarget the record must point at the NEW pool and no longer
	// carry the entry-time one, so the wait reason names the pool the holder
	// actually moved to.
	got := m.waitReqPoolsNow(taskID)
	if !slices.Contains(got, "node:fastb") {
		t.Fatalf("current wait pools after retarget = %v, want the new pool node:fastb", got)
	}
	if slices.Contains(got, "node:solo") {
		t.Fatalf("current wait pools after retarget = %v, the entry-time pool node:solo must be dropped", got)
	}
}

func countTaskWaiters(led *capacity.Ledger, holder string) int {
	n := 0
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			n++
		}
	}
	return n
}

func taskWaiterPools(led *capacity.Ledger, holder string) []string {
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			return w.Pools
		}
	}
	return nil
}
