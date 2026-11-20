// Verifying-state capacity: a task that enters verifying releases its worker
// lease, so the verifier waits for and holds only its own verify:<task> slot.
// These tests drive a real capacity.Ledger with fake pools (llm:pool, node:n1,
// node-verify:n1) through the Manager's release, judge admission and relaunch
// paths.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -count=1 -run 'VerifyingLease|VerifierSlot|VerifyingRetry'
package autonomous

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// verifyLeaseVerifierBackend is the backend whose pools are node-only, so its
// verifier takes the node's verifier slot and never an llm or worker slot.
const verifyLeaseVerifierBackend = "pi-prompt"

// verifyLeaseFixture wires a Manager to a real ledger. The worker backend
// needs llm:pool and node:n1 (both limit 1). The verifier backend needs only
// node:n1, which the ledger swaps for node-verify:n1 when verifierSlots > 0.
func verifyLeaseFixture(t *testing.T, verifierSlots int) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("llm:pool", 1)
	led.SetLimit("node:n1", 1)
	if verifierSlots > 0 {
		led.SetLimit(capacity.VerifierPool("n1"), verifierSlots)
	}
	m.SetCapacity(led, func(backend, _ string) ([]string, string) {
		if backend == verifyLeaseVerifierBackend {
			return []string{"node:n1"}, "n1"
		}
		return []string{"llm:pool", "node:n1"}, "n1"
	})
	return m, led
}

// verifyLeaseWorker admits a worker lease for taskID on the worker pools. It
// does not call t, so it is safe in a goroutine.
func verifyLeaseWorker(led *capacity.Ledger, taskID string, wait time.Duration) error {
	return led.Acquire(context.Background(), capacity.Request{
		Holder: taskID, PRDID: "prd-" + taskID, Pools: []string{"llm:pool", "node:n1"}, Node: "n1", Role: capacity.RoleWorker,
	}, wait, nil, nil)
}

// verifyLeaseEnterVerifying is the task entering verifying: its worker session
// has ended, so the worker lease is released.
func verifyLeaseEnterVerifying(m *Manager, taskID string) {
	m.releaseWorkerLeaseForVerify(&Task{ID: taskID})
}

// verifyLeaseAdmitVerifier admits the verifier of taskID through AdmitJudge, as
// the verification wiring does. It does not call t, so it is safe in a goroutine.
func verifyLeaseAdmitVerifier(m *Manager, taskID string, wait time.Duration) (func(), error) {
	_, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder(taskID), WorkerHolder: taskID, PRDID: "prd-" + taskID,
		Backend: verifyLeaseVerifierBackend, Wait: wait,
	})
	return release, err
}

// verifyLeasePoolHeld reports the Held count of one pool in a snapshot.
func verifyLeasePoolHeld(st capacity.Status, pool string) int {
	for _, p := range st.Pools {
		if p.Name == pool {
			return p.Held
		}
	}
	return 0
}

// verifyLeaseWaiting reports whether holder waits in the ledger queue.
func verifyLeaseWaiting(led *capacity.Ledger, holder string) bool {
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			return true
		}
	}
	return false
}

// verifyLeaseWaitFor polls cond for up to five seconds.
func verifyLeaseWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestVerifyingLeaseHoldsNoWorkerLease: once a task is verifying and its
// verifier is admitted, the task holds no lease of its own, the llm and worker
// node pools are free, and the verifier holds only verify:<task> on the node's
// verifier pool.
func TestVerifyingLeaseHoldsNoWorkerLease(t *testing.T) {
	m, led := verifyLeaseFixture(t, 1)
	if err := verifyLeaseWorker(led, "task-a", time.Second); err != nil {
		t.Fatalf("worker admit: %v", err)
	}
	verifyLeaseEnterVerifying(m, "task-a")
	release, err := verifyLeaseAdmitVerifier(m, "task-a", time.Second)
	if err != nil {
		t.Fatalf("verifier admit: %v", err)
	}
	defer release()

	if ls := led.LeaseOf("task-a"); ls != nil {
		t.Fatalf("task still holds worker lease while verifying: %+v", ls)
	}
	st := led.Snapshot()
	if n := verifyLeasePoolHeld(st, "llm:pool"); n != 0 {
		t.Fatalf("llm:pool Held = %d while verifying, want 0", n)
	}
	if n := verifyLeasePoolHeld(st, "node:n1"); n != 0 {
		t.Fatalf("node:n1 Held = %d while verifying, want 0", n)
	}
	if len(st.Leases) != 1 {
		t.Fatalf("want exactly the verifier lease, got %+v", st.Leases)
	}
	ls := led.LeaseOf(VerifyHolder("task-a"))
	if ls == nil {
		t.Fatal("verifier holds no verify:task-a lease")
	}
	if !slices.Equal(ls.Pools, []string{capacity.VerifierPool("n1")}) {
		t.Fatalf("verifier pools = %v, want only the node verifier pool", ls.Pools)
	}
}

// TestVerifyingLeaseFreesLaneForSecondTask: with llm:pool limited to one, a
// second task's worker is admitted while the first task is still verifying.
func TestVerifyingLeaseFreesLaneForSecondTask(t *testing.T) {
	m, led := verifyLeaseFixture(t, 1)
	if err := verifyLeaseWorker(led, "task-a", time.Second); err != nil {
		t.Fatalf("first worker admit: %v", err)
	}
	verifyLeaseEnterVerifying(m, "task-a")
	release, err := verifyLeaseAdmitVerifier(m, "task-a", time.Second)
	if err != nil {
		t.Fatalf("first verifier admit: %v", err)
	}
	defer release()

	if err := verifyLeaseWorker(led, "task-b", 2*time.Second); err != nil {
		t.Fatalf("second task not admitted while first verifies: %v", err)
	}
	if led.LeaseOf("task-b") == nil {
		t.Fatal("second task holds no worker lease")
	}
	if !led.Held(VerifyHolder("task-a")) {
		t.Fatal("first task's verifier lease disappeared")
	}
}

// TestVerifyingRetryReAdmitted: a task back to running for a retry is admitted
// again through Acquire. It queues while the llm lane is full, and the next
// relaunch frees its lease through releaseForRelaunch.
func TestVerifyingRetryReAdmitted(t *testing.T) {
	m, led := verifyLeaseFixture(t, 1)
	if err := verifyLeaseWorker(led, "task-a", time.Second); err != nil {
		t.Fatalf("first worker admit: %v", err)
	}
	verifyLeaseEnterVerifying(m, "task-a")
	releaseVerifier, err := verifyLeaseAdmitVerifier(m, "task-a", time.Second)
	if err != nil {
		t.Fatalf("verifier admit: %v", err)
	}
	releaseVerifier() // verification failed; the task goes back to running

	// Another task fills the llm lane, so the retry has to queue.
	if err := verifyLeaseWorker(led, "task-b", time.Second); err != nil {
		t.Fatalf("lane filler admit: %v", err)
	}
	retry := make(chan error, 1)
	go func() { retry <- verifyLeaseWorker(led, "task-a", 5*time.Second) }()
	verifyLeaseWaitFor(t, "retry queued behind a full lane", func() bool {
		return verifyLeaseWaiting(led, "task-a")
	})
	if led.Held("task-a") {
		t.Fatal("retry admitted while the llm lane was full")
	}

	led.Release("task-b")
	select {
	case err := <-retry:
		if err != nil {
			t.Fatalf("retry admit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retry not admitted after the lane was freed")
	}
	if led.LeaseOf("task-a") == nil {
		t.Fatal("retried task holds no worker lease")
	}

	if prev := m.releaseForRelaunch(&Task{ID: "task-a"}); prev != "n1" {
		t.Fatalf("releaseForRelaunch node = %q, want n1", prev)
	}
	if led.Held("task-a") || led.Held(VerifyHolder("task-a")) {
		t.Fatal("releaseForRelaunch left a lease held for the retried task")
	}
}

// TestVerifierSlotCountRespected: with one node verifier slot, a second task's
// verifier waits in the ledger queue on the verifier pool without taking a
// worker slot, and is admitted once the first verifier releases.
func TestVerifierSlotCountRespected(t *testing.T) {
	m, led := verifyLeaseFixture(t, 1)
	if err := verifyLeaseWorker(led, "task-a", time.Second); err != nil {
		t.Fatalf("worker a admit: %v", err)
	}
	verifyLeaseEnterVerifying(m, "task-a")
	releaseA, err := verifyLeaseAdmitVerifier(m, "task-a", time.Second)
	if err != nil {
		t.Fatalf("verifier a admit: %v", err)
	}

	if err := verifyLeaseWorker(led, "task-b", time.Second); err != nil {
		t.Fatalf("worker b admit: %v", err)
	}
	verifyLeaseEnterVerifying(m, "task-b")

	type admitted struct {
		release func()
		err     error
	}
	second := make(chan admitted, 1)
	go func() {
		rel, err := verifyLeaseAdmitVerifier(m, "task-b", 5*time.Second)
		second <- admitted{rel, err}
	}()
	verifyLeaseWaitFor(t, "second verifier queued on the verifier slot", func() bool {
		return verifyLeaseWaiting(led, VerifyHolder("task-b"))
	})

	st := led.Snapshot()
	if led.Held(VerifyHolder("task-b")) {
		t.Fatal("second verifier admitted while the only verifier slot is taken")
	}
	if n := verifyLeasePoolHeld(st, "node:n1"); n != 0 {
		t.Fatalf("worker slot node:n1 Held = %d while verifiers wait, want 0", n)
	}
	if n := verifyLeasePoolHeld(st, "llm:pool"); n != 0 {
		t.Fatalf("llm:pool Held = %d while verifiers wait, want 0", n)
	}
	releaseA()
	select {
	case r := <-second:
		if r.err != nil {
			t.Fatalf("second verifier admit: %v", r.err)
		}
		defer r.release()
	case <-time.After(5 * time.Second):
		t.Fatal("second verifier not admitted after the first released")
	}
	ls := led.LeaseOf(VerifyHolder("task-b"))
	if ls == nil || !slices.Equal(ls.Pools, []string{capacity.VerifierPool("n1")}) {
		t.Fatalf("second verifier lease = %+v, want only the node verifier pool", ls)
	}
}
