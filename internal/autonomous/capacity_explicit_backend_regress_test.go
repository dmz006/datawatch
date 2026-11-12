package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

const (
	regressLocalPool  = "llm:local"
	regressClaudePool = "llm:claude-code"
)

// regressKeys maps claude-code to its own pool and every other backend to the
// local pool, and records each (backend, model) it is asked about.
type regressKeys struct {
	mu    sync.Mutex
	calls [][2]string
}

func (k *regressKeys) keys(backend, model string) ([]string, string) {
	k.mu.Lock()
	k.calls = append(k.calls, [2]string{backend, model})
	k.mu.Unlock()
	if backend == "claude-code" {
		return []string{regressClaudePool}, ""
	}
	return []string{regressLocalPool}, ""
}

func (k *regressKeys) asked(backend, model string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, c := range k.calls {
		if c[0] == backend && c[1] == model {
			return true
		}
	}
	return false
}

// (a) + (b): a held task retried through ResolveTaskReview with an explicit
// claude-code/sonnet worker is admitted against the claude-code pool while
// the local pool is full, and its record matches the admission request.
func TestRegress_ReviewRetryExplicitBackendAdmittedOnRequestedPool(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, spawns := heldTaskOf(t, m, api, prd)
	before := len(spawns.workers())

	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit(regressLocalPool, 1)
	led.SetLimit(regressClaudePool, 1)
	keys := &regressKeys{}
	m.SetCapacity(led, keys.keys)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{regressLocalPool}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var leasePools []string
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		if req.Backend == "claude-code" {
			if ls := led.LeaseOf(task.ID); ls != nil {
				mu.Lock()
				leasePools = append([]string(nil), ls.Pools...)
				mu.Unlock()
			}
		}
		return spawns.spawn(ctx, req)
	}
	ok := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api.SetExecutors(spawn, ok)

	res := ReviewResolution{Action: reviewActionRetry, Backend: "claude-code", Model: "sonnet", Effort: EffortNormal}
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, res, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview: %v", err)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)

	reqs := spawnRequests(spawns)
	if len(reqs) <= before {
		t.Fatal("retried task never spawned: it was not admitted while the local pool was full")
	}
	if reqs[before].Backend != "claude-code" || reqs[before].Model != "sonnet" {
		t.Fatalf("spawn worker = %s/%s, want claude-code/sonnet", reqs[before].Backend, reqs[before].Model)
	}
	mu.Lock()
	got := leasePools
	mu.Unlock()
	if len(got) != 1 || got[0] != regressClaudePool {
		t.Fatalf("lease pools = %v, want [%s]", got, regressClaudePool)
	}
	if !keys.asked("claude-code", "sonnet") {
		t.Fatalf("admission never requested claude-code/sonnet: %v", keys.calls)
	}

	// (b) the task record shows what was requested.
	rec := escalationTask(t, m, prd.ID)
	if rec.Backend != "claude-code" || rec.Model != "sonnet" {
		t.Fatalf("task record worker = %s/%s, want claude-code/sonnet", rec.Backend, rec.Model)
	}
	if rec.Backend != reqs[before].Backend || rec.Model != reqs[before].Model {
		t.Fatalf("task record %s/%s does not match the admission request %s/%s", rec.Backend, rec.Model, reqs[before].Backend, reqs[before].Model)
	}
	if rec.Status != TaskCompleted && normTaskStatus(rec.Status) != TaskCompleted {
		t.Fatalf("task status = %q, want completed", rec.Status)
	}
	if ls := led.LeaseOf("other"); ls == nil {
		t.Fatal("local-pool holder lost its lease: the test did not keep the local pool full")
	}
}

// (c): the live wait reason names the requested pool, and classification
// ignores the suffix: the original text and the suffixed text are both
// pool_full.
func TestRegress_WaitReasonNamesRequestedPoolAndStaysPoolFull(t *testing.T) {
	m, _, _ := capFixtureFakeClock(t, 1)
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit("host", 1)
	led.SetLimit(regressClaudePool, 1)
	// claude-code resolves to its llm pool plus the shared host pool; the
	// host pool is the one that is full, so the ledger's own text never
	// names llm:claude-code.
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{regressClaudePool, "host"}, "" })
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	if err := led.Acquire(context.Background(), capacity.Request{Holder: holder.ID, PRDID: prd.ID, Pools: []string{"host"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	// The ledger's own text for the same wait, before any suffix.
	var original string
	probeCtx, cancel := context.WithCancel(context.Background())
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		_ = led.Acquire(probeCtx, capacity.Request{Holder: "probe", PRDID: prd.ID, Pools: []string{regressClaudePool, "host"}}, 0, nil, func(reason string) {
			if original == "" {
				original = reason
				cancel()
			}
		})
	}()
	<-probeDone
	if original == "" {
		t.Fatal("ledger gave no wait reason")
	}

	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(context.Background(), cur, &queued, "claude-code", "sonnet", capacity.RoleWorker, nil, "")
	}()
	capWaitUntil(t, func() bool {
		snap, ok := m.Store().TaskSnapshot(queued.ID)
		return ok && snap.Status == TaskWaitingCapacity && snap.WaitReason != ""
	})
	snap, _ := m.Store().TaskSnapshot(queued.ID)
	if !strings.Contains(snap.WaitReason, "requested pool llm:claude-code") {
		t.Fatalf("wait reason does not name the requested pool: %q", snap.WaitReason)
	}
	if got := ClassifyWaitReason(original); got != WaitCausePoolFull {
		t.Fatalf("original %q classified %q, want pool_full", original, got)
	}
	if got := ClassifyWaitReason(snap.WaitReason); got != WaitCausePoolFull {
		t.Fatalf("suffixed %q classified %q, want pool_full", snap.WaitReason, got)
	}
	led.Release(holder.ID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// (d1): a first acquire that waits 3h on the audit clock, then re-enters
// through the node-load failover path, gets only the remaining 1h of a 4h
// budget.
func TestRegress_WaitBudgetRemainingAfterThreeHourWait(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 4 * 3600
	m.mu.Unlock()
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	if err := led.Acquire(context.Background(), capNodeReq(holder.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(ctx, cur, &queued, "opencode", "", capacity.RoleWorker, nil, "")
	}()
	capWaitUntil(t, func() bool { return len(ac.all()) == 1 })
	ac.Advance(3 * time.Hour)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled acquire returned nil")
	}
	if got := m.capacityWaitRemaining(queued.ID, m.capacityWait()); got != time.Hour {
		t.Fatalf("remaining after 3h = %v, want 1h", got)
	}

	// Re-entry through the failover: it is handed the remaining hour only.
	var seen time.Duration
	reentered := make(chan error, 1)
	go func() {
		reentered <- m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, []string{"n-excluded"}, "")
	}()
	capWaitUntil(t, func() bool { return len(ac.all()) >= 2 })
	seen = m.capacityWaitRemaining(queued.ID, m.capacityWait())
	if seen != time.Hour {
		t.Fatalf("budget during re-entry = %v, want 1h", seen)
	}
	led.Release(holder.ID)
	if err := <-reentered; err != nil {
		t.Fatal(err)
	}

	// Spend the last hour: the next entry times out at once.
	m.addCapacityWaitSpent(queued.ID, time.Hour)
	if err := m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, nil, ""); !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("exhausted budget: want ErrWaitTimeout, got %v", err)
	}
}

// regressTickClock is a capacity.Clock whose poll ticks fire only when the
// test calls fire; every other After (the wait deadline) never fires.
type regressTickClock struct {
	mu   sync.Mutex
	now  time.Time
	tick []chan time.Time
}

func (c *regressTickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *regressTickClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	if d != time.Second {
		return ch
	}
	c.mu.Lock()
	c.tick = append(c.tick, ch)
	c.mu.Unlock()
	return ch
}

func (c *regressTickClock) fire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.tick {
		ch <- c.now
	}
	c.tick = nil
}

// (d2): a change of wait reason text while queued does not move the anchor:
// the spent time runs from entering acquireLease, not from the latest reason.
func TestRegress_ReasonChangeDoesNotMoveBudgetAnchor(t *testing.T) {
	m, _, _ := capFixtureFakeClock(t, 1)
	clk := &regressTickClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: clk})
	led.SetLimit("node:n1", 1)
	led.SetLimit("node:n2", 1)
	// The placement is re-resolved on every poll tick, so switching the node
	// between ticks changes the ledger's reason text with no release.
	var pmu sync.Mutex
	node := "n1"
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		pmu.Lock()
		defer pmu.Unlock()
		return []string{"node:" + node}, node
	})
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	prd := newPRDWithTasks(t, m, 3)
	cur, _ := m.Store().GetPRD(prd.ID)
	h1, h2, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1], cur.Story[0].Tasks[2]
	for h, n := range map[string]string{h1.ID: "n1", h2.ID: "n2"} {
		if err := led.Acquire(context.Background(), capacity.Request{Holder: h, PRDID: prd.ID, Pools: []string{"node:" + n}, Node: n}, 0, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, nil, "")
	}()
	var first string
	capWaitUntil(t, func() bool {
		snap, ok := m.Store().TaskSnapshot(queued.ID)
		first = snap.WaitReason
		return ok && snap.Status == TaskWaitingCapacity && first != ""
	})
	ac.Advance(10 * time.Minute)
	pmu.Lock()
	node = "n2"
	pmu.Unlock()
	capWaitUntil(t, func() bool {
		clk.fire() // the tick channel may not be registered yet; fire until the reason moves
		snap, _ := m.Store().TaskSnapshot(queued.ID)
		return snap.WaitReason != "" && snap.WaitReason != first
	})
	ac.Advance(20 * time.Minute)
	led.Release(h2.ID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := m.capacityWaitRemaining(queued.ID, 100*time.Minute); got != 70*time.Minute {
		t.Fatalf("remaining = %v, want 70m (30m spent despite the reason change)", got)
	}
}

// (d3): once an acquire succeeds the executor ends the admission and the
// spent-wait record is cleared, so the next lease gets the full budget. The
// whole path runs through the executor: queue 20m, get admitted, finish.
func TestRegress_SpentWaitClearedAfterSuccessfulAcquire(t *testing.T) {
	m, api, led := capFixtureFakeClock(t, 1)
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	if err := led.Acquire(context.Background(), capNodeReq(holder.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Direct acquire: the queue time is charged, and stays until the
	// admission is ended.
	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, nil, "")
	}()
	capWaitUntil(t, func() bool { return len(ac.all()) == 1 })
	ac.Advance(20 * time.Minute)
	led.Release(holder.ID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	bound := m.capacityWait()
	if got := m.capacityWaitRemaining(queued.ID, bound); got != bound-20*time.Minute {
		t.Fatalf("remaining after wait = %v, want %v", got, bound-20*time.Minute)
	}
	led.Release(queued.ID)

	// Through the executor: the wait is charged, then cleared on admission.
	one := newPRDOnBackend(t, m, "opencode", "")
	cur1, _ := m.Store().GetPRD(one.ID)
	task := cur1.Story[0].Tasks[0]
	if err := led.Acquire(context.Background(), capNodeReq("blocker", one.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) { return SpawnResult{SessionID: "s"}, nil }
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	fin := make(chan *PRD, 1)
	go func() { fin <- runToTerminal(t, m, api, one.ID, spawn, verify) }()
	capWaitUntil(t, func() bool {
		snap, ok := m.Store().TaskSnapshot(task.ID)
		return ok && snap.Status == TaskWaitingCapacity
	})
	ac.Advance(25 * time.Minute)
	led.Release("blocker")
	if got := <-fin; got.Status != PRDCompleted {
		t.Fatalf("PRD ended %s, want completed", got.Status)
	}
	if got := m.capacityWaitRemaining(task.ID, bound); got != bound {
		t.Fatalf("next lease budget = %v, want the full %v (spent-wait record survived the acquire)", got, bound)
	}
}
