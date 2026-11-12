package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// replaceRig is a Manager whose capacity key and choice functions read state
// the test changes at runtime, standing in for editing compute_nodes without a
// daemon restart.
type replaceRig struct {
	m    *Manager
	led  *capacity.Ledger
	prd  *PRD
	task Task

	mu      sync.Mutex
	pools   []string
	node    string
	choices []capacity.NodeChoice
}

func (r *replaceRig) setPlacement(pools []string, node string, choices []capacity.NodeChoice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools, r.node, r.choices = pools, node, choices
}

func newReplaceRig(t *testing.T) *replaceRig {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:A", 1)
	led.SetLimit("node:B", 1)
	r := &replaceRig{m: m, led: led}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]string(nil), r.pools...), r.node
	})
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]capacity.NodeChoice(nil), r.choices...)
	})
	r.prd = newPRDOnBackend(t, m, "pi", "ollama/qwen3.8:27b")
	snap, _ := m.Store().SnapshotPRD(r.prd.ID)
	r.task = snap.Story[0].Tasks[0]
	if r.task.ID == "" {
		t.Fatal("fixture task has no id")
	}
	return r
}

// hold takes a lease for holder on the node's pool.
func (r *replaceRig) hold(t *testing.T, holder, node string) {
	t.Helper()
	req := capacity.Request{Holder: holder, PRDID: holder, Pools: []string{"node:" + node}}
	if err := r.led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("hold %s on %s: %v", holder, node, err)
	}
}

// admitAsync runs admit for the fixture task and reports its result.
func (r *replaceRig) admitAsync(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	task := r.task
	go func() {
		done <- r.m.admit(ctx, r.prd, &task, "pi", "ollama/qwen3.8:27b", capacity.RoleWorker)
	}()
	return done
}

func (r *replaceRig) waitReason() string {
	snap, _ := r.m.Store().SnapshotPRD(r.prd.ID)
	return snap.Story[0].Tasks[0].WaitReason
}

func TestWaiterReplaceAcquireMovesWhenNodeFrees(t *testing.T) {
	r := newReplaceRig(t)
	// The entry collapses to one known node, A, which is busy.
	r.setPlacement([]string{"host", "node:A"}, "A", nil)
	r.hold(t, "holder-a", "A")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := r.admitAsync(ctx)

	pollUntil(t, 5*time.Second, "task queued on node A", func() bool {
		return strings.Contains(r.waitReason(), "node A")
	})
	select {
	case err := <-done:
		t.Fatalf("task admitted while A is busy: %v", err)
	default:
	}

	// compute_nodes now lists A and B; B is known and free.
	r.setPlacement([]string{"host"}, "", []capacity.NodeChoice{{Name: "A", Priority: 10}, {Name: "B", Priority: 90}})
	r.led.Release("holder-a")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting task was never re-placed onto the free node")
	}
	ls := r.led.LeaseOf(r.task.ID)
	if ls == nil || ls.Node != "B" {
		t.Fatalf("lease = %+v, want node B", ls)
	}
}

func TestWaiterReplaceAcquireStaysWhenNoNodeFree(t *testing.T) {
	r := newReplaceRig(t)
	r.setPlacement([]string{"host", "node:A"}, "A", nil)
	r.hold(t, "holder-a", "A")
	r.hold(t, "holder-b", "B")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := r.admitAsync(ctx)

	// Both nodes become known; both are busy.
	r.setPlacement([]string{"host"}, "", []capacity.NodeChoice{{Name: "A", Priority: 10}, {Name: "B", Priority: 90}})

	pollUntil(t, 5*time.Second, "wait reason naming both nodes", func() bool {
		reason := r.waitReason()
		return strings.Contains(reason, "A") && strings.Contains(reason, "B") && strings.Contains(reason, "nodes")
	})
	select {
	case err := <-done:
		t.Fatalf("task admitted while every node is busy: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if r.led.Held(r.task.ID) {
		t.Fatal("task holds a lease while every node is busy")
	}

	r.led.Release("holder-b")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task was not admitted after a node freed")
	}
	if ls := r.led.LeaseOf(r.task.ID); ls == nil || ls.Node != "B" {
		t.Fatalf("lease = %+v, want node B", ls)
	}
}
