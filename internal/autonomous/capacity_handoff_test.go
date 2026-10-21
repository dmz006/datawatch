package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// handoffState captures what the verify closure observed while it ran in the
// executor's goroutine (t.Fatal is unsafe there, so assertions happen after
// the run completes).
type handoffState struct {
	mu       sync.Mutex
	verifyN  int
	handoffs int // verify phases whose worker lease was handed off
}

func (h *handoffState) verifyCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.verifyN
}

func (h *handoffState) handoffCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handoffs
}

// mimicVerifyWiring mirrors cmd/datawatch's verifier admission wiring for a
// given backend: hand the task's worker lease to "verify:<taskID>"; on
// refusal (no worker lease, different/zero pools), release the worker lease
// first and admit the verifier separately. Never blocks.
func mimicVerifyWiring(t *testing.T, m *Manager, led *capacity.Ledger, prdID, taskID, backend string, h *handoffState) (func(), int, bool) {
	t.Helper()
	handed := false
	if m.HandoffVerifyLease(taskID, backend, "") {
		handed = true
	} else {
		led.Release(taskID) // fallback: release the worker lease first — no stacking, no self-deadlock
	}
	if !led.Held(VerifyHolder(taskID)) {
		keys := m.CapacityKeys()
		pools, node := keys(backend, "")
		if len(pools) == 0 {
			t.Fatalf("test wiring: backend %q resolves to no pools", backend)
		}
		if err := led.Acquire(context.Background(), capacity.Request{Holder: VerifyHolder(taskID), PRDID: prdID, Pools: pools, Node: node, Role: capacity.RoleVerifier}, 0, nil, nil); err != nil {
			t.Fatalf("test wiring: verifier acquire: %v", err)
		}
	}
	if h != nil {
		h.mu.Lock()
		h.verifyN++
		if handed {
			h.handoffs++
		}
		h.mu.Unlock()
	}
	st := led.Snapshot()
	if !led.Held(VerifyHolder(taskID)) {
		t.Fatalf("test wiring: verify lease not held (state=%+v)", st)
	}
	return func() { led.Release(VerifyHolder(taskID)) }, len(st.Leases), handed
}

// createHandoffPRD creates a real-directory PRD (B165: a fake project dir
// lets a concurrent boot-resume scan park the PRD) with one story and one
// task, on the given backend.
func createHandoffPRD(t *testing.T, m *Manager, backend, model string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("capacity handoff spec", t.TempDir(), backend, model, EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "make the handoff tests"}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	got.MaxConcurrentTasks = 1 // 0 (zero value) blocks all dispatch; existing helpers set this
	_ = m.Store().SavePRD(got)
	return got
}

func handoffTwoPoolFixture(t *testing.T) (*Manager, *API, *capacity.Ledger) {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:n1", 1)
	led.SetLimit("node:n2", 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if backend == "verifierX" {
			return []string{"node:n2"}, "n2"
		}
		return []string{"node:n1"}, "n1"
	})
	return m, api, led
}

// TestManager_HandoffVerifyLease_RekeysWorkerLease is the unit-level check:
// the worker lease becomes the verifier lease, the worker name is freed, the
// task holds exactly one lease for the same pool, and a double hand-off is
// refused without disturbing the verifier lease.
func TestManager_HandoffVerifyLease_RekeysWorkerLease(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDOnBackend(t, m, "opencode", "")
	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]

	// Simulate the worker's admitted lease (holder = task id, node:n1 pool).
	if err := led.Acquire(context.Background(), capacity.Request{Holder: tk.ID, PRDID: prd.ID, Pools: []string{"node:n1"}, Node: "n1", Role: capacity.RoleWorker}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	if !m.HandoffVerifyLease(tk.ID, "opencode", "") {
		t.Fatal("same-pool verify lease must be handed off")
	}
	if led.Held(tk.ID) {
		t.Fatal("worker lease still held after hand-off")
	}
	if !led.Held(VerifyHolder(tk.ID)) {
		t.Fatal("verifier lease not held after hand-off")
	}
	if st := led.Snapshot(); len(st.Leases) != 1 {
		t.Fatalf("task must hold exactly one lease after hand-off, got %d: %+v", len(st.Leases), st.Leases)
	}
	if m.HandoffVerifyLease(tk.ID, "opencode", "") {
		t.Fatal("double hand-off must be refused")
	}
	if !led.Held(VerifyHolder(tk.ID)) {
		t.Fatal("refused double hand-off must not disturb the verifier lease")
	}
}

// TestManager_HandoffVerifyLease_DifferentPoolsRefused: a verifier backend
// that resolves to a different pool must NOT receive the worker's slot; the
// worker lease survives for the fallback (release-then-acquire) path, which
// also ends holding exactly one lease.
func TestManager_HandoffVerifyLease_DifferentPoolsRefused(t *testing.T) {
	m, _, led := handoffTwoPoolFixture(t)
	prd := newPRDOnBackend(t, m, "opencode", "") // worker → node:n1
	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]

	if err := led.Acquire(context.Background(), capacity.Request{Holder: tk.ID, PRDID: prd.ID, Pools: []string{"node:n1"}, Node: "n1"}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.HandoffVerifyLease(tk.ID, "verifierX", "") {
		t.Fatal("hand-off must be refused when the verifier resolves to a different pool")
	}
	if !led.Held(tk.ID) {
		t.Fatal("worker lease must survive a refused hand-off")
	}
	if led.Held(VerifyHolder(tk.ID)) {
		t.Fatal("refused hand-off must not create the verifier lease")
	}

	// Fallback path: one lease at a time, ended in exactly one.
	led.Release(tk.ID)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: VerifyHolder(tk.ID), PRDID: prd.ID, Pools: []string{"node:n2"}, Node: "n2", Role: capacity.RoleVerifier}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if st := led.Snapshot(); len(st.Leases) != 1 {
		t.Fatalf("fallback must hold exactly one lease, got %d: %+v", len(st.Leases), st.Leases)
	}
}

// TestManager_HandoffVerifyLease_NoWorkerLeaseRefused: nothing to hand off —
// refused, and no leases materialized.
func TestManager_HandoffVerifyLease_NoWorkerLeaseRefused(t *testing.T) {
	m, _, led := capFixture(t, 1)
	if m.HandoffVerifyLease("no-such-task", "opencode", "") {
		t.Fatal("hand-off without a worker lease must be refused")
	}
	if m.HandoffVerifyLease("", "opencode", "") {
		t.Fatal("empty task id must be refused")
	}
	if n := len(led.Snapshot().Leases); n != 0 {
		t.Fatalf("refused hand-off must not create leases, got %d", n)
	}

	m2, _, _, _ := apiFixture(t)
	if m2.HandoffVerifyLease("any-task", "opencode", "") {
		t.Fatal("hand-off must be refused when capacity is not wired")
	}
}

// TestExecutor_TaskInVerifyHoldsExactlyOneLease is the end-to-end property:
// while the worker session is idle and the verifier runs, the task holds
// exactly one capacity lease — its worker slot re-keyed to
// "verify:<taskID>" — and nothing is left holding after the run.
func TestExecutor_TaskInVerifyHoldsExactlyOneLease(t *testing.T) {
	m, api, led := capFixture(t, 1)
	prd := createHandoffPRD(t, m, "opencode", "")
	h := &handoffState{}
	leaks := 0
	verify := func(ctx context.Context, p *PRD, tk *Task) (VerificationResult, error) {
		release, leases, _ := mimicVerifyWiring(t, m, led, p.ID, tk.ID, "opencode", h)
		defer release()
		if leases != 1 {
			leaks++
		}
		return VerificationResult{OK: true}, nil
	}
	got := runToTerminalOrBlocked(t, m, api, prd.ID, fakeSpawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD = %s, want completed (lease leak? error?)", got.Status)
	}
	if h.verifyCalls() != 1 {
		t.Fatalf("verify called %d times, want 1", h.verifyCalls())
	}
	if h.handoffCount() != 1 {
		t.Fatalf("worker lease handoff happened %d times, want 1", h.handoffCount())
	}
	if leaks != 0 {
		t.Fatalf("verify phase held a lease count other than 1 in %d of %d call(s)", leaks, h.verifyCalls())
	}
	if st := led.Snapshot(); len(st.Leases) != 0 || len(st.Waiting) != 0 {
		t.Fatalf("leaked capacity state: leases=%+v waiting=%+v", st.Leases, st.Waiting)
	}
}

// TestExecutor_RetryReleasesAndReAdmitsAfterVerifyFailure: a failing
// verification (retryable, no error → auto-fix retry) must not double-count
// the task: every verify phase holds exactly one lease, and nothing is
// leaked after the retry lands.
func TestExecutor_RetryReleasesAndReAdmitsAfterVerifyFailure(t *testing.T) {
	m, api, led := capFixture(t, 1)
	prd := createHandoffPRD(t, m, "opencode", "")
	h := &handoffState{}
	leaks := 0
	verify := func(ctx context.Context, p *PRD, tk *Task) (VerificationResult, error) {
		release, leases, _ := mimicVerifyWiring(t, m, led, p.ID, tk.ID, "opencode", h)
		defer release()
		if leases != 1 {
			leaks++
		}
		// Retryable failure: OK=false WITHOUT an error (a plain error
		// aborts the PRD instead of auto-fix retrying — different path).
		if h.verifyCalls() == 1 {
			return VerificationResult{OK: false, Summary: "expected first-verify failure", VerifiedAt: time.Now()}, nil
		}
		return VerificationResult{OK: true}, nil
	}
	got := runToTerminalOrBlocked(t, m, api, prd.ID, fakeSpawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD = %s, want completed via auto-fix retry", got.Status)
	}
	if h.verifyCalls() != 2 {
		t.Fatalf("verify called %d times, want 2 (retry must re-spawn and re-verify)", h.verifyCalls())
	}
	if leaks != 0 {
		t.Fatalf("a verify phase held a lease count other than 1 in %d of %d call(s) — double count detected", leaks, h.verifyCalls())
	}
	if st := led.Snapshot(); len(st.Leases) != 0 || len(st.Waiting) != 0 {
		t.Fatalf("leaked capacity state after retry: leases=%+v waiting=%+v", st.Leases, st.Waiting)
	}
}

// TestExecutor_DifferentPoolFallbackNoStacking: a verifier backend resolving
// to a different pool than the worker's must fall back to
// release-then-acquire — the task never holds two slots at once.
func TestExecutor_DifferentPoolFallbackNoStacking(t *testing.T) {
	m, api, led := handoffTwoPoolFixture(t)
	prd := createHandoffPRD(t, m, "opencode", "") // worker backend → node:n1
	h := &handoffState{}
	leaks := 0
	verify := func(ctx context.Context, p *PRD, tk *Task) (VerificationResult, error) {
		release, leases, _ := mimicVerifyWiring(t, m, led, p.ID, tk.ID, "verifierX", h) // → node:n2
		defer release()
		if leases != 1 {
			leaks++
		}
		return VerificationResult{OK: true}, nil
	}
	got := runToTerminalOrBlocked(t, m, api, prd.ID, fakeSpawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD = %s, want completed", got.Status)
	}
	if h.handoffCount() != 0 {
		t.Fatalf("no hand-off should occur across pools, got %d", h.handoffCount())
	}
	if h.verifyCalls() != 1 {
		t.Fatalf("verify called %d times, want 1", h.verifyCalls())
	}
	if leaks != 0 {
		t.Fatalf("verify phase held a lease count other than 1 in %d of %d call(s) — stacking detected", leaks, h.verifyCalls())
	}
	if st := led.Snapshot(); len(st.Leases) != 0 {
		t.Fatalf("leaked leases after run: %+v", st.Leases)
	}
}
