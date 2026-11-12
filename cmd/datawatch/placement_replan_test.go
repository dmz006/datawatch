package main

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// replanRig is a real ledger, inference registry and compute registry wired
// the way wireCapacity does: waiters carry resolveRefresh, and an edit to the
// registry re-plans through replanOnPlacementChange. The poll interval is long
// enough that only the re-plan can move a waiter during the test.
type replanRig struct {
	t       *testing.T
	led     *capacity.Ledger
	reg     *inference.Registry
	resolve func(backend, model string) ([]string, string, []capacity.NodeChoice)
	ticker  func()
}

func newReplanRig(t *testing.T, listed []string) *replanRig {
	t.Helper()
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: listed}},
		map[string]int{"alpha": 50, "beta": 50, "gamma": 50})
	led := capacity.New(capacity.Options{PollInterval: time.Hour})
	for _, n := range []string{"alpha", "beta", "gamma"} {
		led.SetLimit("node:"+n, 1)
	}
	regFn := func() *inference.Registry { return reg }
	nodesFn := func() *compute.Registry { return creg }
	rig := &replanRig{t: t, led: led, reg: reg, resolve: capacityResolver(regFn, nodesFn)}
	rig.ticker = replanOnPlacementChange(led, regFn, nodesFn)
	return rig
}

func (r *replanRig) setList(nodes ...string) {
	r.t.Helper()
	llm, err := r.reg.Get("ollama")
	if err != nil {
		r.t.Fatal(err)
	}
	llm.ComputeNodes = nodes
	if err := r.reg.Update(llm); err != nil {
		r.t.Fatal(err)
	}
}

// hold takes a lease on one node, as a running session there would.
func (r *replanRig) hold(holder, node string) {
	r.t.Helper()
	req := capacity.Request{Holder: holder, PRDID: "prd-0", Pools: []string{"node:" + node}, Node: node}
	if err := r.led.Acquire(context.Background(), req, 0, nil, nil); err != nil {
		r.t.Fatalf("hold %s: %v", holder, err)
	}
}

// queue starts a blocking admission for holder built the way wireCapacity
// builds one, and waits until it is queued.
func (r *replanRig) queue(holder string) <-chan error {
	r.t.Helper()
	pools, node, choices := r.resolve("ollama", "ollama/qwen")
	req := capacity.Request{Holder: holder, PRDID: "prd-1", Pools: pools, Node: node, Model: "ollama/qwen"}
	req = withNodeChoices(req, choices)
	req.Refresh = resolveRefresh(r.resolve, req, "ollama", "ollama/qwen")
	done := make(chan error, 1)
	go func() {
		done <- r.led.Acquire(context.Background(), req, 0, func() bool { return false }, func(string) {})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, w := range r.led.Snapshot().Waiting {
			if w.Holder == holder {
				return done
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("%s never queued", holder)
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *replanRig) expectOn(done <-chan error, holder, node string) {
	r.t.Helper()
	select {
	case err := <-done:
		if err != nil {
			r.t.Fatalf("%s: Acquire: %v", holder, err)
		}
	case <-time.After(2 * time.Second):
		r.t.Fatalf("%s still waiting, want it granted on %s", holder, node)
	}
	if ls := r.led.LeaseOf(holder); ls == nil || ls.Node != node {
		r.t.Fatalf("%s lease = %+v, want node %s", holder, ls, node)
	}
}

func (r *replanRig) expectWaiting(holder string) {
	r.t.Helper()
	time.Sleep(50 * time.Millisecond)
	if r.led.Held(holder) {
		r.t.Fatalf("%s holds a lease, want it still waiting", holder)
	}
}

// An llm update that adds nodes to compute_nodes moves the waiters queued on
// the first node onto the new ones at once, one per node.
func TestListEditSpreadsWaitersOverAddedNodes(t *testing.T) {
	r := newReplanRig(t, []string{"alpha"})
	r.reg.SetChangeFn(r.ticker)
	r.hold("running", "alpha")
	w1, w2 := r.queue("w1"), r.queue("w2")
	r.expectWaiting("w1")
	r.expectWaiting("w2")

	r.setList("alpha", "beta", "gamma")

	for _, w := range []struct {
		done <-chan error
		id   string
	}{{w1, "w1"}, {w2, "w2"}} {
		select {
		case err := <-w.done:
			if err != nil {
				t.Fatalf("%s: %v", w.id, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s still waiting after the list gained two nodes", w.id)
		}
	}
	on := map[string]int{}
	on[r.led.LeaseOf("w1").Node]++
	on[r.led.LeaseOf("w2").Node]++
	if on["beta"] != 1 || on["gamma"] != 1 {
		t.Fatalf("waiters landed on %v, want one on beta and one on gamma", on)
	}
}

// An llm update that drops a node moves its waiters to a listed node.
func TestListEditMovesWaitersOffRemovedNode(t *testing.T) {
	r := newReplanRig(t, []string{"alpha", "beta"})
	r.reg.SetChangeFn(r.ticker)
	r.hold("run-alpha", "alpha")
	r.hold("run-beta", "beta")
	w := r.queue("w1")
	r.expectWaiting("w1")

	r.setList("beta", "gamma")
	r.expectOn(w, "w1", "gamma")
}

// A waiter whose node is still listed stays queued when the edit changes
// nothing that matters to it.
func TestListEditKeepsWaiterOnListedNode(t *testing.T) {
	r := newReplanRig(t, []string{"alpha", "beta"})
	r.reg.SetChangeFn(r.ticker)
	r.hold("run-alpha", "alpha")
	r.hold("run-beta", "beta")
	w := r.queue("w1")

	r.setList("alpha", "beta", "ghost") // ghost is unknown to the compute registry
	r.expectWaiting("w1")
	r.led.Release("run-alpha")
	r.expectOn(w, "w1", "alpha")
}

// With no registry hook the ticker alone re-plans when the resolved candidates
// differ, and does nothing while they match.
func TestPlacementTickerReplansOnlyOnChange(t *testing.T) {
	r := newReplanRig(t, []string{"alpha"})
	r.hold("running", "alpha")
	w := r.queue("w1")

	r.ticker() // nothing changed
	r.expectWaiting("w1")

	r.setList("alpha", "beta")
	r.expectWaiting("w1") // no hook: still queued until the ticker runs
	r.ticker()
	r.expectOn(w, "w1", "beta")
}

// Editing a node's priority in the compute registry changes the signature the
// ticker compares.
func TestPlacementSignatureTracksListAndPriority(t *testing.T) {
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"}}},
		map[string]int{"alpha": 10, "beta": 20})
	regFn := func() *inference.Registry { return reg }
	nodesFn := func() *compute.Registry { return creg }
	base := placementSignature(regFn, nodesFn)
	if again := placementSignature(regFn, nodesFn); again != base {
		t.Fatalf("signature changed with no edit: %q vs %q", base, again)
	}
	llm, _ := reg.Get("ollama")
	llm.ComputeNodes = []string{"beta"}
	if err := reg.Update(llm); err != nil {
		t.Fatal(err)
	}
	if placementSignature(regFn, nodesFn) == base {
		t.Fatal("signature unchanged after the list lost a node")
	}
}
