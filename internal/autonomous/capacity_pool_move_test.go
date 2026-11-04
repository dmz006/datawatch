package autonomous

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

func poolMoveRig(t *testing.T, keys func(string, string) ([]string, string)) (*Manager, *capacity.Ledger, *PRD, *Task) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	fc := newFakeCapClock()
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond, Clock: fc})
	led.SetLimit("node:datawatch", 1)
	m.SetCapacity(led, keys)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := &cur.Story[0].Tasks[0]
	task.SessionID = "live-sess"
	task.ComputeNode = "datawatch"
	task.Backend = "claude-code" // a hosted backend by default; tests of local engines override it
	_ = m.Store().SaveTask(task)
	return m, led, cur, task
}

func nodeHeld(led *capacity.Ledger, pool string) int {
	for _, p := range led.Snapshot().Pools {
		if p.Name == pool {
			return p.Held
		}
	}
	return 0
}

func claudeKeys(string, string) ([]string, string) { return []string{"llm:claude"}, "" }

func TestPoolMove_AdoptDropsNodeLeaseForNodelessBackend(t *testing.T) {
	m, led, prd, task := poolMoveRig(t, claudeKeys)
	// Lease left over from when the task ran on pi-prompt.
	led.Adopt(capacity.Request{Holder: task.ID, PRDID: prd.ID, Pools: []string{"llm:pi"}, Node: "datawatch"}, "live-sess")
	if nodeHeld(led, "node:datawatch") != 1 {
		t.Fatal("setup: node:datawatch not held")
	}
	m.adoptCapacity(prd, task)
	if got := nodeHeld(led, "node:datawatch"); got != 0 {
		t.Fatalf("node:datawatch Held = %d, want 0", got)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "" {
		t.Fatalf("lease = %+v, want no node", ls)
	}
	if st, _ := m.Store().GetPRD(prd.ID); st.Story[0].Tasks[0].ComputeNode != "" {
		t.Fatal("stored ComputeNode not cleared")
	}
}

func TestPoolMove_FreshAdoptIgnoresRecordedNode(t *testing.T) {
	m, led, prd, task := poolMoveRig(t, claudeKeys)
	m.adoptCapacity(prd, task)
	if got := nodeHeld(led, "node:datawatch"); got != 0 {
		t.Fatalf("node:datawatch Held = %d, want 0", got)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "" || ls.NodeUnknown {
		t.Fatalf("lease = %+v, want held with no node", ls)
	}
}

func TestPoolMove_WaiterGrantedAfterMove(t *testing.T) {
	m, led, prd, task := poolMoveRig(t, claudeKeys)
	led.Adopt(capacity.Request{Holder: task.ID, PRDID: prd.ID, Pools: []string{"llm:pi"}, Node: "datawatch"}, "live-sess")
	done := make(chan error, 1)
	go func() {
		done <- led.Acquire(context.Background(), capacity.Request{Holder: "waiter", PRDID: "w", Pools: []string{"node:datawatch"}}, 5*time.Second, nil, nil)
	}()
	time.Sleep(100 * time.Millisecond)
	if led.Held("waiter") {
		t.Fatal("waiter granted while node still held")
	}
	m.adoptCapacity(prd, task)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pi-prompt waiter not granted after the move")
	}
}

func TestPoolMove_NodeTakingBackendKeepsNodeLease(t *testing.T) {
	m, led, prd, task := poolMoveRig(t, func(string, string) ([]string, string) {
		return []string{"llm:pi", "node:datawatch"}, "datawatch"
	})
	m.adoptCapacity(prd, task)
	if got := nodeHeld(led, "node:datawatch"); got != 1 {
		t.Fatalf("node:datawatch Held = %d, want 1", got)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "datawatch" {
		t.Fatalf("lease = %+v, want node datawatch", ls)
	}
}

// A node-less backend ignores even the node its live session reports.
func TestPoolMove_AdoptIgnoresSessionReportedNodeForNodelessBackend(t *testing.T) {
	m, led, prd, task := poolMoveRig(t, claudeKeys)
	m.SetSessionNode(func(id string) string { return "datawatch" })
	m.adoptCapacity(prd, task)
	if got := nodeHeld(led, "node:datawatch"); got != 0 {
		t.Fatalf("node:datawatch Held = %d, want 0", got)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "" {
		t.Fatalf("lease = %+v, want no node", ls)
	}
}
