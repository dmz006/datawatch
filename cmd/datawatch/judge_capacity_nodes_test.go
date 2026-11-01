package main

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// A judge of an entry that pools two nodes is placed by the ledger on a node
// with room: with the first node's engine in use it takes the other, and with
// both in use it waits instead of being admitted onto either.
func TestJudgeAdmittersMultiNodePlacesOnIdleNodeOrWaits(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("node:alpha", 1)
	led.SetLimit("node:beta", 1)
	led.SetLimit("llm:pi-prompt", 2)
	keys := func(string, string) ([]string, string) { return []string{"host", "llm:pi-prompt"}, "" }
	choices := func(string, string) []capacity.NodeChoice {
		return []capacity.NodeChoice{{Name: "alpha", Priority: 5}, {Name: "beta", Priority: 1}}
	}
	admit, try, _ := judgeAdmitters(led, newVerifierRegistry(), keys, 200*time.Millisecond, choices)

	// A worker already holds alpha.
	led.Adopt(capacity.Request{Holder: "worker", PRDID: "p", Pools: []string{"host", "llm:pi-prompt"}, Node: "alpha"}, "sess-w")

	release, err := admit(context.Background(), "pi-prompt", "m", "unit-1", "p")
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if ls := led.LeaseOf("judge:unit-1"); ls == nil || ls.Node != "beta" {
		t.Fatalf("judge lease = %+v, want it placed on beta: alpha is in use", ls)
	}
	for _, p := range led.Snapshot().Pools {
		if len(p.Name) > 5 && p.Name[:5] == "node:" && p.Held+p.External > p.Limit {
			t.Errorf("%s is over its limit: %d/%d", p.Name, p.Held+p.External, p.Limit)
		}
	}

	// Both nodes are in use now: a second judge waits and times out, and the probe says no.
	if _, err := admit(context.Background(), "pi-prompt", "m", "unit-2", "p"); err == nil {
		t.Fatal("second judge was admitted with both nodes in use")
	}
	if _, ok := try(context.Background(), "pi-prompt", "m", "unit-3", "p"); ok {
		t.Fatal("the probe admitted a judge with both nodes in use")
	}
	if led.Held("judge:unit-2") || led.Held("judge:unit-3") {
		t.Fatal("a refused judge holds a lease")
	}

	release()
	if _, ok := try(context.Background(), "pi-prompt", "m", "unit-3", "p"); !ok {
		t.Fatal("the probe refused a judge once beta was free")
	}
}
