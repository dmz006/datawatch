package autonomous

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// consumerGateFixture wires a manager to a real ledger whose NodeGate is the
// test's fn (in place of the daemon's nodeHealthView.gate), so the test proves
// what the ledger sees from an Automaton placement: the consumer name on the
// request and that a gate denial is honoured end to end.
func consumerGateFixture(t *testing.T, gate func(node, consumer string) (bool, string)) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Second, NodeGate: gate})
	led.SetLimit("node:good", 1)
	led.SetLimit("node:denied", 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"node:good"}, "good" })
	return m, led
}

// A worker placement must ride the session_spawn consumer name, and a node the
// gate denies for that consumer must never be the one granted — the ledger
// skips it in the candidate walk and grants the healthy one instead.
func TestConsumer_SessionSpawnOnPlacementAndDeniedNodeNeverGranted(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	gate := func(node, consumer string) (bool, string) {
		mu.Lock()
		seen = append(seen, node+"/"+consumer)
		mu.Unlock()
		if node == "denied" && consumer == consumerSessionSpawn {
			return false, "consumer session_spawn denied"
		}
		return true, ""
	}
	m, led := consumerGateFixture(t, gate)
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		return []capacity.NodeChoice{{Name: "denied", Priority: 9}, {Name: "good", Priority: 1}}
	})

	// Probe: the same single-node request WITHOUT a consumer goes where the
	// session_spawn one goes — the gate denies "denied" only for that
	// consumer, so only a request that carries the consumer name is refused.
	if d := led.Check(capacity.Request{Holder: "probe", PRDID: "p", Node: "denied", Pools: []string{"node:denied"}}); d.Kind != capacity.DecisionAdmit {
		t.Fatalf("probe without a consumer = %+v, want admit (the denial is consumer-specific)", d)
	}
	if d := led.Check(capacity.Request{Holder: "probe2", PRDID: "p", Node: "denied", Pools: []string{"node:denied"}, Consumer: consumerSessionSpawn}); d.Kind == capacity.DecisionAdmit {
		t.Fatalf("probe WITH session_spawn = %+v, want a refusal naming the consumer", d)
	}
	// The probes exercised the gate too; only the placement's calls count below.
	mu.Lock()
	seen = nil
	mu.Unlock()

	prd := newPRDWithTasks(t, m, 1)
	tk := prd.Story[0].Tasks[0]
	if err := m.admit(context.Background(), prd, &tk, "pi", "auto", capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
	defer led.Release(tk.ID)

	ls := led.LeaseOf(tk.ID)
	if ls == nil || ls.Node != "good" {
		t.Fatalf("lease = %+v, want node good because the denied node refuses session_spawn", ls)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("the gate was never consulted; the candidate walk must check every candidate for the request's consumer")
	}
	checkedDenied := false
	for _, entry := range seen {
		node, cons := cutSlash(entry)
		if cons != consumerSessionSpawn {
			t.Errorf("placement presented consumer %q on node %q, want %q (entries: %v)", cons, node, consumerSessionSpawn, seen)
		}
		if node == "denied" {
			checkedDenied = true
		}
	}
	if !checkedDenied {
		t.Errorf("gate calls %v never checked the denied candidate; the walk must test it", seen)
	}
}

// Single-node path: the request names one node (no candidate list) and the
// gate denies it for session_spawn. The admit must then wait and come out with
// the capacity timeout and NO lease — never a grant on the denied node — and
// once the gate opens, the same manager path is granted normally.
func TestConsumer_DeniedSingleNodeNeverGranted(t *testing.T) {
	var mu sync.Mutex
	allow := false
	gate := func(node, consumer string) (bool, string) {
		mu.Lock()
		a := allow
		mu.Unlock()
		if !a && node == "denied" && consumer == consumerSessionSpawn {
			return false, "consumer session_spawn denied"
		}
		return true, ""
	}
	m, led := consumerGateFixture(t, gate)
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"node:denied"}, "denied" })
	// A one-second wait bound so the denied admission ends in the timeout,
	// not in a hang.
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 1
	m.mu.Unlock()
	prd := newPRDWithTasks(t, m, 2)

	tkDenied := prd.Story[0].Tasks[0]
	err := m.admit(context.Background(), prd, &tkDenied, "pi", "auto", capacity.RoleWorker)
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("admit on a denied node = %v, want the capacity wait timeout (a grant would be the bug)", err)
	}
	if ls := led.LeaseOf(tkDenied.ID); ls != nil {
		t.Fatalf("a denied single node must never be granted: lease %+v", ls)
	}
	if r := led.Check(capacity.Request{Holder: "now", PRDID: prd.ID, Node: "denied", Pools: []string{"node:denied"}, Consumer: consumerSessionSpawn}); r.Kind == capacity.DecisionAdmit {
		t.Fatalf("Check while denied = %+v, want a refusal naming the consumer", r)
	}

	mu.Lock()
	allow = true
	mu.Unlock()
	tkAllowed := prd.Story[0].Tasks[1]
	if err := m.admit(context.Background(), prd, &tkAllowed, "pi", "auto", capacity.RoleWorker); err != nil {
		t.Fatalf("admit after the gate opens: %v", err)
	}
	if ls := led.LeaseOf(tkAllowed.ID); ls == nil || ls.Node != "denied" {
		t.Fatalf("lease = %+v, want the same node once the gate grants it", ls)
	}
}

func cutSlash(s string) (node, cons string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
