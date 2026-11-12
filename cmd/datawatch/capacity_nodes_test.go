package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// previousKeys is the pool resolver as it behaved with one node per entry:
// the first compute node of the first LLM of the model's kind.
func previousKeys(reg *inference.Registry) func(backend, model string) ([]string, string) {
	return func(backend, model string) ([]string, string) {
		pools := []string{"host"}
		if reg != nil {
			if l, err := reg.Get(backend); err == nil && l != nil {
				pools = append(pools, "llm:"+l.Name)
			}
		}
		first := map[string]string{}
		if reg != nil {
			for _, l := range reg.List() {
				kind := string(l.Kind)
				if _, seen := first[kind]; seen || len(l.ComputeNodes) == 0 {
					continue
				}
				first[kind] = l.ComputeNodes[0]
			}
		}
		node, gated := autonomous.LocalEngineGate(backend, model, func(provider string) (string, bool) {
			n, ok := first[provider]
			return n, ok
		})
		if gated {
			pools = append(pools, "node:"+node)
		}
		return pools, node
	}
}

func nodeTestRegistries(t *testing.T, llms []*inference.LLM, nodes map[string]int) (*inference.Registry, *compute.Registry) {
	t.Helper()
	reg := inference.NewRegistry()
	for _, l := range llms {
		if err := reg.Add(l); err != nil {
			t.Fatalf("add llm %s: %v", l.Name, err)
		}
	}
	creg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, prio := range nodes {
		if err := creg.Add(&compute.Node{Name: name, Kind: compute.KindLocal, SchedulingPriority: prio}); err != nil {
			t.Fatalf("add node %s: %v", name, err)
		}
	}
	return reg, creg
}

func TestKeysOffersEveryNodeOfTheEntry(t *testing.T) {
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "ghost", "beta", "gamma"}}},
		map[string]int{"alpha": 0, "beta": 80, "gamma": 20})
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return creg })

	pools, node, choices := resolve("ollama", "ollama/qwen")
	if want := []string{"host", "llm:ollama"}; !reflect.DeepEqual(pools, want) {
		t.Fatalf("pools = %v, want %v (no node pool when the ledger chooses)", pools, want)
	}
	if node != "" {
		t.Fatalf("node = %q, want empty", node)
	}
	want := []capacity.NodeChoice{{Name: "alpha", Priority: 50}, {Name: "beta", Priority: 80}, {Name: "gamma", Priority: 20}}
	if !reflect.DeepEqual(choices, want) {
		t.Fatalf("choices = %+v, want %+v (unknown node dropped, unset priority 50)", choices, want)
	}

	// The resolver pair autonomous wires reports the same split.
	keys := func(b, m string) ([]string, string) { p, n, _ := resolve(b, m); return p, n }
	if p, n := keys("ollama", "ollama/qwen"); !reflect.DeepEqual(p, pools) || n != "" {
		t.Fatalf("keys = %v %q", p, n)
	}

	// A ledger given those choices places the request on the best node.
	led := capacity.New(capacity.Options{})
	for _, n := range []string{"alpha", "beta", "gamma"} {
		led.SetLimit("node:"+n, 1)
	}
	req := withNodeChoices(capacity.Request{Holder: "t1", Pools: pools, Node: node, Model: "ollama/qwen"}, choices)
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ls := led.LeaseOf("t1"); ls == nil || ls.Node != "beta" {
		t.Fatalf("lease = %+v, want node beta", ls)
	}

	// Hosted backends and unresolved providers gain no node and no choices.
	if p, n, c := resolve("claude-code", "opus"); n != "" || c != nil || !reflect.DeepEqual(p, []string{"host"}) {
		t.Fatalf("hosted = %v %q %v", p, n, c)
	}

	// With the pool reduced to one known node that node is used as before.
	reg2, creg2 := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"ghost", "beta"}}},
		map[string]int{"beta": 80})
	resolve2 := capacityResolver(func() *inference.Registry { return reg2 }, func() *compute.Registry { return creg2 })
	p, n, c := resolve2("ollama", "ollama/qwen")
	if n != "beta" || c != nil || !reflect.DeepEqual(p, []string{"host", "llm:ollama", "node:beta"}) {
		t.Fatalf("one known node = %v %q %v", p, n, c)
	}
}

func TestKeysSingleNodeMatchesPreviousPools(t *testing.T) {
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{
			{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha"}},
			{Name: "unregistered", Kind: "openwebui", ComputeNodes: []string{"nowhere"}},
			{Name: "hosted", Kind: "claude-code"},
		},
		map[string]int{"alpha": 70})
	cases := []struct{ backend, model string }{
		{"pi", "ollama/qwen3.8:27b"},
		{"ollama", "ollama/qwen3.8:27b"},
		{"pi-prompt", "openwebui/model"},
		{"pi", "ollama"},
		{"pi", "unknown/model"},
		{"claude-code", "opus"},
		{"hosted", ""},
		{"nosuch", "ollama/x"},
	}
	for name, nodes := range map[string]func() *compute.Registry{
		"compute registry": func() *compute.Registry { return creg },
		"no registry":      func() *compute.Registry { return nil },
	} {
		resolve := capacityResolver(func() *inference.Registry { return reg }, nodes)
		for _, tc := range cases {
			wantPools, wantNode := previousKeys(reg)(tc.backend, tc.model)
			pools, node, choices := resolve(tc.backend, tc.model)
			if !reflect.DeepEqual(pools, wantPools) || node != wantNode || choices != nil {
				t.Errorf("%s %s/%s: got %v %q %v, want %v %q", name, tc.backend, tc.model, pools, node, choices, wantPools, wantNode)
			}
		}
	}
	// A nil inference registry gives the host pool only.
	resolve := capacityResolver(func() *inference.Registry { return nil }, nil)
	if p, n, c := resolve("pi", "ollama/x"); !reflect.DeepEqual(p, []string{"host"}) || n != "" || c != nil {
		t.Fatalf("nil registry = %v %q %v", p, n, c)
	}
}

func TestResolverUsesEntryHostingTheModel(t *testing.T) {
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{
			{Name: "ollama-a", Kind: "ollama", ComputeNodes: []string{"alpha"}},
			{Name: "ollama-b", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"},
				Models: []inference.EnabledModel{{Node: "alpha", Model: "qwen"}, {Node: "beta", Model: "qwen"}}},
		},
		map[string]int{"alpha": 50, "beta": 50})
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return creg })

	pools, node, choices := resolve("pi-prompt", "ollama/qwen")
	if node != "" || len(choices) != 2 || !reflect.DeepEqual(pools, []string{"host"}) {
		t.Fatalf("hosting entry = %v %q %v, want both nodes as choices", pools, node, choices)
	}
	// A model no entry lists falls back to the first entry with nodes.
	if p, n, c := resolve("pi-prompt", "ollama/other"); n != "alpha" || c != nil || !reflect.DeepEqual(p, []string{"host", "node:alpha"}) {
		t.Fatalf("unlisted model = %v %q %v", p, n, c)
	}
}

func TestResolverDropsNodeNotServingModel(t *testing.T) {
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta", "gamma"},
			Models: []inference.EnabledModel{{Node: "alpha", Model: "qwen"}, {Node: "beta", Model: "llama"}}}},
		map[string]int{"alpha": 50, "beta": 50, "gamma": 50})
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return creg })

	// beta lists only another model; gamma lists none and stays a candidate.
	_, node, choices := resolve("pi", "ollama/qwen")
	want := []capacity.NodeChoice{{Name: "alpha", Priority: 50}, {Name: "gamma", Priority: 50}}
	if node != "" || !reflect.DeepEqual(choices, want) {
		t.Fatalf("choices = %q %+v, want %+v", node, choices, want)
	}
	// alpha lists only qwen, so llama is offered on beta and unlisted gamma.
	_, _, choices = resolve("pi", "ollama/llama")
	want = []capacity.NodeChoice{{Name: "beta", Priority: 50}, {Name: "gamma", Priority: 50}}
	if !reflect.DeepEqual(choices, want) {
		t.Fatalf("llama choices = %+v, want %+v", choices, want)
	}
}

func TestPlacementSignatureTracksModelsList(t *testing.T) {
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"}}},
		map[string]int{"alpha": 50, "beta": 50})
	regFn := func() *inference.Registry { return reg }
	nodesFn := func() *compute.Registry { return creg }
	base := placementSignature(regFn, nodesFn)
	l, _ := reg.Get("ollama")
	l.Models = []inference.EnabledModel{{Node: "alpha", Model: "qwen"}}
	if err := reg.Update(l); err != nil {
		t.Fatal(err)
	}
	if placementSignature(regFn, nodesFn) == base {
		t.Fatal("signature unchanged after the models list changed")
	}
}
