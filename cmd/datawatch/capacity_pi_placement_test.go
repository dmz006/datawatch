package main

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
)

const piPlacementModel = "ollama/qwen3.8:27b"

var piPlacementPriorities = map[string]int{"datawatch": 50, "local-node": 50, "verdict": 50}

// piPlacementLLMs builds the three single-node ollama entries and a pi-prompt
// entry listing local-node then datawatch, in the order given by reversed.
func piPlacementLLMs(piNodes []string, reversed bool) []*inference.LLM {
	llms := []*inference.LLM{
		{Name: "ollama-datawatch", Kind: "ollama", ComputeNodes: []string{"datawatch"}},
		{Name: "ollama-local-node", Kind: "ollama", ComputeNodes: []string{"local-node"}},
		{Name: "ollama-verdict", Kind: "ollama", ComputeNodes: []string{"verdict"}},
		{Name: "pi-prompt", Kind: "pi-prompt", ComputeNodes: piNodes},
	}
	if reversed {
		for i, j := 0, len(llms)-1; i < j; i, j = i+1, j-1 {
			llms[i], llms[j] = llms[j], llms[i]
		}
	}
	return llms
}

func TestPiPlacementOwnNodeListYieldsCandidates(t *testing.T) {
	resolve, _ := candidateSetup(t, piPlacementLLMs([]string{"local-node", "datawatch"}, false), piPlacementPriorities)

	_, node, choices := resolve("pi-prompt", piPlacementModel)
	if node != "" {
		t.Fatalf("node = %q, want empty so the ledger chooses", node)
	}
	got := map[string]bool{}
	for _, c := range choices {
		got[c.Name] = true
	}
	if len(choices) != 2 || !got["datawatch"] || !got["local-node"] {
		t.Fatalf("choices = %+v, want datawatch and local-node only", choices)
	}
}

func TestPiPlacementBusyNodeSendsNextRequestToOther(t *testing.T) {
	for _, first := range []string{"datawatch", "local-node"} {
		other := map[string]string{"datawatch": "local-node", "local-node": "datawatch"}[first]
		resolve, led := candidateSetup(t, piPlacementLLMs([]string{"local-node", "datawatch"}, false),
			piPlacementPriorities, "datawatch", "local-node")

		// Hold the first node's only engine directly.
		held := capacity.Request{Holder: "held", PRDID: "prd", Pools: []string{"host", "node:" + first}, Node: first, Model: piPlacementModel}
		if ls := candidateAcquire(t, led, held); ls.Node != first {
			t.Fatalf("held node = %q, want %q", ls.Node, first)
		}
		ls := candidateAcquire(t, led, candidateRequest(resolve, "next", "pi-prompt", piPlacementModel))
		if ls.Node != other {
			t.Fatalf("with %s held, node = %q, want %q", first, ls.Node, other)
		}
	}
}

func TestPiPlacementSingleNodeListKeepsNodePool(t *testing.T) {
	resolve, _ := candidateSetup(t, piPlacementLLMs([]string{"local-node"}, false), piPlacementPriorities)

	pools, node, choices := resolve("pi-prompt", piPlacementModel)
	if node != "local-node" || choices != nil {
		t.Fatalf("node = %q choices = %v, want local-node and no candidates", node, choices)
	}
	if want := []string{"host", "llm:pi-prompt", "node:local-node"}; !reflect.DeepEqual(pools, want) {
		t.Fatalf("pools = %v, want %v", pools, want)
	}
}

func TestPiPlacementNonPiBackendUsesProviderKindLookup(t *testing.T) {
	llms := []*inference.LLM{
		{Name: "ollama-datawatch", Kind: "ollama", ComputeNodes: []string{"datawatch"}},
		{Name: "opencode", Kind: "opencode"},
	}
	resolve, _ := candidateSetup(t, llms, piPlacementPriorities)

	pools, node, choices := resolve("opencode", piPlacementModel)
	if node != "datawatch" || choices != nil {
		t.Fatalf("node = %q choices = %v, want datawatch and no candidates", node, choices)
	}
	if want := []string{"host", "llm:opencode", "node:datawatch"}; !reflect.DeepEqual(pools, want) {
		t.Fatalf("pools = %v, want %v", pools, want)
	}
}

func TestPiPlacementEngineEntryIgnoresRegistryOrder(t *testing.T) {
	var names []string
	for _, reversed := range []bool{false, true} {
		reg, _ := nodeTestRegistries(t, piPlacementLLMs([]string{"local-node", "datawatch"}, reversed), piPlacementPriorities)
		l, ok := engineEntry(reg, "ollama", piPlacementModel)
		if !ok {
			t.Fatalf("reversed=%v: no entry", reversed)
		}
		names = append(names, l.Name)
	}
	if names[0] != names[1] || names[0] != "ollama-datawatch" {
		t.Fatalf("entries = %v, want ollama-datawatch both times", names)
	}
}
