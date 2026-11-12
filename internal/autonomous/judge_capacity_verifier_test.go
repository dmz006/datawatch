package autonomous

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// workerOnFast admits one worker task whose lease lands on node "fast" and
// returns the task, so the tests can check what happened to that exact worker
// lease.
func workerOnFast(t *testing.T, m *Manager, led *capacity.Ledger) (string, string) {
	t.Helper()
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	if err := m.admit(context.Background(), cur, &task, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "fast" {
		t.Fatalf("worker lease = %+v, want node fast", ls)
	}
	return prd.ID, task.ID
}

// TestVerifierPoolRunsWhileExecutorHoldsNode: with a verifier pool on fast,
// a verifier is admitted while an executor still holds every executor slot on
// node:slow — it takes node-verify:fast (fast, its own executor pool free,
// wins least-loaded-first), not any executor pool.
func TestVerifierPoolRunsWhileExecutorHoldsNode(t *testing.T) {
	m, led := poolFixture(t, true)
	led.SetLimit(capacity.VerifierPool("fast"), 1)
	// An executor holds the only node:slow slot (limit 1).
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "exec-1", Pools: []string{"host", "llm:pool", "node:slow"}, Node: "slow", Role: capacity.RoleWorker,
	}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	_, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder("task-a"), PRDID: "prd", Backend: "pi", Model: "ollama/q", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if !led.Held("exec-1") {
		t.Fatal("executor must still hold node:slow while the verifier runs")
	}
	ls := led.LeaseOf(VerifyHolder("task-a"))
	if ls == nil || ls.Node == "" {
		t.Fatalf("verifier lease = %+v, want a candidate node", ls)
	}
	if !slices.Contains(ls.Pools, capacity.VerifierPool(ls.Node)) {
		t.Fatalf("verifier lease = %+v, want the %s pool it was placed on", ls, capacity.VerifierPool(ls.Node))
	}
	if exec := "node:" + ls.Node; slices.Contains(ls.Pools, exec) {
		t.Fatalf("verifier took the executor pool %s", exec)
	}
}

// TestVerifierPoolReleasesWorkerLeaseNotHandingOver: the worker's lease is on
// a node with a verifier pool, so the hand-over is skipped: the worker lease
// is released (not re-keyed) and the verifier takes its own lease on
// node-verify:fast, freeing the executor slot.
func TestVerifierPoolReleasesWorkerLeaseNotHandingOver(t *testing.T) {
	m, led := poolFixture(t, true)
	led.SetLimit(capacity.VerifierPool("fast"), 2)
	prdID, taskID := workerOnFast(t, m, led)

	node, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder(taskID), WorkerHolder: taskID, PRDID: prdID,
		Backend: "pi", Model: "ollama/q", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "fast" {
		t.Fatalf("verifier node = %q, want fast", node)
	}
	if led.LeaseOf(taskID) != nil {
		t.Fatal("worker lease still held: it must be released, not handed over")
	}
	ls := led.LeaseOf(VerifyHolder(taskID))
	if ls == nil || !slices.Contains(ls.Pools, capacity.VerifierPool("fast")) {
		t.Fatalf("verifier lease = %+v, want the %s pool (a hand-over would keep the worker's node:fast)", ls, capacity.VerifierPool("fast"))
	}
	// The worker's executor slot is free again.
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "exec-2", Pools: []string{"host", "llm:pool", "node:fast"}, Node: "fast", Role: capacity.RoleWorker,
	}, 0, nil, nil); err != nil {
		t.Fatalf("executor slot on fast not freed for the verifier pool: %v", err)
	}
}

// TestNoVerifierPoolKeepsHandOver: a node with no verifier pool (limit 0, the
// pool-off meaning) keeps today's hand-over exactly: the worker lease is
// re-keyed as is and the verifier keeps the worker's node:fast pool.
func TestNoVerifierPoolKeepsHandOver(t *testing.T) {
	m, led := poolFixture(t, true) // no verifier pool set on any node
	prdID, taskID := workerOnFast(t, m, led)

	node, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder(taskID), WorkerHolder: taskID, PRDID: prdID,
		Backend: "pi", Model: "ollama/q", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "fast" {
		t.Fatalf("verifier node = %q, want the worker's node fast", node)
	}
	if led.LeaseOf(taskID) != nil {
		t.Fatal("worker lease must be gone after the hand-over")
	}
	ls := led.LeaseOf(VerifyHolder(taskID))
	if ls == nil || !slices.Contains(ls.Pools, "node:fast") {
		t.Fatalf("handed worker lease = %+v, want the worker's node:fast pool kept as is", ls)
	}
}

// TestJudgeFitsAgreesWithAdmitJudge: with the verifier pool in play,
// JudgeFits reports the same room as AdmitJudge for the same request — room
// in the pool admits both, a full pool blocks both, and the release unblocks
// both.
func TestJudgeFitsAgreesWithAdmitJudge(t *testing.T) {
	m, led := poolFixture(t, true)
	led.SetLimit(capacity.VerifierPool("fast"), 1)
	prdID, taskID := workerOnFast(t, m, led)
	j := JudgeAdmit{
		Holder: VerifyHolder(taskID), WorkerHolder: taskID, PRDID: prdID,
		Backend: "pi", Model: "ollama/q", Wait: 100 * time.Millisecond,
	}

	// Room in the verifier pool: fits, and admits for real on fast.
	if !m.JudgeFits(j) {
		t.Fatal("JudgeFits false with a free verifier slot on fast")
	}
	node, release, err := m.AdmitJudge(context.Background(), j)
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	if node != "fast" {
		t.Fatalf("verifier node = %q, want fast (empty, higher priority)", node)
	}
	release()

	// Fill both placements the next verifier could take: fast's one verifier
	// slot (only a verifier can hold node-verify:fast, so block it with one —
	// not an executor, whose pool node:fast is separate) and slow's only slot
	// (slow has no verifier pool, so a verifier there uses node:slow).
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "verify-fill", Pools: []string{"node:fast"}, Node: "fast", Role: capacity.RoleVerifier,
	}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	defer led.Release("verify-fill")
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "exec-slow", Pools: []string{"node:slow"}, Node: "slow", Role: capacity.RoleWorker,
	}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	defer led.Release("exec-slow")

	// Beyond the cap: both must say no.
	if m.JudgeFits(j) {
		t.Fatal("JudgeFits true beyond the verifier cap")
	}
	if _, _, err := m.AdmitJudge(context.Background(), j); err == nil {
		t.Fatal("AdmitJudge admitted beyond the verifier cap")
	}

	// The release unblocks both: room again, and admitted on fast's verifier
	// pool (fast is least loaded and its verifier slot is free).
	led.Release("verify-fill")
	if !m.JudgeFits(j) {
		t.Fatal("JudgeFits still false after the release")
	}
	node, release, err = m.AdmitJudge(context.Background(), j)
	if err != nil {
		t.Fatalf("AdmitJudge after the release: %v", err)
	}
	if node != "fast" {
		t.Fatalf("verifier node = %q, want fast after the release", node)
	}
	if ls := led.LeaseOf(VerifyHolder(taskID)); ls == nil || !slices.Contains(ls.Pools, capacity.VerifierPool("fast")) {
		t.Fatalf("verifier lease = %+v, want the %s pool", ls, capacity.VerifierPool("fast"))
	}
	release()
}

// TestVerifierBeyondCapWaitsUntilRelease: a verifier past the verifier pool's
// cap is not admitted while the slot is taken, and is admitted at once after
// the release — the pool limit binds, independent of the executor pool.
func TestVerifierBeyondCapWaitsUntilRelease(t *testing.T) {
	m, led := poolFixture(t, true)
	led.SetLimit(capacity.VerifierPool("fast"), 1)
	vpool := capacity.VerifierPool("fast")

	// A verifier lease takes fast's one verifier slot, and an executor takes
	// slow's only slot (slow has no verifier pool, so a verifier there would
	// use node:slow), so no candidate is left.
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "verify-held", Pools: []string{"node:fast"}, Node: "fast", Role: capacity.RoleVerifier,
	}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	defer led.Release("verify-held")
	// ...and an executor takes slow's only slot.
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "exec-slow", Pools: []string{"node:slow"}, Node: "slow", Role: capacity.RoleWorker,
	}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	j := JudgeAdmit{Holder: VerifyHolder("task-wait"), PRDID: "prd", Backend: "pi", Model: "ollama/q", Wait: 100 * time.Millisecond}
	if _, _, err := m.AdmitJudge(context.Background(), j); err == nil {
		t.Fatal("verifier admitted beyond the cap")
	}
	if m.JudgeFits(j) {
		t.Fatal("JudgeFits true beyond the cap")
	}

	// The release makes room: admitted at once on fast (least loaded, then
	// priority), taking fast's verifier pool.
	led.Release("verify-held")
	node, release, err := m.AdmitJudge(context.Background(), j)
	if err != nil {
		t.Fatalf("AdmitJudge after the release: %v", err)
	}
	defer release()
	if node != "fast" {
		t.Fatalf("verifier node = %q, want fast (its slot freed)", node)
	}
	if ls := led.LeaseOf(VerifyHolder("task-wait")); ls == nil || !slices.Contains(ls.Pools, vpool) {
		t.Fatalf("verifier lease = %+v, want the %s pool", ls, vpool)
	}
}
