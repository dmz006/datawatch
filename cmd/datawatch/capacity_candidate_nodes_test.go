package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// candidateSetup builds a resolver over a fake inference registry and a fake
// compute registry, and a ledger with one engine per listed node.
func candidateSetup(t *testing.T, llms []*inference.LLM, prio map[string]int, limited ...string) (func(backend, model string) ([]string, string, []capacity.NodeChoice), *capacity.Ledger) {
	t.Helper()
	reg, creg := nodeTestRegistries(t, llms, prio)
	resolve := capacityResolver(func() *inference.Registry { return reg }, func() *compute.Registry { return creg })
	led := capacity.New(capacity.Options{})
	for _, n := range limited {
		led.SetLimit("node:"+n, 1)
	}
	return resolve, led
}

// candidateRequest resolves backend/model and builds the ledger request the
// admission paths build from it.
func candidateRequest(resolve func(string, string) ([]string, string, []capacity.NodeChoice), holder, backend, model string) capacity.Request {
	pools, node, choices := resolve(backend, model)
	return withNodeChoices(capacity.Request{Holder: holder, PRDID: "prd", Pools: pools, Node: node, Model: model}, choices)
}

func candidateAcquire(t *testing.T, led *capacity.Ledger, req capacity.Request) *capacity.Lease {
	t.Helper()
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("%s: acquire: %v", req.Holder, err)
	}
	ls := led.LeaseOf(req.Holder)
	if ls == nil {
		t.Fatalf("%s: no lease", req.Holder)
	}
	return ls
}

func leasePools(ls *capacity.Lease) map[string]bool {
	m := map[string]bool{}
	for _, p := range ls.Pools {
		m[p] = true
	}
	return m
}

func TestCandidateBusyNodeSendsRequestToFreeNode(t *testing.T) {
	resolve, led := candidateSetup(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"},
			Models: []inference.EnabledModel{{Node: "alpha", Model: "qwen"}, {Node: "beta", Model: "qwen"}}}},
		map[string]int{"alpha": 90, "beta": 10}, "alpha", "beta")

	// Alpha is the preferred node and takes the first request with its one engine.
	first := candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi", "ollama/qwen"))
	if first.Node != "alpha" {
		t.Fatalf("first node = %q, want alpha", first.Node)
	}
	// Alpha's engine is held, so the next request lands on beta.
	second := candidateAcquire(t, led, candidateRequest(resolve, "t2", "pi", "ollama/qwen"))
	if second.Node != "beta" {
		t.Fatalf("second node = %q, want beta while alpha is busy", second.Node)
	}
	p := leasePools(second)
	if !p["node:beta"] || p["node:alpha"] || !p["host"] {
		t.Fatalf("second pools = %v, want host and node:beta only", second.Pools)
	}
}

func TestCandidateSkipsNodeNotServingModel(t *testing.T) {
	resolve, led := candidateSetup(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta", "gamma"},
			Models: []inference.EnabledModel{{Node: "alpha", Model: "llama"}, {Node: "beta", Model: "qwen"}, {Node: "gamma", Model: "qwen"}}}},
		map[string]int{"alpha": 99, "beta": 20, "gamma": 30}, "alpha", "beta", "gamma")

	_, _, choices := resolve("pi", "ollama/qwen")
	for _, c := range choices {
		if c.Name == "alpha" {
			t.Fatalf("choices = %+v, alpha does not serve qwen", choices)
		}
	}
	// Alpha has the top priority and is idle, yet never gets a qwen request.
	for i, holder := range []string{"t1", "t2"} {
		ls := candidateAcquire(t, led, candidateRequest(resolve, holder, "pi", "ollama/qwen"))
		want := []string{"gamma", "beta"}[i]
		if ls.Node != want {
			t.Fatalf("%s node = %q, want %q", holder, ls.Node, want)
		}
		if leasePools(ls)["node:alpha"] {
			t.Fatalf("%s pools = %v include node:alpha", holder, ls.Pools)
		}
	}
	if led.Held("t3") {
		t.Fatal("unexpected lease")
	}
	// Both serving nodes are full, so the next request waits rather than
	// spilling onto alpha, and alpha stays free.
	err := led.Acquire(context.Background(), candidateRequest(resolve, "t3", "pi", "ollama/qwen"), 30*time.Millisecond, nil, nil)
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("err = %v, want wait timeout", err)
	}
	if strings.Contains(err.Error(), "alpha") {
		t.Fatalf("wait reason %q names alpha", err)
	}
	// A llama request goes to alpha.
	if ls := candidateAcquire(t, led, candidateRequest(resolve, "t4", "pi", "ollama/llama")); ls.Node != "alpha" {
		t.Fatalf("llama node = %q, want alpha", ls.Node)
	}
}

func TestCandidateSingleNodeKeepsNodePool(t *testing.T) {
	resolve, led := candidateSetup(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha"}}},
		map[string]int{"alpha": 50}, "alpha")

	pools, node, choices := resolve("pi", "ollama/qwen")
	if node != "alpha" || choices != nil || !reflect.DeepEqual(pools, []string{"host", "node:alpha"}) {
		t.Fatalf("resolve = %v %q %v", pools, node, choices)
	}
	ls := candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi", "ollama/qwen"))
	if ls.Node != "alpha" || !leasePools(ls)["node:alpha"] {
		t.Fatalf("lease = %+v, want node alpha and its pool", ls)
	}
}

func TestCandidateEqualLoadPriorityThenListOrder(t *testing.T) {
	llm := func(nodes ...string) []*inference.LLM {
		return []*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: nodes}}
	}
	// Equal load: the higher scheduling priority wins, whatever the list order.
	resolve, led := candidateSetup(t, llm("alpha", "beta"), map[string]int{"alpha": 40, "beta": 70}, "alpha", "beta")
	if ls := candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi", "ollama/qwen")); ls.Node != "beta" {
		t.Fatalf("priority node = %q, want beta", ls.Node)
	}
	// Equal load and equal priority: list order wins, in either order.
	for _, order := range [][]string{{"alpha", "beta"}, {"beta", "alpha"}} {
		resolve, led := candidateSetup(t, llm(order...), map[string]int{"alpha": 60, "beta": 60}, "alpha", "beta")
		if ls := candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi", "ollama/qwen")); ls.Node != order[0] {
			t.Fatalf("list %v: node = %q, want %q", order, ls.Node, order[0])
		}
	}
}

func TestCandidateOneEnginePerNodeWaitNamesBothNodes(t *testing.T) {
	resolve, led := candidateSetup(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"}}},
		map[string]int{"alpha": 50, "beta": 50}, "alpha", "beta")
	candidateAcquire(t, led, candidateRequest(resolve, "t1", "pi", "ollama/qwen"))
	candidateAcquire(t, led, candidateRequest(resolve, "t2", "pi", "ollama/qwen"))

	var reasons []string
	err := led.Acquire(context.Background(), candidateRequest(resolve, "t3", "pi", "ollama/qwen"), 30*time.Millisecond, nil,
		func(r string) { reasons = append(reasons, r) })
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("err = %v, want wait timeout", err)
	}
	if led.Held("t3") {
		t.Fatal("t3 holds a lease with both nodes busy")
	}
	if len(reasons) == 0 {
		t.Fatal("onWait never called")
	}
	for _, text := range []string{reasons[0], err.Error()} {
		for _, want := range []string{"alpha", "beta", "t1", "t2"} {
			if !strings.Contains(text, want) {
				t.Fatalf("reason %q does not name %q", text, want)
			}
		}
	}
	// Each node still carries exactly one engine.
	perNode := map[string]int{}
	for _, ls := range led.Snapshot().Leases {
		perNode[ls.Node]++
	}
	if perNode["alpha"] != 1 || perNode["beta"] != 1 {
		t.Fatalf("leases per node = %v, want one engine each", perNode)
	}
	led.Release("t1")
	if ls := candidateAcquire(t, led, candidateRequest(resolve, "t3", "pi", "ollama/qwen")); ls.Node == "" {
		t.Fatal("t3 not placed after a release")
	}
}

func TestCandidateResolverChangesWhenComputeNodesReduced(t *testing.T) {
	prio := map[string]int{"alpha": 50, "beta": 50}
	two, _ := candidateSetup(t, []*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"}}}, prio)
	one, _ := candidateSetup(t, []*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha"}}}, prio)

	p2, n2, c2 := two("pi", "ollama/qwen")
	if n2 != "" || len(c2) != 2 || !reflect.DeepEqual(p2, []string{"host"}) {
		t.Fatalf("two nodes = %v %q %v", p2, n2, c2)
	}
	p1, n1, c1 := one("pi", "ollama/qwen")
	if n1 != "alpha" || c1 != nil || !reflect.DeepEqual(p1, []string{"host", "node:alpha"}) {
		t.Fatalf("one node = %v %q %v", p1, n1, c1)
	}
}
