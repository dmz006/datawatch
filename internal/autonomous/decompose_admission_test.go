package autonomous

// Decompose (planner) admission — a decompose attempt acquires the llm: and
// node: pools a task on the same backend+model would use, minus "host" (the
// autonomous:decompose one-shot session is already counted against host by
// the ledger's External counter, so the planner lease must not stack on it).
// The lease is held only for the attempt and released on success, error,
// cancel and timeout; the wait is surfaced on the PRD (PRD.WaitReason, same
// convention as Task.WaitReason) and cleared when it ends; a cancel/reject
// while queued drops the planner immediately without running the LLM call.

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// decomposeCapFixture mirrors cmd/datawatch's keys function for a
// local-engine backend: host + llm:<name> + node:<name>.
func decomposeCapFixture(t *testing.T) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("host", 4)
	led.SetLimit("llm:ollama", 1)
	led.SetLimit("node:n1", 2)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:ollama", "node:n1"}, "n1"
	})
	return m, led
}

func planHolder(prdID string) string { return "plan:" + prdID }

func prdWaitReason(t *testing.T, m *Manager, prdID string) string {
	t.Helper()
	cur, ok := m.Store().SnapshotPRD(prdID)
	if !ok {
		return ""
	}
	return cur.WaitReason
}

func heldBy(led *capacity.Ledger, holder string) bool {
	for _, l := range led.Snapshot().Leases {
		if l.Holder == holder {
			return true
		}
	}
	return false
}

func plannerQueued(led *capacity.Ledger, prdID string) bool {
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == planHolder(prdID) {
			return true
		}
	}
	return false
}

func TestDecompose_WaitsForLLMPoolAndProceeds(t *testing.T) {
	m, led := decomposeCapFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)

	// Charge the llm: pool (max_inflight=1) with another inflight call; the
	// node limit is 2, so one node slot held is not enough to block — the
	// llm pool is what queues the planner.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other-llm", PRDID: "o", Pools: []string{"llm:ollama"}, Node: "n1", Model: "ollama/qm"}, 0, nil, nil); err != nil {
		t.Fatalf("holding llm slot: %v", err)
	}
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "other-node", PRDID: "o2", Pools: []string{"node:n1"}, Node: "n1", Model: "qm"}, 0, nil, nil); err != nil {
		t.Fatalf("holding node slot: %v", err)
	}

	var calls atomic.Int32
	orig := m.decompose
	m.decompose = func(req DecomposeRequest) (string, error) {
		calls.Add(1)
		return orig(req)
	}

	done := make(chan struct {
		prd *PRD
		err error
	}, 1)
	go func() {
		got, err := m.Decompose(prd.ID)
		done <- struct {
			prd *PRD
			err error
		}{got, err}
	}()

	// Queued: wait reason surfaced on the PRD, planner not admitted.
	pollUntil(t, 3*time.Second, "planner queued with a wait reason", func() bool {
		return prdWaitReason(t, m, prd.ID) != "" && plannerQueued(led, prd.ID)
	})
	if calls.Load() != 0 {
		t.Fatalf("planner must not run while queued (calls=%d)", calls.Load())
	}

	// Free both pools: the planner proceeds and completes.
	led.Release("other-llm")
	led.Release("other-node")
	var res struct {
		prd *PRD
		err error
	}
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("decompose did not finish after capacity freed")
	}
	if res.err != nil {
		t.Fatalf("decompose: %v", res.err)
	}
	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDNeedsReview {
		t.Fatalf("want PRDNeedsReview after decompose, got %s", got.Status)
	}
	if got.WaitReason != "" {
		t.Fatalf("wait reason must clear once admitted, got %q", got.WaitReason)
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("planner lease leaked after decompose finished")
	}
}

func TestDecompose_ReleasesLeaseOnAttemptError(t *testing.T) {
	m, led := decomposeCapFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	m.decompose = func(DecomposeRequest) (string, error) {
		return "", errors.New("upstream boom")
	}

	// The attempt is admitted, then fails — the lease must not outlive it.
	if _, err := m.decomposeAdmitted(prd, "opencode", "qm", DecomposeRequest{Spec: "s"}); err == nil {
		t.Fatal("expected the attempt to error")
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("lease leaked after a failed attempt")
	}
	if r := prdWaitReason(t, m, prd.ID); r != "" {
		t.Fatalf("wait reason must clear after the attempt ends, got %q", r)
	}
}

func TestDecompose_TimeoutClearsWaitReason(t *testing.T) {
	m, led := decomposeCapFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)

	// Fill the single llm: slot so the planner can never be admitted.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy", Pools: []string{"llm:ollama"}}, 0, nil, nil); err != nil {
		t.Fatalf("holding llm slot: %v", err)
	}
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 1 // wait 1s, then give up
	m.mu.Unlock()

	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return "x", nil
	}

	start := time.Now()
	_, err := m.decomposeAdmitted(prd, "opencode", "qm", DecomposeRequest{Spec: "s"})
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("want a wait timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Fatalf("waited only %s — the wait timeout was not honored", elapsed)
	}
	if calls.Load() != 0 {
		t.Fatal("planner ran despite the timeout")
	}
	if r := prdWaitReason(t, m, prd.ID); r != "" {
		t.Fatalf("wait reason must clear on timeout, got %q", r)
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("lease leaked after the wait timed out")
	}
}

func TestDecompose_CancelWhileWaitingDropsPlanner(t *testing.T) {
	m, led := decomposeCapFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)

	if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy", Pools: []string{"llm:ollama"}}, 0, nil, nil); err != nil {
		t.Fatalf("holding llm slot: %v", err)
	}

	var calls atomic.Int32
	orig := m.decompose
	m.decompose = func(req DecomposeRequest) (string, error) {
		calls.Add(1)
		return orig(req)
	}

	done := make(chan error, 1)
	go func() { _, err := m.Decompose(prd.ID); done <- err }()

	// Queue, then cancel the PRD while the planner waits.
	pollUntil(t, 3*time.Second, "planner queued", func() bool {
		return plannerQueued(led, prd.ID)
	})
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDCancelled
	_ = m.Store().SavePRD(cur)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("decompose must fail when cancelled while waiting")
		}
		if !errors.Is(err, errDecomposeCancelled) {
			t.Fatalf("want errDecomposeCancelled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled planner was not dropped from the queue")
	}

	if calls.Load() != 0 {
		t.Fatalf("planner must never have run (calls=%d)", calls.Load())
	}
	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDCancelled {
		t.Fatalf("cancel must survive the in-flight decompose, got %s", got.Status)
	}
	if r := prdWaitReason(t, m, prd.ID); r != "" {
		t.Fatalf("wait reason must clear when the planner is dropped, got %q", r)
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("lease leaked for a dropped planner")
	}
	if plannerQueued(led, prd.ID) {
		t.Fatal("cancelled planner still in the wait queue")
	}
}

func TestDecompose_NodeLimitRespected(t *testing.T) {
	m, led := decomposeCapFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)

	// Fill the node pool (2 slots — the hard limit of engines per node) so
	// the planner cannot be admitted.
	for i := 0; i < 2; i++ {
		if err := led.Acquire(context.Background(), capacity.Request{Holder: fmt.Sprintf("worker-%d", i), PRDID: "other", Pools: []string{"node:n1"}, Node: "n1", Model: "qm"}, 0, nil, nil); err != nil {
			t.Fatalf("filling node slot %d: %v", i, err)
		}
	}

	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return "x", nil
	}

	done := make(chan struct{}, 1)
	go func() {
		_, _ = m.decomposeAdmitted(prd, "opencode", "qm", DecomposeRequest{Spec: "s"})
		done <- struct{}{}
	}()

	pollUntil(t, 3*time.Second, "planner queued on node pool", func() bool {
		return plannerQueued(led, prd.ID)
	})
	if calls.Load() != 0 {
		t.Fatal("planner ran past the node limit (2 engines per node)")
	}

	led.Release("worker-0")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("planner did not proceed after a node slot freed")
	}
	if calls.Load() != 1 {
		t.Fatalf("want exactly one planner run, got %d", calls.Load())
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("planner lease leaked")
	}
}

func TestDecompose_PlannerLeaseExcludesHost(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{
		PollInterval: 10 * time.Millisecond,
		// One external (operator) session occupies the entire host pool —
		// the planner must still proceed, so its lease cannot include host.
		External: func(pool string) int {
			if pool == "host" {
				return 1
			}
			return 0
		},
	})
	led.SetLimit("host", 1)
	led.SetLimit("llm:ollama", 2)
	led.SetLimit("node:n1", 2)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:ollama", "node:n1"}, "n1"
	})

	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)

	var leaseAtRun capacity.Lease
	var seen atomic.Bool
	orig := m.decompose
	m.decompose = func(req DecomposeRequest) (string, error) {
		for _, l := range led.Snapshot().Leases {
			if l.Holder == planHolder(prd.ID) {
				leaseAtRun = l
				seen.Store(true)
			}
		}
		return orig(req)
	}

	start := time.Now()
	got, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("decompose with a full host pool must proceed: %v", err)
	}
	if dur := time.Since(start); dur > 3*time.Second {
		t.Fatalf("decompose had to wait despite host being excluded (took %s)", dur)
	}
	if got != nil && got.Status != PRDNeedsReview {
		t.Fatalf("want PRDNeedsReview, got %s", got.Status)
	}
	if !seen.Load() {
		t.Fatal("planner lease was never observed at run time")
	}
	for _, p := range leaseAtRun.Pools {
		if p == "host" {
			t.Fatalf("planner lease stacks on the External-counted host slot: %v", leaseAtRun.Pools)
		}
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("planner lease leaked")
	}
}

func TestDecompose_CapacityDisabledUnchanged(t *testing.T) {
	m, led := decomposeCapFixture(t)
	off := false
	m.mu.Lock()
	m.cfg.CapacityEnabled = &off
	m.mu.Unlock()

	// Full pools: with admission disabled the attempt must run anyway,
	// holding no lease — admission-off behaviour is a hard no-op.
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy", Pools: []string{"llm:ollama", "node:n1", "node:n1"}}, 0, nil, nil); err != nil {
		t.Fatalf("holding pools: %v", err)
	}
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	var calls atomic.Int32
	orig := m.decompose
	m.decompose = func(req DecomposeRequest) (string, error) {
		calls.Add(1)
		if heldBy(led, planHolder(prd.ID)) {
			t.Log("unexpected planner lease while admission is disabled")
		}
		return orig(req)
	}

	if _, err := m.decomposeAdmitted(prd, "opencode", "qm", DecomposeRequest{Spec: "s"}); err != nil {
		t.Fatalf("admission disabled must not block: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("want exactly one direct run, got %d", calls.Load())
	}
	if held := heldBy(led, planHolder(prd.ID)); held {
		t.Fatal("admission disabled must not hold a lease")
	}
}
