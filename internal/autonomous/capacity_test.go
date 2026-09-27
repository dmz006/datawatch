package autonomous

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

func capFixture(t *testing.T, limit int) (*Manager, *API, *capacity.Ledger) {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:n1", limit)
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"node:n1"}, "n1" })
	return m, api, led
}

func newPRDWithTasks(t *testing.T, m *Manager, n int) *PRD {
	t.Helper()
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	var tasks []Task
	for i := 0; i < n; i++ {
		tasks = append(tasks, Task{Title: fmt.Sprintf("T%d", i), Spec: "write docs/x.md"})
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: tasks}})
	got, _ := m.Store().GetPRD(prd.ID)
	got.MaxConcurrentTasks = n
	_ = m.Store().SavePRD(got)
	return got
}

func TestCapacity_ParallelTasksNeverExceedNodeLimit(t *testing.T) {
	m, api, _ := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 3)
	var cur, peak int32
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		n := atomic.AddInt32(&cur, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, tk *Task) (VerificationResult, error) {
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		return VerificationResult{OK: true}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("want completed, got %s", got.Status)
	}
	if peak != 1 {
		t.Fatalf("node limit 1 but %d sessions ran at once", peak)
	}
}

func TestCapacity_WaitingTaskShowsStatusAndNeverFails(t *testing.T) {
	m, api, led := capFixture(t, 1)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "other-prd-task", PRDID: "other", Pools: []string{"node:n1"}}, time.Second, nil, nil)
	spawned := int32(0)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) { atomic.AddInt32(&spawned, 1); return SpawnResult{SessionID: "s"}, nil }
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) { return VerificationResult{OK: true}, nil }
	// SetExecutors kicks an async boot-resume scan; let it finish before the PRD exists.
	api.SetExecutors(spawn, verify)
	time.Sleep(100 * time.Millisecond)
	prd := newPRDWithTasks(t, m, 1)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	var reason string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ := m.Store().GetPRD(prd.ID)
		tk := cur.Story[0].Tasks[0]
		if tk.Status == TaskWaitingCapacity {
			reason = tk.WaitReason
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reason == "" || atomic.LoadInt32(&spawned) != 0 {
		t.Fatalf("task should be waiting_capacity with a reason and no spawn (reason=%q spawned=%d)", reason, spawned)
	}
	led.Release("other-prd-task")
	got := runWait(t, m, prd.ID)
	if got.Status != PRDCompleted || got.Story[0].Tasks[0].WaitReason != "" || got.Story[0].Tasks[0].RetryCount != 0 {
		t.Fatalf("after capacity frees the task must complete without consuming a retry: %+v", got.Story[0].Tasks[0])
	}
}

func runWait(t *testing.T, m *Manager, id string) *PRD {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := m.Store().GetPRD(id)
		if got.Status == PRDCompleted || got.Status == PRDFailed {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("PRD did not finish")
	return nil
}

func TestCapacity_SessionCapErrorIsWaitNotFailure(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	var mu sync.Mutex
	calls := 0
	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	defer func() { capRetryPause = old }()
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls < 3 {
			return SpawnResult{}, fmt.Errorf("start failed: max sessions (5) reached")
		}
		return SpawnResult{SessionID: "s"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted || calls != 3 {
		t.Fatalf("cap rejection must be retried: status=%s calls=%d", got.Status, calls)
	}
	if got.Story[0].Tasks[0].RetryCount != 0 {
		t.Fatal("waiting must not consume auto-fix retries")
	}
}

func TestCapacity_LeaseReleasedAfterTask(t *testing.T) {
	m, api, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) { return SpawnResult{SessionID: "s-" + r.TaskID}, nil }
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) { return VerificationResult{OK: true}, nil }
	runToTerminal(t, m, api, prd.ID, spawn, verify)
	if len(led.Snapshot().Leases) != 0 {
		t.Fatalf("leases leaked: %+v", led.Snapshot().Leases)
	}
}

func TestCapacity_DisabledSkipsAdmission(t *testing.T) {
	m, api, led := capFixture(t, 1)
	off := false
	m.mu.Lock()
	m.cfg.CapacityEnabled = &off
	m.mu.Unlock()
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "x", Pools: []string{"node:n1"}}, time.Second, nil, nil)
	prd := newPRDWithTasks(t, m, 1)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) { return SpawnResult{SessionID: "s"}, nil }
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) { return VerificationResult{OK: true}, nil }
	if got := runToTerminal(t, m, api, prd.ID, spawn, verify); got.Status != PRDCompleted {
		t.Fatalf("admission disabled must not block: %s", got.Status)
	}
}

func TestCapacity_CancelWhileWaitingRemovesFromQueueAndKeepsCancelled(t *testing.T) {
	m, api, led := capFixture(t, 1)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "holder", PRDID: "other", Pools: []string{"node:n1"}}, time.Second, nil, nil)
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) { return SpawnResult{SessionID: "s"}, nil }
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) { return VerificationResult{OK: true}, nil }
	api.SetExecutors(spawn, verify)
	time.Sleep(100 * time.Millisecond)
	prd := newPRDWithTasks(t, m, 1)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	var taskID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && taskID == "" {
		cur, _ := m.Store().GetPRD(prd.ID)
		if tk := cur.Story[0].Tasks[0]; tk.Status == TaskWaitingCapacity {
			taskID = tk.ID
		}
		time.Sleep(10 * time.Millisecond)
	}
	if taskID == "" {
		t.Fatal("task never reached waiting_capacity")
	}
	if _, err := m.CancelTask(prd.ID, taskID, "operator", "test"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(led.Snapshot().Waiting) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(led.Snapshot().Waiting) != 0 {
		t.Fatal("cancelled task still in the wait queue")
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	if cur.Story[0].Tasks[0].Status != TaskCancelled {
		t.Fatalf("cancelled task must stay cancelled, got %s", cur.Story[0].Tasks[0].Status)
	}
}

func TestCapacity_BootResumeRequeuesWaitingTasks(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskWaitingCapacity
	tk.WaitReason = "pool host full"
	_ = m.Store().SaveTask(tk)
	cur, _ = m.Store().GetPRD(prd.ID)
	m.resetInProgressTasksForResume(cur)
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Tasks[0].Status != TaskPending {
		t.Fatalf("waiting task must be re-queued as pending on boot, got %s", got.Story[0].Tasks[0].Status)
	}
}

// A stall (ErrWorkerStalled) must consume the normal auto_fix_retries budget
// like any other verification failure, not fail the task on the first
// occurrence — the error text itself says "retrying task".
func TestExecutor_WorkerStallRespectsRetryBudget(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)

	spawns := 0
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: fmt.Sprintf("s-%d", spawns)}, nil
	}
	verifyCalls := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		verifyCalls++
		if verifyCalls == 1 {
			return VerificationResult{}, fmt.Errorf("stall: %w", ErrWorkerStalled)
		}
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("a stalled-then-recovered task must complete, got %s", got.Status)
	}
	if spawns != 2 {
		t.Fatalf("want 2 spawns (initial + 1 retry after stall), got %d", spawns)
	}
	if got.Story[0].Tasks[0].Error == "" {
		t.Fatal("the stall reason should be recorded on the task even after it recovers")
	}
}

func TestExecutor_WorkerStallExhaustsRetriesLikeNormalFailure(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)

	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{}, fmt.Errorf("stall: %w", ErrWorkerStalled)
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDFailed {
		t.Fatalf("repeated stalls must still fail once retries are exhausted, got %s", got.Status)
	}
}
