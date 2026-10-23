package capacity

import (
	"context"
	"sync"
	"testing"
	"time"
)

// A HeldBackpressure callback runs while the ledger holds its own lock. The
// regression this guards: a callback that read the ledger back (Snapshot)
// deadlocked every capacity call. The ledger now hands the callback the held
// count and the limit, so a waiter on a full pool is dispatched and the
// ledger stays usable.
func TestHeldBackpressureReceivesHeldAndLimitAndDoesNotDeadlock(t *testing.T) {
	var mu sync.Mutex
	var sawFull bool
	l := New(Options{
		HeldBackpressure: func(pool string, held, limit int) (bool, string) {
			mu.Lock()
			defer mu.Unlock()
			if pool == "node:a" && held == 0 && limit == 1 {
				sawFull = true
			}
			return false, ""
		},
	})
	l.SetLimit("node:a", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := l.Acquire(ctx, Request{Holder: "t1", PRDID: "p1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	done := make(chan error, 1)
	waited := make(chan string, 4)
	go func() {
		done <- l.Acquire(ctx, Request{Holder: "t2", PRDID: "p2", Pools: []string{"node:a"}}, 6*time.Second, nil, func(r string) { waited <- r })
	}()
	select {
	case <-waited:
	case <-time.After(4 * time.Second):
		t.Fatal("second request never started waiting")
	}

	snap := make(chan struct{})
	go func() { _ = l.Snapshot(); close(snap) }()
	select {
	case <-snap:
	case <-time.After(4 * time.Second):
		t.Fatal("Snapshot blocked: the ledger is deadlocked")
	}

	l.Release("t1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second acquire after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter was not granted after the release")
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawFull {
		t.Fatal("HeldBackpressure was never given held=0 limit=1 for node:a (it runs only while the pool has room)")
	}
}
