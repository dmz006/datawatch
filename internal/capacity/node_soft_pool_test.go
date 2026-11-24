package capacity

import (
	"errors"
	"testing"
	"time"
)

// Node engine pools (node:<name>, node-verify:<name>) are soft by default: how
// many models an engine can run at once is up to the user's hardware, so an
// operator start over the pool is admitted and records the overage, exactly as
// on any other pool. Workers keep waiting.

// An operator start over a full node pool is admitted; the lease records the
// pool, its limit and how far over it went.
func TestOperatorStartOverSoftNodePoolIsAdmittedAndRecordsOverage(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:gpu1", 1)
	if err := acq(t, l, Request{Holder: "w1", Pools: []string{"node:gpu1"}, Node: "gpu1"}, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"node:gpu1"}, Node: "gpu1"}, 50*time.Millisecond); err != nil {
		t.Fatalf("operator start over a soft node pool must be admitted: %v", err)
	}
	ls := l.LeaseOf("op1")
	if ls == nil || len(ls.Overage) != 1 {
		t.Fatalf("lease = %+v, want one overage record", ls)
	}
	if o := ls.Overage[0]; o.Pool != "node:gpu1" || o.Limit != 1 || o.Over != 1 {
		t.Fatalf("overage = %+v, want pool node:gpu1 limit 1 over 1", o)
	}
	for _, p := range l.Snapshot().Pools {
		if p.Name == "node:gpu1" && (p.Held != 2 || p.Over != 1) {
			t.Fatalf("pool = %+v, want held 2 over 1", p)
		}
	}
}

// The verifier pool of a node is soft in the same way.
func TestOperatorStartOverSoftNodeVerifyPoolIsAdmitted(t *testing.T) {
	l := New(fast())
	vp := VerifierPool("gpu1")
	l.SetLimit(vp, 1)
	if err := acq(t, l, Request{Holder: "v1", Pools: []string{vp}, Node: "gpu1"}, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{vp}, Node: "gpu1"}, 50*time.Millisecond); err != nil {
		t.Fatalf("operator start over a soft node-verify pool must be admitted: %v", err)
	}
	if ls := l.LeaseOf("op1"); ls == nil || len(ls.Overage) != 1 || ls.Overage[0].Pool != vp {
		t.Fatalf("lease = %+v, want one overage record on %s", ls, vp)
	}
}

// The overage notice fires for a node pool like for any other pool.
func TestOperatorOverSoftNodePoolRaisesOverageNotice(t *testing.T) {
	got := make(chan Lease, 1)
	opt := fast()
	opt.OnOverage = func(ls Lease) { got <- ls }
	l := New(opt)
	l.SetLimit("node:gpu1", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"node:gpu1"}, Node: "gpu1"}, time.Second)
	_ = acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"node:gpu1"}, Node: "gpu1"}, time.Second)
	select {
	case ls := <-got:
		if ls.Holder != "op1" || len(ls.Overage) != 1 || ls.Overage[0].Pool != "node:gpu1" {
			t.Fatalf("notice lease = %+v", ls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no overage notice for an operator lease over a node pool")
	}
}

// A worker still waits on a node pool, soft or not: the soft setting is for
// operator starts only. The operator's overage lease counts in the used figure.
func TestWorkerStillWaitsOnSoftNodePool(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:gpu1", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"node:gpu1"}, Node: "gpu1"}, time.Second)
	err := acq(t, l, Request{Holder: "w2", Pools: []string{"node:gpu1"}, Node: "gpu1"}, 60*time.Millisecond)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("worker err = %v, want wait timeout on a full node pool", err)
	}
	_ = acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"node:gpu1"}, Node: "gpu1"}, time.Second)
	l.Release("w1")
	err = acq(t, l, Request{Holder: "w3", Pools: []string{"node:gpu1"}, Node: "gpu1"}, 60*time.Millisecond)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("worker err = %v, want wait timeout while the operator holds the only engine", err)
	}
}

// An operator request with node choices still lands on a node with a free
// engine when one exists (the least loaded node ranks first), and goes over the
// least loaded node only when every candidate is full.
func TestOperatorMultiNodeStartPrefersFreeEngineThenOverages(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"node:a"}, Node: "a"}, time.Second)
	choices := []NodeChoice{{Name: "a", Priority: 9}, {Name: "b", Priority: 1}}
	if err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"host"}, NodeChoices: choices}, time.Second); err != nil {
		t.Fatal(err)
	}
	if ls := l.LeaseOf("op1"); ls == nil || ls.Node != "b" || len(ls.Overage) != 0 {
		t.Fatalf("lease = %+v, want node b with no overage", ls)
	}
	if err := acq(t, l, Request{Holder: "op2", Class: ClassOperator, Pools: []string{"host"}, NodeChoices: choices}, 50*time.Millisecond); err != nil {
		t.Fatalf("operator start with every node full must be admitted: %v", err)
	}
	if ls := l.LeaseOf("op2"); ls == nil || len(ls.Overage) != 1 || ls.Overage[0].Over != 1 {
		t.Fatalf("lease = %+v, want one overage record, one over", ls)
	}
}
