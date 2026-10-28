package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
)

// productionShape builds the fleet's LLM registry the way it is registered in
// production: three single-node ollama entries (one per serving node, node
// names anonymised), a pi-prompt entry whose compute_nodes list two of those
// nodes and whose models list serves the same model on both, plus plain
// opencode and pi entries that list no compute nodes of their own. Every node
// is registered as a compute node limited to one engine, so at most one lease
// can sit on a node at a time. It reuses the candidate-setup helpers and
// returns the shared resolver function, the ledger and the node list.
var productionShapeFixture = struct {
	nodes []string
	prio  map[string]int
	llms  []*inference.LLM
}{
	nodes: []string{"alpha", "bravo", "delta"},
	prio:  map[string]int{"alpha": 70, "bravo": 60, "delta": 60},
	llms: []*inference.LLM{
		{Name: "ollama-alpha", Kind: inference.KindOllama, ComputeNodes: []string{"alpha"},
			Models: []inference.EnabledModel{{Node: "alpha", Model: "qwen"}}},
		{Name: "ollama-bravo", Kind: inference.KindOllama, ComputeNodes: []string{"bravo"},
			Models: []inference.EnabledModel{{Node: "bravo", Model: "qwen"}}},
		{Name: "ollama-delta", Kind: inference.KindOllama, ComputeNodes: []string{"delta"},
			Models: []inference.EnabledModel{{Node: "delta", Model: "qwen"}}},
		{Name: "pi-prompt", Kind: inference.KindPiPrompt, ComputeNodes: []string{"bravo", "delta"},
			Models: []inference.EnabledModel{{Node: "bravo", Model: "qwen"}, {Node: "delta", Model: "qwen"}}},
		{Name: "opencode", Kind: inference.KindOpenCode},
		{Name: "pi", Kind: inference.KindPi},
	},
}

func productionShape(t *testing.T) (func(backend, model string) ([]string, string, []capacity.NodeChoice), *capacity.Ledger, []string) {
	t.Helper()
	fx := productionShapeFixture
	resolve, led := candidateSetup(t, fx.llms, fx.prio, fx.nodes...)
	return resolve, led, fx.nodes
}

func TestProductionShapePiPromptOffersBothNodes(t *testing.T) {
	resolve, led, nodes := productionShape(t)

	wantNodes := map[string]bool{}
	for _, n := range nodes {
		wantNodes[n] = true
	}
	if !wantNodes["alpha"] || !wantNodes["bravo"] || !wantNodes["delta"] {
		t.Fatalf("nodes = %v, want alpha, bravo and delta", nodes)
	}

	pools, node, choices := resolve("pi-prompt", "ollama/qwen")
	if node != "" || !reflect.DeepEqual(pools, []string{"host", "llm:pi-prompt"}) {
		t.Fatalf("resolve = %v %q %v, want no fixed node, the ledger chooses", pools, node, choices)
	}
	have := map[string]bool{}
	for _, c := range choices {
		have[c.Name] = true
	}
	if len(choices) != 2 || !have["bravo"] || !have["delta"] {
		t.Fatalf("choices = %+v, want bravo and delta", choices)
	}

	// With one engine per node the two leases land on two different nodes.
	first := candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi-prompt", "ollama/qwen"))
	second := candidateAcquire(t, led, candidateRequest(resolve, "t2", "pi-prompt", "ollama/qwen"))
	if first.Node == second.Node || !have[first.Node] || !have[second.Node] {
		t.Fatalf("leases on %q and %q, want bravo and delta", first.Node, second.Node)
	}
	if !leasePools(first)["node:"+first.Node] || !leasePools(second)["node:"+second.Node] {
		t.Fatalf("lease pools = %v and %v, want each lease on its own node pool", first.Pools, second.Pools)
	}
}

// The pi-prompt backend must draw its candidates from its own entry's
// compute_nodes. The production registry also carries three single-node
// ollama entries (one per node, including alpha, which the pi-prompt entry
// does not list), so a resolver that fell back to the provider's engine
// entries would leak alpha into the candidates.
func TestProductionShapePiPromptTakesCandidatesFromOwnEntryNotOllamaEntries(t *testing.T) {
	resolve, _, _ := productionShape(t)

	_, node, choices := resolve("pi-prompt", "ollama/qwen")
	if node != "" {
		t.Fatalf("node = %q, want the ledger choosing between the entry's nodes", node)
	}
	have := map[string]bool{}
	for _, c := range choices {
		have[c.Name] = true
	}
	if len(choices) != 2 || !have["bravo"] || !have["delta"] || have["alpha"] {
		t.Fatalf("choices = %+v, want exactly bravo and delta, never the alpha that only the single-node ollama entries list", choices)
	}

	// The same rule when the pi-prompt entry lists a single node: it binds
	// that node of its own instead of one the ollama entries list.
	llms := []*inference.LLM{
		{Name: "ollama-alpha", Kind: inference.KindOllama, ComputeNodes: []string{"alpha"},
			Models: []inference.EnabledModel{{Node: "alpha", Model: "qwen"}}},
		{Name: "ollama-delta", Kind: inference.KindOllama, ComputeNodes: []string{"delta"},
			Models: []inference.EnabledModel{{Node: "delta", Model: "qwen"}}},
		{Name: "pi-prompt", Kind: inference.KindPiPrompt, ComputeNodes: []string{"bravo"},
			Models: []inference.EnabledModel{{Node: "bravo", Model: "qwen"}}},
	}
	solo, _ := candidateSetup(t, llms, map[string]int{"alpha": 80, "bravo": 50, "delta": 50}, "alpha", "bravo", "delta")
	pools, node, choices := solo("pi-prompt", "ollama/qwen")
	if node != "bravo" || choices != nil || !reflect.DeepEqual(pools, []string{"host", "llm:pi-prompt", "node:bravo"}) {
		t.Fatalf("resolve = %v %q %v, want the entry's own node bravo with its pool", pools, node, choices)
	}
}

func TestProductionShapeSecondRequestGoesToFreeNodeThirdWaitsNamingBoth(t *testing.T) {
	resolve, led, _ := productionShape(t)

	first := candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi-prompt", "ollama/qwen"))
	second := candidateAcquire(t, led, candidateRequest(resolve, "t2", "pi-prompt", "ollama/qwen"))
	if first.Node == second.Node {
		t.Fatalf("both requests on %q, want one engine per node", first.Node)
	}

	// Both entry nodes are now held: the next request waits, names the two
	// busy nodes, and holds no lease of its own.
	err := led.Acquire(context.Background(), candidateRequest(resolve, "t3", "pi-prompt", "ollama/qwen"), 30*time.Millisecond, nil, nil)
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("err = %v, want wait timeout with both candidate nodes busy", err)
	}
	if led.Held("t3") {
		t.Fatal("t3 holds a lease although every candidate node is busy")
	}
	if !strings.Contains(err.Error(), "bravo") || !strings.Contains(err.Error(), "delta") {
		t.Fatalf("wait reason %q does not name both busy nodes", err)
	}
}

func TestProductionShapeAdoptedSurvivorsHoldNodesWithoutStacking(t *testing.T) {
	resolve, led, _ := productionShape(t)

	// A daemon restart: two workers survived on bravo and delta and their
	// leases are re-established through Adopt, not a fresh Acquire.
	adopt := func(holder, node string) *capacity.Lease {
		led.Adopt(capacity.Request{
			Holder: holder, PRDID: "prd",
			Pools: []string{"host", "llm:pi-prompt"},
			Node:  node,
			Model: "ollama/qwen",
		}, "sess-"+holder)
		ls := led.LeaseOf(holder)
		if ls == nil {
			t.Fatalf("%s: no lease after adopt", holder)
		}
		return ls
	}

	w1 := adopt("w1", "bravo")
	w2 := adopt("w2", "delta")
	for _, ls := range []*capacity.Lease{w1, w2} {
		p := leasePools(ls)
		if ls.Node == "" || !p["node:"+ls.Node] || !p["host"] {
			t.Fatalf("lease %+v, want a node lease on its node pool", ls)
		}
	}

	// Both candidate nodes are held: a new request waits rather than stacking
	// a second lease on either node.
	err := led.Acquire(context.Background(), candidateRequest(resolve, "n1", "pi-prompt", "ollama/qwen"), 30*time.Millisecond, nil, nil)
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("err = %v, want wait timeout while both adoptees hold their nodes", err)
	}
	if led.Held("n1") {
		t.Fatal("n1 admitted while both adoptee nodes are held")
	}

	// Free one adoptee: the new request is admitted only to the freed node.
	led.Release("w1")
	later := candidateAcquire(t, led, candidateRequest(resolve, "n1", "pi-prompt", "ollama/qwen"))
	if later.Node != "bravo" {
		t.Fatalf("node = %q, want bravo, the node freed by the release", later.Node)
	}

	// Still exactly one lease per node, none stacked.
	perNode := map[string]int{}
	for _, ls := range led.Snapshot().Leases {
		perNode[ls.Node]++
	}
	if perNode["bravo"] != 1 || perNode["delta"] != 1 {
		t.Fatalf("leases per node = %v, want one on bravo and one on delta", perNode)
	}
}

func TestProductionShapeNodePoolsNeverHeldAboveTheirLimits(t *testing.T) {
	resolve, led, _ := productionShape(t)

	checkLimits := func() {
		t.Helper()
		for _, p := range led.Snapshot().Pools {
			if !strings.HasPrefix(p.Name, "node:") || p.Limit <= 0 {
				continue
			}
			if p.Held > p.Limit {
				t.Fatalf("pool %s held by %d above its engine limit of %d", p.Name, p.Held, p.Limit)
			}
		}
	}

	// Repeated admit / overflow / release cycles: after every step, no node
	// pool is held above its one-engine limit.
	for round := 1; round <= 3; round++ {
		id := func(kind string) string { return fmt.Sprintf("w%d-%s", round, kind) }

		if ls := candidateAcquire(t, led, candidateRequest(resolve, id("a"), "pi-prompt", "ollama/qwen")); ls.Node == "" {
			t.Fatalf("round %d: placed without a node", round)
		}
		checkLimits()
		if ls := candidateAcquire(t, led, candidateRequest(resolve, id("b"), "pi-prompt", "ollama/qwen")); ls.Node == "" {
			t.Fatalf("round %d: placed without a node", round)
		}
		checkLimits()

		// Third request has no candidate node: it must time out, not stack.
		err := led.Acquire(context.Background(), candidateRequest(resolve, id("c"), "pi-prompt", "ollama/qwen"), 30*time.Millisecond, nil, nil)
		if !errors.Is(err, capacity.ErrWaitTimeout) {
			t.Fatalf("round %d: err = %v, want wait timeout", round, err)
		}
		if led.Held(id("c")) {
			t.Fatalf("round %d: overflow request holds a lease", round)
		}
		checkLimits()

		led.Release(id("a"))
		checkLimits()
		led.Release(id("b"))
		checkLimits()
	}
}
