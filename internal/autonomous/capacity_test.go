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
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawned, 1)
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	// SetExecutors kicks an async boot-resume scan; let it finish before the PRD exists.
	api.SetExecutors(spawn, verify)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	prd := newPRDWithTasks(t, m, 1)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	var reason string
	pollUntil(t, 3*time.Second, "task waiting_capacity", func() bool {
		cur, _ := m.Store().SnapshotPRD(prd.ID)
		tk := cur.Story[0].Tasks[0]
		if tk.Status == TaskWaitingCapacity {
			reason = tk.WaitReason
			return true
		}
		return false
	})
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
	pollPRDTerminal(t, m, id, 5*time.Second)
	got, _ := m.Store().SnapshotPRD(id)
	return got
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

// BL407 Phase 4 — a workspace-lock collision (two of the same PRD's
// own cluster-dispatched tasks briefly sharing one Phase-4-assigned
// branch) must be retried like a capacity wait, not surfaced as a
// hard task failure.
func TestCapacity_WorkspaceLockErrorIsWaitNotFailure(t *testing.T) {
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
			return SpawnResult{}, fmt.Errorf(`workspace lock: agent a1 already owns project "p" branch "automaton/x" (state=running)`)
		}
		return SpawnResult{SessionID: "s"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted || calls != 3 {
		t.Fatalf("workspace-lock rejection must be retried: status=%s calls=%d", got.Status, calls)
	}
	if got.Story[0].Tasks[0].RetryCount != 0 {
		t.Fatal("waiting must not consume auto-fix retries")
	}
}

func TestIsWorkspaceLockError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("some other error"), false},
		{fmt.Errorf(`workspace lock: agent a1 already owns project "p" branch "b" (state=running)`), true},
	}
	for _, c := range cases {
		if got := IsWorkspaceLockError(c.err); got != c.want {
			t.Errorf("IsWorkspaceLockError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestCapacity_LeaseReleasedAfterTask(t *testing.T) {
	m, api, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
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
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	if got := runToTerminal(t, m, api, prd.ID, spawn, verify); got.Status != PRDCompleted {
		t.Fatalf("admission disabled must not block: %s", got.Status)
	}
}

func TestCapacity_CancelWhileWaitingRemovesFromQueueAndKeepsCancelled(t *testing.T) {
	m, api, led := capFixture(t, 1)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "holder", PRDID: "other", Pools: []string{"node:n1"}}, time.Second, nil, nil)
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) { return SpawnResult{SessionID: "s"}, nil }
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, verify)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	prd := newPRDWithTasks(t, m, 1)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	var taskID string
	pollUntil(t, 3*time.Second, "task waiting_capacity", func() bool {
		cur, _ := m.Store().SnapshotPRD(prd.ID)
		if tk := cur.Story[0].Tasks[0]; tk.Status == TaskWaitingCapacity {
			taskID = tk.ID
		}
		return taskID != ""
	})
	if _, err := m.CancelTask(prd.ID, taskID, "operator", "test"); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, 3*time.Second, "cancelled task leaving the wait queue", func() bool {
		return len(led.Snapshot().Waiting) == 0
	})
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

// BL462 F1/F2/F3: a task that was verifying on a live session when the
// daemon restarted keeps its session, re-takes its capacity lease at boot
// (so the host pool is not over-admitted), and keeps the lease until the
// session ends.
func TestCapacity_BootResumeReadoptsLiveTaskLease(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	live := &cur.Story[0].Tasks[0]
	live.Status = TaskVerifying
	live.SessionID = "live-sess"
	_ = m.Store().SaveTask(live)
	cur, _ = m.Store().GetPRD(prd.ID)

	alive := map[string]bool{"live-sess": true}
	m.SetSessionAliveFn(func(id string) bool { return alive[id] })

	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(prd.ID)
	if st := got.Story[0].Tasks[0].Status; st != TaskVerifying {
		t.Fatalf("live task must stay verifying, got %s", st)
	}
	if got.Story[0].Tasks[0].SessionID != "live-sess" {
		t.Fatalf("live task must keep its session, got %q", got.Story[0].Tasks[0].SessionID)
	}
	if !led.Held(live.ID) {
		t.Fatal("live task holds no capacity lease after boot")
	}

	// Idempotent: a second boot-time pass, or executeOne's own adoption,
	// must not add a second lease.
	m.adoptCapacity(cur, live)
	m.resetInProgressTasksForResume(cur)
	if n := len(led.Snapshot().Leases); n != 1 {
		t.Fatalf("want exactly one lease for the live task, got %d", n)
	}

	// The limit is 1 and the live task holds the slot, so the sibling task
	// must not be admitted over it.
	sib := &cur.Story[0].Tasks[1]
	err := led.Acquire(context.Background(), capacity.Request{Holder: sib.ID, PRDID: prd.ID, Pools: []string{"node:n1"}}, 50*time.Millisecond, nil, nil)
	if err == nil {
		t.Fatal("sibling was admitted over a live task's slot")
	}

	// Reaping keeps the lease while the session is alive, and frees it
	// once the session ends.
	if rel := led.Reap(func(ls capacity.Lease) bool { return alive[ls.SessionID] }); len(rel) != 0 {
		t.Fatalf("live lease reaped: %v", rel)
	}
	alive["live-sess"] = false
	if rel := led.Reap(func(ls capacity.Lease) bool { return alive[ls.SessionID] }); len(rel) != 1 {
		t.Fatalf("lease of ended session not reaped, got %v", rel)
	}
}

// localEngineKeys mirrors cmd/datawatch's keys function: a local-engine
// session takes its provider's node pool, a hosted-backend session takes none.
func localEngineKeys(backend, model string) ([]string, string) {
	pools := []string{"host"}
	node, gated := LocalEngineGate(backend, model, func(provider string) (string, bool) {
		if provider == "ollama" {
			return "alpha", true
		}
		return "", false
	})
	if gated {
		pools = append(pools, "node:"+node)
	}
	return pools, node
}

func capFixtureLocalEngine(t *testing.T) (*Manager, *API, *capacity.Ledger) {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:alpha", 1)
	m.SetCapacity(led, localEngineKeys)
	return m, api, led
}

func newPRDOnBackend(t *testing.T, m *Manager, backend, model string) *PRD {
	t.Helper()
	prd, _ := m.CreatePRD("s", "/w/proj", backend, model, EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T0", Spec: "write docs/x.md"}}}})
	got, _ := m.Store().GetPRD(prd.ID)
	got.MaxConcurrentTasks = 1
	_ = m.Store().SavePRD(got)
	return got
}

func TestCapacity_LocalEngineTaskWaitsOnNodePool(t *testing.T) {
	m, api, led := capFixtureLocalEngine(t)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:alpha"}}, time.Second, nil, nil)
	spawned := int32(0)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawned, 1)
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, verify)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	prd := newPRDOnBackend(t, m, "pi", "ollama/qwen3.8:27b")
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)
	if err := api.Run(prd.ID); err != nil {
		t.Fatal(err)
	}
	var reason string
	pollUntil(t, 3*time.Second, "task waiting_capacity", func() bool {
		cur, _ := m.Store().SnapshotPRD(prd.ID)
		tk := cur.Story[0].Tasks[0]
		if tk.Status == TaskWaitingCapacity {
			reason = tk.WaitReason
			return true
		}
		return false
	})
	if reason == "" || atomic.LoadInt32(&spawned) != 0 {
		t.Fatalf("local-engine task should wait on node:alpha with no spawn (reason=%q spawned=%d)", reason, spawned)
	}
	led.Release("other")
	if got := runWait(t, m, prd.ID); got.Status != PRDCompleted {
		t.Fatalf("after node:alpha frees the task must complete, got %s", got.Status)
	}
}

func TestCapacity_HostedTaskNeverWaitsOnNodePool(t *testing.T) {
	m, api, led := capFixtureLocalEngine(t)
	_ = led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:alpha"}}, time.Second, nil, nil)
	spawned := int32(0)
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawned, 1)
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	prd := newPRDOnBackend(t, m, "claude-code", "claude-sonnet-5-5")
	if got := runToTerminal(t, m, api, prd.ID, spawn, verify); got.Status != PRDCompleted {
		t.Fatalf("hosted task must not wait on node:alpha, got %s", got.Status)
	}
	if atomic.LoadInt32(&spawned) != 1 {
		t.Fatalf("hosted task should spawn once, spawned=%d", spawned)
	}
	led.Release("other")
}

// fakeCapClock is a capacity.Clock whose time only moves when a test sets it.
// Its After channels never fire, so a queued request is admitted only when a
// lease is released, never because real time passed.
type fakeCapClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeCapClock() *fakeCapClock {
	return &fakeCapClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeCapClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeCapClock) After(time.Duration) <-chan time.Time {
	return make(chan time.Time)
}

// capFixtureFakeClock is capFixture with the ledger on the fake clock, so the
// tests below cannot depend on wall-clock poll ticks or wait deadlines.
func capFixtureFakeClock(t *testing.T, limit int) (*Manager, *API, *capacity.Ledger) {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit("node:n1", limit)
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"node:n1"}, "n1" })
	return m, api, led
}

// capWaitUntil polls until cond holds, failing the test after two seconds.
func capWaitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	pollUntil(t, 2*time.Second, "condition", cond)
}

// capNodeReq is a request for one task on the node:n1 pool.
func capNodeReq(holder, prdID string) capacity.Request {
	return capacity.Request{Holder: holder, PRDID: prdID, Pools: []string{"node:n1"}, Node: "n1"}
}

// admitQueued starts a blocking Acquire for holder and returns the channel
// its result arrives on.
func admitQueued(led *capacity.Ledger, req capacity.Request) <-chan error {
	done := make(chan error, 1)
	go func() { done <- led.Acquire(context.Background(), req, 0, nil, nil) }()
	return done
}

// awaitAdmitted fails the test unless the queued request is granted promptly.
func awaitAdmitted(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: acquire failed: %v", what, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: queued task was not admitted", what)
	}
}

// A holder whose session is killed loses its node lease at the next reap,
// and the next queued task on that node is admitted. The session table is
// in-memory; the liveness check mirrors cmd/datawatch's lease reaper.
func TestCapacity_KilledHolderLeaseReleasedAdmitsNextOnNode(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0].ID, cur.Story[0].Tasks[1].ID

	sessions := map[string]string{"sess-holder": "running"}
	live := func(ls capacity.Lease) bool {
		st, ok := sessions[ls.SessionID]
		return ok && st != "complete" && st != "failed" && st != "killed"
	}

	if err := led.Acquire(context.Background(), capNodeReq(holder, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	led.Bind(holder, "sess-holder")
	done := admitQueued(led, capNodeReq(queued, prd.ID))
	capWaitUntil(t, func() bool { return len(led.Snapshot().Waiting) == 1 })

	if rel := led.Reap(live); len(rel) != 0 {
		t.Fatalf("live holder reaped: %v", rel)
	}
	sessions["sess-holder"] = "killed"
	if rel := led.Reap(live); len(rel) != 1 || rel[0] != holder {
		t.Fatalf("killed holder not reaped, got %v", rel)
	}
	awaitAdmitted(t, done, "after kill")
	if led.Held(holder) || !led.Held(queued) {
		t.Fatal("lease not handed from the killed holder to the queued task")
	}
}

// A stale holder (its worker stopped making progress, but the session is
// still reported running) is released through releaseCapacity, the call the
// executor makes on the stall path, and the next queued task is admitted.
func TestCapacity_StaleHolderReleaseAdmitsNextOnNode(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	stale, queued := cur.Story[0].Tasks[0].ID, cur.Story[0].Tasks[1].ID

	if err := led.Acquire(context.Background(), capNodeReq(stale, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	// The session stays "running" from the session manager's view, so the
	// reaper would keep this lease; only the executor's stall release frees it.
	led.Bind(stale, "sess-stale")
	done := admitQueued(led, capNodeReq(queued, prd.ID))
	capWaitUntil(t, func() bool { return len(led.Snapshot().Waiting) == 1 })

	m.releaseCapacity(stale)
	awaitAdmitted(t, done, "after stale release")
	if led.Held(stale) || !led.Held(queued) {
		t.Fatal("stale holder's lease was not released to the queued task")
	}
}

// The executor's stall path frees the stalled worker's lease before the
// retry spawns, so the retry's session kill sees no lease held.
func TestCapacity_StalledWorkerLeaseReleasedBeforeRetry(t *testing.T) {
	m, api, led := capFixtureFakeClock(t, 1)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)

	var heldAtKill []bool
	m.SetSessionKillerFn(func(string) error {
		heldAtKill = append(heldAtKill, len(led.Snapshot().Leases) > 0)
		return nil
	})
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
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("a stalled-then-recovered task must complete, got %s", got.Status)
	}
	if len(heldAtKill) == 0 || heldAtKill[0] {
		t.Fatalf("stalled worker's lease was still held when its session was killed for retry: %v", heldAtKill)
	}
	if n := len(led.Snapshot().Leases); n != 0 {
		t.Fatalf("leases leaked after completion: %d", n)
	}
}

// Cancelling a task releases its lease immediately, so a waiting task on the
// same node is not held behind a cancelled holder.
func TestCapacity_CancelTaskReleasesLease(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]

	if err := led.Acquire(context.Background(), capNodeReq(task.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CancelTask(prd.ID, task.ID, "test", ""); err != nil {
		t.Fatal(err)
	}
	if led.Held(task.ID) {
		t.Fatal("cancelled task still holds its capacity lease")
	}
}
