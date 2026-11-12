package capacity

import (
	"context"
	"testing"
	"time"
)

// cancelled and onWait are documented as running without the ledger lock, so
// they may call back into the ledger. A regression would deadlock here.
func TestAcquireCallbacksRunOutsideLock(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "t1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(ctx, Request{Holder: "t2", Pools: []string{"node:a"}}, 2*time.Second,
			func() bool { _ = l.Snapshot(); return false },
			func(string) { _ = l.Held("t1"); _ = l.Limit("node:a") })
	}()
	time.Sleep(50 * time.Millisecond)
	l.Release("t1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Acquire deadlocked: a callback ran under the ledger lock")
	}
}

// HeldBackpressure receives held and limit as arguments from the ledger's own
// state, so it never needs to read the ledger back.
func TestHeldBackpressureGetsLedgerValues(t *testing.T) {
	var gotHeld, gotLimit int
	opt := fast()
	opt.HeldBackpressure = func(pool string, held, limit int) (bool, string) {
		gotHeld, gotLimit = held, limit
		return false, ""
	}
	l := New(opt)
	l.SetLimit("node:a", 3)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "t1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(ctx, Request{Holder: "t2", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotHeld != 1 || gotLimit != 3 {
		t.Fatalf("HeldBackpressure saw held=%d limit=%d, want 1 and 3", gotHeld, gotLimit)
	}
}
