package main

import (
	"context"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// judgeLedgerFixture is the capacity fixture for the judge admission tests:
// a real in-memory ledger with one "host" backend pool and one "node:alpha"
// engine pool (each limit 1), and a keys function mapping every backend but
// "nopools" to both pools — the no-pools path.
func judgeLedgerFixture(t *testing.T) (*capacity.Ledger, func(backend, model string) ([]string, string)) {
	t.Helper()
	led := capacity.New(capacity.Options{})
	led.SetLimit("host", 1)
	led.SetLimit("node:alpha", 1)
	keys := func(backend, model string) ([]string, string) {
		if backend == "nopools" {
			return nil, ""
		}
		return []string{"host", "node:alpha"}, "alpha"
	}
	return led, keys
}

// judgeAdmitFixture wires judgeAdmitters over the fixture (blocking wait of
// 2s) and returns the admit/try/hook closures plus the ledger.
func judgeAdmitFixture(t *testing.T) (
	admit func(ctx context.Context, backend, model, unitID, prdID string) (func(), error),
	try func(ctx context.Context, backend, model, unitID, prdID string) (func(), bool),
	started func(sessionName, sessionID string),
	led *capacity.Ledger,
) {
	t.Helper()
	led, keys := judgeLedgerFixture(t)
	admit, try, started = judgeAdmitters(led, newVerifierRegistry(), keys, 2*time.Second)
	return
}

// judgePoolStatus returns the live pool status for name, failing the test if
// the pool is absent.
func judgePoolStatus(t *testing.T, led *capacity.Ledger, name string) capacity.PoolStatus {
	t.Helper()
	for _, p := range led.Snapshot().Pools {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("pool %q not in snapshot %v", name, led.Snapshot().Pools)
	return capacity.PoolStatus{}
}

func TestJudgeAdmitRequestShape(t *testing.T) {
	r := judgeAdmitRequest("u1", "p1", []string{"host", "node:alpha"}, "alpha", "qwen", 3)
	if r.Holder != autonomouspkg.JudgeHolder("u1") {
		t.Errorf("Holder = %q, want %q", r.Holder, "judge:u1")
	}
	if r.PRDID != "p1" {
		t.Errorf("PRDID = %q, want the unit PRD %q", r.PRDID, "p1")
	}
	if r.Role != capacity.RoleVerifier {
		t.Errorf("Role = %q, want %q", r.Role, capacity.RoleVerifier)
	}
	if len(r.Pools) != 2 || r.Pools[0] != "host" || r.Pools[1] != "node:alpha" {
		t.Errorf("Pools = %v", r.Pools)
	}
	if r.Node != "alpha" || r.Model != "qwen" || r.Priority != 3 {
		t.Errorf("Node/Model/Priority = %q %q %d", r.Node, r.Model, r.Priority)
	}
}

func TestJudgeUnitFromSessionName(t *testing.T) {
	if got, ok := judgeUnitFromSessionName("autonomous:guardrail:story-7"); !ok || got != "story-7" {
		t.Errorf("guardrail name: got %q %v, want story-7 true", got, ok)
	}
	for _, name := range []string{
		"autonomous:guardrail",  // bare prefix, no unit id
		"autonomous:guardrail:", // bare prefix + colon
		"autonomous:guardrails:u1",
		"autonomous:verify:t1",
		"decompose:abc",
		"",
	} {
		if got, ok := judgeUnitFromSessionName(name); ok || got != "" {
			t.Errorf("%q: got %q %v, want empty false", name, got, ok)
		}
	}
}

func TestJudgeAdmitHoldsBackendAndNodePools(t *testing.T) {
	admit, _, _, led := judgeAdmitFixture(t)
	release, err := admit(context.Background(), "ollama", "qwen", "u1", "p1")
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("judge lease not recorded")
	}
	for _, pool := range []string{"host", "node:alpha"} {
		p := judgePoolStatus(t, led, pool)
		if p.Held != 1 {
			t.Errorf("%s Held = %d, want 1", pool, p.Held)
		}
		found := false
		for _, h := range p.Holders {
			if h == "judge:u1" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s holders %v missing judge:u1", pool, p.Holders)
		}
	}
	// The unit's one slot blocks a concurrent contender on either pool.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "contender", Pools: []string{"host"}}, 100*time.Millisecond, func() bool { return false }, func(string) {}); err == nil {
		t.Fatal("contender admitted while the judge holds the pool")
	}
	release()
	if led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("lease still held after release")
	}
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "contender", Pools: []string{"host"}}, 200*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("pool not freed after release: %v", err)
	}
	led.Release("contender")
}

func TestJudgeAdmitWaitsThenAcquiresWhenSlotFrees(t *testing.T) {
	admit, _, _, led := judgeAdmitFixture(t)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "peer", Pools: []string{"host", "node:alpha"}}, time.Second, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("peer acquire: %v", err)
	}
	type res struct {
		release func()
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		r, err := admit(context.Background(), "ollama", "qwen", "u1", "p1")
		ch <- res{r, err}
	}()
	time.Sleep(300 * time.Millisecond) // the judge must be waiting, not granted
	if led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("judge admitted while the peer held the slot")
	}
	led.Release("peer")
	r := <-ch
	if r.err != nil {
		t.Fatalf("judge admit after the slot freed: %v", r.err)
	}
	defer r.release()
	if !led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("judge did not hold the freed slot")
	}
}

func TestJudgeAdmitReleaseIdempotent(t *testing.T) {
	admit, _, _, led := judgeAdmitFixture(t)
	release, err := admit(context.Background(), "ollama", "qwen", "u1", "p1")
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	release()
	release() // the deferred release may fire twice: second call must be a no-op
	if led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("lease still held after double release")
	}
	// The slot was freed exactly once.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "contender", Pools: []string{"host", "node:alpha"}}, 200*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("double release lost the pool slot: %v", err)
	}
	led.Release("contender")
}

func TestJudgeAdmitNoPoolsBackendNoOp(t *testing.T) {
	admit, try, _, led := judgeAdmitFixture(t)
	release, err := admit(context.Background(), "nopools", "", "u1", "p1")
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	release()
	release()
	if led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("no-pools admit must not hold any lease")
	}
	rel, ok := try(context.Background(), "nopools", "", "u1", "p1")
	if !ok {
		t.Fatal("try must report available for a pool-less backend")
	}
	rel()
	rel()
	// Nothing was held, so a concurrent contender still gets the slot.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "contender", Pools: []string{"host"}}, 200*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("contender blocked by a no-pools judge: %v", err)
	}
	led.Release("contender")
}

func TestJudgeAdmitFailureIsInfraVerdict(t *testing.T) {
	admit, _, _, led := judgeAdmitFixture(t)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "peer", Pools: []string{"host"}}, time.Second, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("peer acquire: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release, err := admit(ctx, "ollama", "qwen", "u1", "p1")
	if err == nil {
		t.Fatal("admit must fail on a cancelled context while the pool is taken")
	}
	release() // the failed-admission release must be safe to call
	if led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("failed admission must not hold the lease")
	}
	v := judgeAdmitFailureVerdict(err)
	if v.Outcome != "block" {
		t.Errorf("Outcome = %q, want block", v.Outcome)
	}
	if v.Category != "infra" {
		t.Errorf("Category = %q, want infra (resource contention, not content)", v.Category)
	}
	if !strings.HasPrefix(v.Summary, "guardrail(capacity): ") {
		t.Errorf("Summary = %q, want the guardrail(capacity): prefix", v.Summary)
	}
	if v.JudgeSessionID != "" || v.JudgeOutputPath != "" {
		t.Errorf("no session started, got session %q path %q", v.JudgeSessionID, v.JudgeOutputPath)
	}
}

func TestJudgeTryAdmitProbesWithoutCommitting(t *testing.T) {
	_, try, _, led := judgeAdmitFixture(t)
	// Room available: the probe grants.
	rel, ok := try(context.Background(), "ollama", "qwen", "u1", "p1")
	if !ok {
		t.Fatal("try must succeed while the pool is free")
	}
	if !led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("probe did not hold the slot")
	}
	rel()
	rel() // idempotent

	// Pool taken: the probe declines and holds nothing.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "peer", Pools: []string{"host"}}, time.Second, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("peer acquire: %v", err)
	}
	rel, ok = try(context.Background(), "ollama", "qwen", "u2", "p1")
	if ok {
		t.Fatal("try must fail while the pool is taken")
	}
	if led.Held(autonomouspkg.JudgeHolder("u2")) {
		t.Fatal("failed probe must not hold the slot")
	}
	rel()
	led.Release("peer")
}

// TestJudgeAdmitReleasesOnPanic mirrors the guardrail closure: the release is
// deferred at admission time and fires when the judge body panics, so a
// crashing judge never leaks its slot.
func TestJudgeAdmitReleasesOnPanic(t *testing.T) {
	admit, _, _, led := judgeAdmitFixture(t)
	var release func()
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
			if release != nil {
				release()
			}
		}()
		var err error
		release, err = admit(context.Background(), "ollama", "qwen", "u1", "p1")
		if err != nil {
			t.Errorf("admit: %v", err)
		}
		if !led.Held(autonomouspkg.JudgeHolder("u1")) {
			t.Error("lease not held before the judge body")
		}
		panic("guardrail session exploded")
	}()
	if !panicked {
		t.Fatal("the simulated judge body must panic")
	}
	if led.Held(autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("lease leaked after a panicking guardrail session")
	}
	// And the slot is reusable immediately.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "contender", Pools: []string{"host", "node:alpha"}}, 200*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("slot not reusable after the panic: %v", err)
	}
	led.Release("contender")
}

func TestJudgeSessionStartedBindsJudgeLease(t *testing.T) {
	admit, _, started, led := judgeAdmitFixture(t)
	releaseU1, err := admit(context.Background(), "ollama", "qwen", "u1", "p1")
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	started("autonomous:guardrail:u1", "sess1")
	if ls := led.LeaseOf(autonomouspkg.JudgeHolder("u1")); ls == nil || ls.SessionID != "sess1" {
		t.Fatalf("judge lease not bound to its session, got %+v", ls)
	}
	// Re-binding the same judge session with a new id overwrites the old one.
	started("autonomous:guardrail:u1", "sess2")
	if ls := led.LeaseOf(autonomouspkg.JudgeHolder("u1")); ls == nil || ls.SessionID != "sess2" {
		t.Fatalf("re-bind failed, got %+v", ls)
	}
	releaseU1()

	// Names of other one-shot roles must not touch the judge's leases. With
	// the single slot free, the second unit admits alongside them.
	releaseU2, err := admit(context.Background(), "ollama", "qwen", "u2", "p1")
	if err != nil {
		t.Fatalf("admit u2: %v", err)
	}
	started("autonomous:verify:t9", "sessV")
	started("decompose:job-x", "sessD")
	started("autonomous:guardrail:", "sessEmpty") // bare prefix: no unit to bind
	if ls := led.LeaseOf(autonomouspkg.JudgeHolder("u2")); ls == nil || ls.SessionID != "" {
		t.Fatalf("non-judge session names must leave the judge lease unbound, got %+v", ls)
	}
	releaseU2()
}
