package autonomous

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// shortenNoNodePoll makes a task that holds no node ask for capacity again
// quickly, and restores the production interval when the test ends.
func shortenNoNodePoll(t *testing.T) {
	t.Helper()
	old := noNodePollInterval
	noNodePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { noNodePollInterval = old })
}

// nodeAppears rewires the rig so the capacity resolver names no node until the
// returned function is called, and node gpu-a from then on: the way a node
// comes online (or gets a free slot) while a task waits for one.
func nodeAppears(m *Manager, led *capacity.Ledger) (appear func()) {
	var online atomic.Bool
	led.SetLimit("node:gpu-a", 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if online.Load() {
			return []string{"host", "llm:pool", "node:gpu-a"}, "gpu-a"
		}
		return []string{"host", "llm:pool"}, ""
	})
	return func() { online.Store(true) }
}

// noNodeRun starts a one-task pi-prompt PRD on the unnoded rig in the
// background and returns a stop-and-collect function plus the spawn log.
type noNodeRun struct {
	prdID  string
	taskID string
	done   chan error
	fin    chan struct{}

	mu     sync.Mutex
	spawns []SpawnRequest
}

func startNoNodeRun(t *testing.T, m *Manager, backend, model string) *noNodeRun {
	t.Helper()
	prd, err := m.CreatePRD("s", "/w/proj", backend, model, EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T0", Spec: "write docs/x.md"}}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDApproved
	_ = m.Store().SavePRD(cur)

	r := &noNodeRun{prdID: prd.ID, taskID: cur.Story[0].Tasks[0].ID, done: make(chan error, 1), fin: make(chan struct{})}
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		r.mu.Lock()
		r.spawns = append(r.spawns, req)
		r.mu.Unlock()
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.fin:
		case <-time.After(5 * time.Second):
		}
	})
	go func() {
		defer close(r.fin)
		r.done <- m.Run(ctx, prd.ID, spawn, verify)
	}()
	return r
}

func (r *noNodeRun) spawnCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.spawns)
}

func (r *noNodeRun) task(m *Manager) Task {
	got, _ := m.Store().TaskSnapshot(r.taskID)
	return got
}

// waitNoNode polls cond until it holds or the deadline passes.
func waitNoNode(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestNoNodeLeasedPiTaskIsRequeuedNotFailed verifies a pi task whose lease
// names no node goes back to waiting with a node reason, holds no lease while
// it waits, is not spawned, and its retry counters do not move.
func TestNoNodeLeasedPiTaskIsRequeuedNotFailed(t *testing.T) {
	shortenNoNodePoll(t)
	m, _ := unnodedLeaseRig(t, []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}})
	m.cfg.CapacityWaitTimeoutSeconds = 60
	r := startNoNodeRun(t, m, "pi-prompt", "ollama/qwen3.8:27b")

	waitNoNode(t, "task to wait for a node", func() bool {
		tk := r.task(m)
		return tk.Status == TaskWaitingCapacity && strings.Contains(tk.WaitReason, "no node")
	})
	// Let several re-admission rounds pass; the task must stay out of failure.
	time.Sleep(150 * time.Millisecond)
	tk := r.task(m)
	if tk.Status == TaskFailed || tk.Error != "" {
		t.Fatalf("task status = %q, error = %q, want it waiting and unfailed", tk.Status, tk.Error)
	}
	if strings.Contains(tk.WaitReason, "holds capacity but no compute node") {
		t.Fatalf("wait reason carries the old failure text: %q", tk.WaitReason)
	}
	if tk.RetryCount != 0 || tk.PriorAttemptCount != 0 {
		t.Fatalf("retry counters moved: RetryCount = %d, PriorAttemptCount = %d, want 0 and 0", tk.RetryCount, tk.PriorAttemptCount)
	}
	if n := r.spawnCount(); n != 0 {
		t.Fatalf("worker spawned %d times with no node, want 0", n)
	}
}

// TestNoNodeLeasedRetryCountUnchangedAfterNodeAppears verifies the retry count
// of a no-node task is the same after the wait ends and the worker spawns as
// it was for a task that never waited: zero.
func TestNoNodeLeasedRetryCountUnchangedAfterNodeAppears(t *testing.T) {
	shortenNoNodePoll(t)
	m, led := unnodedLeaseRig(t, []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}})
	m.cfg.CapacityWaitTimeoutSeconds = 60
	appear := nodeAppears(m, led)
	r := startNoNodeRun(t, m, "pi-prompt", "ollama/qwen3.8:27b")
	waitNoNode(t, "task to wait for a node", func() bool { return r.task(m).Status == TaskWaitingCapacity })
	before := r.task(m)

	appear()
	waitNoNode(t, "worker spawn", func() bool { return r.spawnCount() == 1 })
	waitNoNode(t, "run to finish", func() bool {
		select {
		case <-r.done:
			return true
		default:
			return false
		}
	})
	after := r.task(m)
	if after.RetryCount != before.RetryCount || after.PriorAttemptCount != before.PriorAttemptCount {
		t.Fatalf("retry counters moved across the wait: before (%d, %d), after (%d, %d)",
			before.RetryCount, before.PriorAttemptCount, after.RetryCount, after.PriorAttemptCount)
	}
	if after.Status == TaskFailed {
		t.Fatalf("task failed after a node appeared: %q", after.Error)
	}
}

// TestNoNodeLeasedSpawnsOnceNodeAppears verifies the waiting task spawns, on
// the node that appeared, as soon as the ledger can lease one.
func TestNoNodeLeasedSpawnsOnceNodeAppears(t *testing.T) {
	shortenNoNodePoll(t)
	m, led := unnodedLeaseRig(t, []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}})
	m.cfg.CapacityWaitTimeoutSeconds = 60
	appear := nodeAppears(m, led)
	r := startNoNodeRun(t, m, "pi", "ollama/qwen3.8:27b")
	waitNoNode(t, "task to wait for a node", func() bool { return r.task(m).Status == TaskWaitingCapacity })
	if n := r.spawnCount(); n != 0 {
		t.Fatalf("spawned %d times before a node existed, want 0", n)
	}

	appear()
	waitNoNode(t, "worker spawn", func() bool { return r.spawnCount() == 1 })
	r.mu.Lock()
	node := r.spawns[0].ComputeNode
	r.mu.Unlock()
	if node != "gpu-a" {
		t.Fatalf("spawn ComputeNode = %q, want gpu-a", node)
	}
}

// TestNoNodeLeasedWaitIsBoundedByCapacityBudget verifies the wait ends with the
// existing capacity wait bound, as any admission that waited out its budget:
// the error wraps the capacity wait timeout and no worker was spawned.
func TestNoNodeLeasedWaitIsBoundedByCapacityBudget(t *testing.T) {
	shortenNoNodePoll(t)
	m, _ := unnodedLeaseRig(t, []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}})
	m.cfg.CapacityWaitTimeoutSeconds = 1
	prd, err := m.CreatePRD("s", "/w/proj", "pi-prompt", "ollama/qwen3.8:27b", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T0", Spec: "write docs/x.md"}}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	t0 := &cur.Story[0].Tasks[0]
	// The task holds a lease that names no node, as it does after admission.
	if err := m.admit(context.Background(), cur, t0, "pi-prompt", "ollama/qwen3.8:27b", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, _, _, werr := m.awaitLeasedNode(context.Background(), cur, t0, "pi-prompt", "ollama/qwen3.8:27b")
	if werr == nil || !strings.Contains(werr.Error(), capacity.ErrWaitTimeout.Error()) {
		t.Fatalf("awaitLeasedNode error = %v, want one wrapping the capacity wait timeout", werr)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("wait took %s, want it bounded by the 1s capacity budget", elapsed)
	}
}
