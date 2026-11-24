package capacity

import (
	"errors"
	"testing"
	"time"
)

// A node whose operator turned hard_limit on keeps the old behaviour for every
// class: its engine pools (node: and node-verify:) are a physical limit and an
// operator start waits for a free engine.

func TestOperatorStartWaitsOnHardLimitNodePool(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:gpu1", 1)
	l.SetHardLimit("gpu1", true)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"node:gpu1"}, Node: "gpu1"}, time.Second)
	err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"node:gpu1"}, Node: "gpu1"}, 60*time.Millisecond)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("operator err = %v, want wait timeout on a hard-limit node", err)
	}
	if ls := l.LeaseOf("op1"); ls != nil {
		t.Fatalf("operator holds a lease %+v on a hard-limit node", ls)
	}
	// It starts as soon as an engine frees up, with no overage.
	done := make(chan error, 1)
	go func() {
		done <- acq(t, l, Request{Holder: "op2", Class: ClassOperator, Pools: []string{"node:gpu1"}, Node: "gpu1"}, 2*time.Second)
	}()
	time.Sleep(30 * time.Millisecond)
	l.Release("w1")
	if err := <-done; err != nil {
		t.Fatalf("operator after release: %v", err)
	}
	if ls := l.LeaseOf("op2"); ls == nil || len(ls.Overage) != 0 {
		t.Fatalf("lease = %+v, want a lease with no overage", ls)
	}
}

func TestOperatorStartWaitsOnHardLimitNodeVerifyPool(t *testing.T) {
	l := New(fast())
	vp := VerifierPool("gpu1")
	l.SetLimit(vp, 1)
	l.SetHardLimit("gpu1", true)
	_ = acq(t, l, Request{Holder: "v1", Pools: []string{vp}, Node: "gpu1"}, time.Second)
	err := acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{vp}, Node: "gpu1"}, 60*time.Millisecond)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("operator err = %v, want wait timeout on a hard-limit node verifier pool", err)
	}
}

// hard_limit is per node: another node of the same ledger stays soft, and the
// setting can be switched off again live.
func TestHardLimitIsPerNodeAndRevocable(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	l.SetHardLimit("a", true)
	_ = acq(t, l, Request{Holder: "wa", Pools: []string{"node:a"}, Node: "a"}, time.Second)
	_ = acq(t, l, Request{Holder: "wb", Pools: []string{"node:b"}, Node: "b"}, time.Second)
	if err := acq(t, l, Request{Holder: "opb", Class: ClassOperator, Pools: []string{"node:b"}, Node: "b"}, 50*time.Millisecond); err != nil {
		t.Fatalf("soft node b: %v", err)
	}
	if err := acq(t, l, Request{Holder: "opa", Class: ClassOperator, Pools: []string{"node:a"}, Node: "a"}, 50*time.Millisecond); !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("hard node a: err = %v, want wait timeout", err)
	}
	l.SetHardLimit("a", false)
	if err := acq(t, l, Request{Holder: "opa", Class: ClassOperator, Pools: []string{"node:a"}, Node: "a"}, 50*time.Millisecond); err != nil {
		t.Fatalf("node a after hard_limit off: %v", err)
	}
	if !l.HardLimit("b") && l.HardLimit("a") {
		t.Fatal("HardLimit reads back the wrong node")
	}
}

// Workers wait on a node pool whatever hard_limit says, so a soft cap does not
// change where they are placed. The same worker sequence on a ledger whose
// nodes are soft and on one whose nodes are hard gives the same placement and
// the same refusals, step by step.
func TestSoftNodeCapDoesNotChangeWorkerPlacement(t *testing.T) {
	run := func(hard bool) []string {
		l := New(fast())
		l.SetLimit("node:a", 2)
		l.SetLimit("node:b", 1)
		l.SetLimit("node:c", 1)
		if hard {
			l.SetHardLimit("a", true)
			l.SetHardLimit("b", true)
			l.SetHardLimit("c", true)
		}
		choices := []NodeChoice{{Name: "a", Priority: 1}, {Name: "b", Priority: 5}, {Name: "c", Priority: 3}}
		var out []string
		for _, h := range []string{"w1", "w2", "w3", "w4", "w5", "w6"} {
			err := acq(t, l, Request{Holder: h, PRDID: "p", Pools: []string{"host"}, Model: "m", NodeChoices: choices}, 20*time.Millisecond)
			if err != nil {
				out = append(out, h+":"+err.Error())
				continue
			}
			out = append(out, h+":"+l.LeaseOf(h).Node)
		}
		return out
	}
	soft, hard := run(false), run(true)
	if len(soft) != len(hard) {
		t.Fatalf("soft %v, hard %v", soft, hard)
	}
	for i := range soft {
		if soft[i] != hard[i] {
			t.Fatalf("step %d: soft %q, hard %q (soft %v, hard %v)", i, soft[i], hard[i], soft, hard)
		}
	}
	// Four engines in all: the fifth and sixth worker wait on both.
	if got := soft[4]; got == "w5:a" || got == "w5:b" || got == "w5:c" {
		t.Fatalf("fifth worker was placed: %v", soft)
	}
}

// With an operator over the cap of node a, a worker is placed on the node that
// has room, exactly as it would be if that engine were a hard limit.
func TestWorkerPlacementSkipsNodeHeldOverCapByOperator(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	_ = acq(t, l, Request{Holder: "w1", Pools: []string{"node:a"}, Node: "a"}, time.Second)
	_ = acq(t, l, Request{Holder: "op1", Class: ClassOperator, Pools: []string{"node:a"}, Node: "a"}, time.Second)
	choices := []NodeChoice{{Name: "a", Priority: 9}, {Name: "b", Priority: 1}}
	if err := acq(t, l, Request{Holder: "w2", Pools: []string{"host"}, NodeChoices: choices}, time.Second); err != nil {
		t.Fatal(err)
	}
	if ls := l.LeaseOf("w2"); ls == nil || ls.Node != "b" {
		t.Fatalf("lease = %+v, want the worker on node b", ls)
	}
}
