package autonomous

import (
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// waitingOverrideRig builds a running PRD whose only task waits for capacity
// on a full pi pool (queueWaiter fills node:solo and queues the task on it).
func waitingOverrideRig(t *testing.T) (*Manager, *capacity.Ledger, string, string) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:solo", 1)
	led.SetLimit("llm:claude", 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if backend == "claude-code" {
			return []string{"llm:claude"}, ""
		}
		return []string{"llm:pi", "node:solo"}, "solo"
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	cur.Story[0].Tasks[0].Status = TaskWaitingCapacity
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := cur.Story[0].Tasks[0].ID
	queueWaiter(t, led, taskID, prd.ID)
	m.setWaitReqPools(taskID, []string{"llm:pi", "node:solo"})
	return m, led, prd.ID, taskID
}

func TestWaitingOverride_MovesRequestToClaudePool(t *testing.T) {
	m, led, prdID, taskID := waitingOverrideRig(t)
	if _, res, err := m.SetTaskLLM(prdID, taskID, "claude-code", "", "", "op"); err != nil || res == nil || !res.Changed[0].CapacityMoved {
		t.Fatalf("SetTaskLLM res=%+v err=%v, want capacity moved", res, err)
	}
	// llm:claude is free, so the single request is granted there at once.
	deadline := time.Now().Add(2 * time.Second)
	for led.LeaseOf(taskID) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("no request on the claude pool; waiting=%+v", led.Snapshot().Waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := countTaskWaiters(led, taskID); n != 0 {
		t.Fatalf("waiters on the old pool = %d, want 0", n)
	}
	if got := m.waitReqPoolsNow(taskID); slices.Contains(got, "node:solo") {
		t.Fatalf("wait pools = %v still name the old pool", got)
	}
}

func TestWaitingOverride_ResetRequeuesOnClaudePoolWithOneRequest(t *testing.T) {
	m, led, prdID, taskID := waitingOverrideRig(t)
	// Fill the claude pool so the moved request stays queued and visible.
	_ = led.Acquire(t.Context(), capacity.Request{Holder: "claude-occupant", PRDID: "x", Pools: []string{"llm:claude"}}, 100*time.Millisecond, nil, nil)
	t.Cleanup(func() { led.Release("claude-occupant") })

	if _, _, err := m.SetTaskLLM(prdID, taskID, "claude-code", "", "", "op"); err != nil {
		t.Fatalf("SetTaskLLM: %v", err)
	}
	if got := taskWaiterPools(led, taskID); !slices.Equal(got, []string{"llm:claude"}) {
		t.Fatalf("pools after override = %v, want [llm:claude]", got)
	}
	m.setFallthrough(taskID, &CapacityAlternate{Backend: "pi-prompt"})

	up, err := m.ResetTask(prdID, taskID, "op", false)
	if err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	tk, _ := findTaskInPRD(up, taskID)
	if tk.AdmittedBackend != "" || tk.AdmittedModel != "" {
		t.Fatalf("admitted pair = %q/%q, want cleared", tk.AdmittedBackend, tk.AdmittedModel)
	}
	if _, ok := m.fallthroughOf(taskID); ok {
		t.Fatal("stale fallthrough survived the reset")
	}
	if n := countTaskWaiters(led, taskID); n != 1 {
		t.Fatalf("live requests = %d, want exactly 1", n)
	}
	if got := taskWaiterPools(led, taskID); !slices.Equal(got, []string{"llm:claude"}) {
		t.Fatalf("pools after reset = %v, want [llm:claude]", got)
	}
}
