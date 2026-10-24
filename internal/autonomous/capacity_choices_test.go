package autonomous

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// poolFixture wires a ledger whose resolver pair describes one LLM entry that
// pools nodes "slow" (priority 10) and "fast" (priority 90) when pooled is
// true, or the single node "solo" otherwise.
func poolFixture(t *testing.T, pooled bool) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"slow", "fast", "solo"} {
		led.SetLimit("node:"+n, 1)
	}
	if pooled {
		m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"host", "llm:pool"}, "" })
		m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
			return []capacity.NodeChoice{{Name: "slow", Priority: 10}, {Name: "fast", Priority: 90}}
		})
	} else {
		m.SetCapacity(led, func(backend, model string) ([]string, string) {
			return []string{"host", "llm:pool", "node:solo"}, "solo"
		})
		m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
			return []capacity.NodeChoice{{Name: "solo", Priority: 50}}
		})
	}
	return m, led
}

func TestAdmitPlacesTasksOnPooledNodes(t *testing.T) {
	m, led := poolFixture(t, true)
	if m.CapacityChoices() == nil {
		t.Fatal("CapacityChoices not wired")
	}
	prd := newPRDWithTasks(t, m, 3)
	cur, _ := m.Store().GetPRD(prd.ID)
	t0, t1, t2 := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1], cur.Story[0].Tasks[2]

	if err := m.admit(context.Background(), cur, &t0, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if ls := led.LeaseOf(t0.ID); ls == nil || ls.Node != "fast" {
		t.Fatalf("first task lease = %+v, want node fast", ls)
	}
	if err := m.admit(context.Background(), cur, &t1, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if ls := led.LeaseOf(t1.ID); ls == nil || ls.Node != "slow" {
		t.Fatalf("second task lease = %+v, want node slow once fast is full", ls)
	}
	// Both nodes are full, so a third task is not admitted.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := m.admit(ctx, cur, &t2, "pi", "ollama/q", capacity.RoleWorker); err == nil {
		t.Fatal("third task admitted with every node full")
	}
}

func TestAdmitSingleChoiceKeepsExistingPath(t *testing.T) {
	m, led := poolFixture(t, false)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	if err := m.admit(context.Background(), cur, &task, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	ls := led.LeaseOf(task.ID)
	if ls == nil || ls.Node != "solo" {
		t.Fatalf("lease = %+v, want node solo", ls)
	}
	if !samePoolSet(ls.Pools, []string{"host", "llm:pool", "node:solo"}) {
		t.Fatalf("pools = %v", ls.Pools)
	}
	if got := nodeChoicesFor(m.CapacityChoices(), "pi", "ollama/q"); got != nil {
		t.Fatalf("one candidate must not become choices, got %+v", got)
	}
	if got := nodeChoicesFor(nil, "pi", "m"); got != nil {
		t.Fatalf("nil resolver gave %+v", got)
	}
}

func TestDecomposeAdmittedPlacesPlannerOnPooledNode(t *testing.T) {
	m, led := poolFixture(t, true)
	prd, _ := m.CreatePRD("s", "/w/proj", "pi", "ollama/q", EffortNormal)
	var node string
	m.decompose = func(DecomposeRequest) (string, error) {
		if ls := led.LeaseOf("plan:" + prd.ID); ls != nil {
			node = ls.Node
		}
		return `{"title":"T","stories":[]}`, nil
	}
	if _, err := m.decomposeAdmitted(prd, "pi", "ollama/q", DecomposeRequest{Spec: "s"}); err != nil {
		t.Fatal(err)
	}
	if node != "fast" {
		t.Fatalf("planner lease node = %q, want fast", node)
	}
	if led.Held("plan:" + prd.ID) {
		t.Fatal("planner lease leaked")
	}
}
