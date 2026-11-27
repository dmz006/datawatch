package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/classifier"
)

func classifyGateFixture(t *testing.T) (*ledgerGate, *capacity.Ledger) {
	t.Helper()
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:n1", 1)
	d := &classifyDeps{}
	d.ledger.Store(led)
	return &ledgerGate{led: &d.ledger}, led
}

func classifyHold(t *testing.T, led *capacity.Ledger, holder, node string) {
	t.Helper()
	req := capacity.Request{Holder: holder, Pools: []string{"node:" + node}, Node: node, Role: capacity.RoleVerifier}
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("hold %s: %v", holder, err)
	}
}

// gateResult runs one gate acquire and gives up on it after limit.
func gateResult(t *testing.T, g *ledgerGate, ctx context.Context, limit time.Duration) (func(), error) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	type res struct {
		release func()
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		release, err := g.Acquire(ctx, "n1", "m")
		ch <- res{release, err}
	}()
	select {
	case r := <-ch:
		t.Cleanup(cancel)
		return r.release, r.err
	case <-time.After(limit):
		cancel()
		<-ch
		t.Fatalf("the classify wait for a node slot did not end within %s", limit)
		return nil, nil
	}
}

// A verifier that holds the node's one slot runs its classify call on it
// instead of waiting for itself.
func TestClassifyGateRunsOnTheCallersOwnNodeSlot(t *testing.T) {
	g, led := classifyGateFixture(t)
	holder := autonomous.VerifyHolder("t1")
	classifyHold(t, led, holder, "n1")
	release, err := gateResult(t, g, classifier.WithHeldSlot(context.Background(), holder), 3*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if n := len(led.Leases()); n != 1 {
		t.Fatalf("%d leases on a one-slot node, want 1", n)
	}
	release()
	if !led.Held(holder) {
		t.Fatal("the classify release freed the verifier's own slot")
	}
}

// A caller whose slot is on another node takes this node's slot as before.
func TestClassifyGateHeldSlotOnAnotherNodeStillTakesASlot(t *testing.T) {
	g, led := classifyGateFixture(t)
	holder := autonomous.VerifyHolder("t1")
	classifyHold(t, led, holder, "n2")
	release, err := gateResult(t, g, classifier.WithHeldSlot(context.Background(), holder), 3*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if n := len(led.Leases()); n != 2 {
		t.Fatalf("%d leases, want the verifier's and the classify call's", n)
	}
	release()
	if n := len(led.Leases()); n != 1 {
		t.Fatalf("%d leases after release, want 1", n)
	}
}

// The wait for another holder's slot has a bound.
func TestClassifyGateWaitIsBounded(t *testing.T) {
	g, led := classifyGateFixture(t)
	g.wait = func() time.Duration { return 150 * time.Millisecond }
	classifyHold(t, led, "other", "n1")
	_, err := gateResult(t, g, context.Background(), 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), capacity.ErrWaitTimeout.Error()) {
		t.Fatalf("err = %v, want a capacity wait timeout", err)
	}
	if w := led.Snapshot().Waiting; len(w) != 0 {
		t.Fatalf("a timed-out classify request is still queued: %v", w)
	}
}
