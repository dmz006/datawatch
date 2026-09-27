package capacity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fast() Options { return Options{PollInterval: 10 * time.Millisecond} }

func TestAcquireWithinLimitAndRelease(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "t1", PRDID: "p1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	waited := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(ctx, Request{Holder: "t2", PRDID: "p2", Pools: []string{"node:a"}}, 2*time.Second, nil, func(r string) { waited <- r })
	}()
	select {
	case r := <-waited:
		if r == "" {
			t.Fatal("wait reason must name the pool")
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire should have to wait")
	}
	l.Release("t1")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !l.Held("t2") || l.Held("t1") {
		t.Fatal("lease ownership wrong")
	}
}

func TestAllOrNothingAcrossPools(t *testing.T) {
	l := New(fast())
	l.SetLimit("host", 5)
	l.SetLimit("node:a", 1)
	_ = l.Acquire(context.Background(), Request{Holder: "t1", Pools: []string{"node:a"}}, time.Second, nil, nil)
	err := l.Acquire(context.Background(), Request{Holder: "t2", Pools: []string{"host", "node:a"}}, 50*time.Millisecond, nil, nil)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	for _, p := range l.Snapshot().Pools {
		if p.Name == "host" && p.Held != 0 {
			t.Fatal("host slot leaked by a failed acquire")
		}
	}
}

func TestExternalUsageCountsAgainstPool(t *testing.T) {
	var ext int32 = 1
	o := fast()
	o.External = func(p string) int { return int(atomic.LoadInt32(&ext)) }
	l := New(o)
	l.SetLimit("host", 1)
	err := l.Acquire(context.Background(), Request{Holder: "t1", Pools: []string{"host"}}, 50*time.Millisecond, nil, nil)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("interactive session must occupy the slot: %v", err)
	}
	atomic.StoreInt32(&ext, 0)
	if err := l.Acquire(context.Background(), Request{Holder: "t1", Pools: []string{"host"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("slot freed externally must be picked up on the next poll: %v", err)
	}
}

func TestFairRoundRobinAcrossPRDs(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	ctx := context.Background()
	_ = l.Acquire(ctx, Request{Holder: "seed", PRDID: "A", Pools: []string{"node:a"}}, time.Second, nil, nil)
	var mu sync.Mutex
	var order []string
	var wg sync.WaitGroup
	launch := func(holder, prd string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Acquire(ctx, Request{Holder: holder, PRDID: prd, Pools: []string{"node:a"}}, 5*time.Second, nil, nil); err == nil {
				mu.Lock()
				order = append(order, holder)
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				l.Release(holder)
			}
		}()
		time.Sleep(15 * time.Millisecond) // deterministic FIFO seq
	}
	launch("a2", "A")
	launch("a3", "A")
	launch("b1", "B")
	l.Release("seed")
	wg.Wait()
	// PRD A was served last (seed), so B must go before A's second task.
	if len(order) != 3 || order[0] != "b1" {
		t.Fatalf("PRD B should be served first, got %v", order)
	}
}

func TestPriorityBeatsFairness(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 1)
	ctx := context.Background()
	_ = l.Acquire(ctx, Request{Holder: "seed", PRDID: "X", Pools: []string{"p"}}, time.Second, nil, nil)
	got := make(chan string, 2)
	go func() {
		_ = l.Acquire(ctx, Request{Holder: "low", PRDID: "L", Priority: 0, Pools: []string{"p"}}, 3*time.Second, nil, nil)
		got <- "low"
	}()
	time.Sleep(20 * time.Millisecond)
	go func() {
		_ = l.Acquire(ctx, Request{Holder: "high", PRDID: "H", Priority: 10, Pools: []string{"p"}}, 3*time.Second, nil, nil)
		got <- "high"
	}()
	time.Sleep(20 * time.Millisecond)
	l.Release("seed")
	if first := <-got; first != "high" {
		t.Fatalf("higher priority must go first, got %s", first)
	}
}

func TestModelAffinityPrefersLoadedModel(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	ctx := context.Background()
	_ = l.Acquire(ctx, Request{Holder: "seed", PRDID: "S", Node: "a", Model: "m1", Pools: []string{"node:a"}}, time.Second, nil, nil)
	got := make(chan string, 2)
	go func() {
		_ = l.Acquire(ctx, Request{Holder: "other", PRDID: "O", Node: "a", Model: "m2", Pools: []string{"node:a"}}, 3*time.Second, nil, nil)
		got <- "other"
	}()
	time.Sleep(20 * time.Millisecond)
	go func() {
		_ = l.Acquire(ctx, Request{Holder: "same", PRDID: "M", Node: "a", Model: "m1", Pools: []string{"node:a"}}, 3*time.Second, nil, nil)
		got <- "same"
	}()
	time.Sleep(20 * time.Millisecond)
	l.Release("seed")
	if first := <-got; first != "same" {
		t.Fatalf("waiter wanting the already-loaded model should go first, got %s", first)
	}
}

func TestBackpressureBlocksThenClears(t *testing.T) {
	var hot int32 = 1
	o := fast()
	o.Backpressure = func(p string) (bool, string) {
		if atomic.LoadInt32(&hot) == 1 {
			return true, "gpu 97% busy"
		}
		return false, ""
	}
	l := New(o)
	err := l.Acquire(context.Background(), Request{Holder: "t", Pools: []string{"node:a"}}, 40*time.Millisecond, nil, nil)
	if !errors.Is(err, ErrWaitTimeout) || err.Error() == "" {
		t.Fatalf("want timeout under backpressure, got %v", err)
	}
	atomic.StoreInt32(&hot, 0)
	if err := l.Acquire(context.Background(), Request{Holder: "t", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledWaiterLeavesQueue(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 1)
	_ = l.Acquire(context.Background(), Request{Holder: "seed", Pools: []string{"p"}}, time.Second, nil, nil)
	var cancelled int32
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "w", Pools: []string{"p"}}, 5*time.Second, func() bool { return atomic.LoadInt32(&cancelled) == 1 }, nil)
	}()
	time.Sleep(30 * time.Millisecond)
	atomic.StoreInt32(&cancelled, 1)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
	if len(l.Snapshot().Waiting) != 0 {
		t.Fatal("cancelled waiter must leave the queue")
	}
}

func TestReapReleasesDeadHolders(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 1)
	_ = l.Acquire(context.Background(), Request{Holder: "dead", Pools: []string{"p"}}, time.Second, nil, nil)
	if got := l.Reap(func(Lease) bool { return false }); len(got) != 1 || got[0] != "dead" {
		t.Fatalf("reaped %v", got)
	}
	if err := l.Acquire(context.Background(), Request{Holder: "n", Pools: []string{"p"}}, 100*time.Millisecond, nil, nil); err != nil {
		t.Fatalf("slot must be free after reap: %v", err)
	}
}

func TestSetLimitRaisesAdmitsWaiters(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 1)
	_ = l.Acquire(context.Background(), Request{Holder: "a", Pools: []string{"p"}}, time.Second, nil, nil)
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "b", Pools: []string{"p"}}, 2*time.Second, nil, nil)
	}()
	time.Sleep(30 * time.Millisecond)
	l.SetLimit("p", 2)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentNeverExceedsLimit(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 2)
	var cur, peak int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := string(rune('a' + i))
			if err := l.Acquire(context.Background(), Request{Holder: h, PRDID: h, Pools: []string{"p"}}, 10*time.Second, nil, nil); err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&cur, -1)
			l.Release(h)
		}(i)
	}
	wg.Wait()
	if peak > 2 {
		t.Fatalf("peak %d exceeded limit 2", peak)
	}
}

// Regression: the reaper callback used to run under the ledger lock, and the
// daemon's callback called Snapshot(), deadlocking the whole ledger (and every
// task admission behind it) the first time a lease existed at a sync tick.
func TestReapCallbackMayCallBackIntoLedger(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 2)
	_ = l.Acquire(context.Background(), Request{Holder: "a", Pools: []string{"p"}}, time.Second, nil, nil)
	done := make(chan []string, 1)
	go func() {
		done <- l.Reap(func(ls Lease) bool {
			_ = l.Snapshot() // must not deadlock
			return ls.Holder != "a"
		})
	}()
	select {
	case got := <-done:
		if len(got) != 1 || got[0] != "a" {
			t.Fatalf("reaped %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reap deadlocked when its callback re-entered the ledger")
	}
	if err := l.Acquire(context.Background(), Request{Holder: "b", Pools: []string{"p"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("ledger unusable after Reap: %v", err)
	}
}

func TestReapSkipsLeaseReplacedWhileCallbackRan(t *testing.T) {
	l := New(fast())
	l.SetLimit("p", 2)
	_ = l.Acquire(context.Background(), Request{Holder: "a", Pools: []string{"p"}}, time.Second, nil, nil)
	got := l.Reap(func(ls Lease) bool {
		l.Release("a")
		_ = l.Acquire(context.Background(), Request{Holder: "a", Pools: []string{"p"}}, time.Second, nil, nil)
		return false
	})
	if len(got) != 0 || !l.Held("a") {
		t.Fatalf("a re-acquired lease must survive a stale reap verdict: reaped=%v held=%v", got, l.Held("a"))
	}
}
