package main

import (
	"reflect"
	"testing"

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
