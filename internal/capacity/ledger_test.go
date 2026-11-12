package capacity

import (
	"context"
	"errors"
	"strings"
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

// BL462 F1: a session that survived a daemon restart re-takes its lease
// without waiting, even when the pool is already at its limit, so a new
// request for the same pool still queues behind it.
func TestAdoptRestoresLeaseOverLimitWithoutWaiting(t *testing.T) {
	led := New(fast())
	led.SetLimit("host", 1)
	led.Adopt(Request{Holder: "task-a", PRDID: "p", Pools: []string{"host"}}, "sess-a")
	led.Adopt(Request{Holder: "task-b", PRDID: "p", Pools: []string{"host"}}, "sess-b")

	st := led.Snapshot()
	if len(st.Leases) != 2 {
		t.Fatalf("want 2 adopted leases, got %+v", st.Leases)
	}
	if st.Pools[0].Held != 2 {
		t.Fatalf("host held = %d, want 2", st.Pools[0].Held)
	}
	for _, ls := range st.Leases {
		if ls.SessionID == "" {
			t.Fatalf("adopted lease %s lost its session binding", ls.Holder)
		}
	}

	err := led.Acquire(context.Background(), Request{Holder: "task-c", PRDID: "p", Pools: []string{"host"}}, 50*time.Millisecond, nil, nil)
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("new request must wait behind adopted leases, got %v", err)
	}
}

// Adopt is idempotent: repeating it keeps one lease and refreshes only the
// session binding, and Release frees it exactly once.
func TestAdoptIsIdempotent(t *testing.T) {
	led := New(fast())
	req := Request{Holder: "task-a", PRDID: "p", Pools: []string{"host"}}
	led.Adopt(req, "")
	led.Adopt(req, "sess-a")
	led.Adopt(req, "sess-a")

	st := led.Snapshot()
	if len(st.Leases) != 1 || st.Leases[0].SessionID != "sess-a" {
		t.Fatalf("want one lease bound to sess-a, got %+v", st.Leases)
	}
	led.Release("task-a")
	if led.Held("task-a") {
		t.Fatal("lease still held after release")
	}
}

// waitQueued polls until n requests are waiting, so the test controls the
// order in which waiters enter the queue instead of relying on sleeps.
func waitQueued(t *testing.T, l *Ledger, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(l.Snapshot().Waiting) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d waiters queued", len(l.Snapshot().Waiting), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// Role ranks a waiter ahead of others in the same pool: verifier before
// worker before planner, ahead of priority. A zero-value role is a worker.
func TestRoleOrdersWaitersWhenPoolFrees(t *testing.T) {
	tests := []struct {
		name  string
		queue []Request // queued in this order behind a full pool
		want  string    // holder granted first once capacity frees
	}{
		{
			name: "verifier queued after planner is granted first",
			queue: []Request{
				{Holder: "plan", PRDID: "P", Role: RolePlanner},
				{Holder: "verify", PRDID: "V", Role: RoleVerifier},
			},
			want: "verify",
		},
		{
			name: "worker queued after planner outranks it",
			queue: []Request{
				{Holder: "plan", PRDID: "P", Role: RolePlanner},
				{Holder: "work", PRDID: "W", Role: RoleWorker},
			},
			want: "work",
		},
		{
			name: "verifier queued after worker is granted first",
			queue: []Request{
				{Holder: "work", PRDID: "W", Role: RoleWorker},
				{Holder: "verify", PRDID: "V", Role: RoleVerifier},
			},
			want: "verify",
		},
		{
			name: "role beats priority: low-priority worker beats high-priority planner",
			queue: []Request{
				{Holder: "plan", PRDID: "P", Priority: 10, Role: RolePlanner},
				{Holder: "work", PRDID: "W", Priority: 0, Role: RoleWorker},
			},
			want: "work",
		},
		{
			name: "zero-value role ranks as worker, ahead of planner",
			queue: []Request{
				{Holder: "plan", PRDID: "P", Role: RolePlanner},
				{Holder: "legacy", PRDID: "L"},
			},
			want: "legacy",
		},
		{
			name: "same role falls through to priority",
			queue: []Request{
				{Holder: "low", PRDID: "L", Priority: 0, Role: RoleVerifier},
				{Holder: "high", PRDID: "H", Priority: 5, Role: RoleVerifier},
			},
			want: "high",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := New(fast())
			l.SetLimit("p", 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := l.Acquire(ctx, Request{Holder: "seed", PRDID: "S", Pools: []string{"p"}}, time.Second, nil, nil); err != nil {
				t.Fatal(err)
			}
			granted := make(chan string, len(tc.queue))
			for i, req := range tc.queue {
				req.Pools = []string{"p"}
				go func(req Request) {
					if err := l.Acquire(ctx, req, 5*time.Second, nil, nil); err == nil {
						granted <- req.Holder
					}
				}(req)
				waitQueued(t, l, i+1)
			}
			l.Release("seed")
			if first := <-granted; first != tc.want {
				t.Fatalf("granted %s first, want %s", first, tc.want)
			}
		})
	}
}

func TestBlockReasonNodePoolNamesNode(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "t1", PRDID: "p1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	var reason string
	err := l.Acquire(ctx, Request{Holder: "t2", PRDID: "p2", Pools: []string{"node:a"}}, 50*time.Millisecond, nil, func(r string) { reason = r })
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	want := "waiting for node a: 1/1 engines in use, held by t1"
	if reason != want {
		t.Fatalf("reason = %q, want %q", reason, want)
	}
}

func TestBlockReasonHostPoolUnchanged(t *testing.T) {
	l := New(fast())
	l.SetLimit("host", 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "t1", PRDID: "p1", Pools: []string{"host"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	var reason string
	err := l.Acquire(ctx, Request{Holder: "t2", PRDID: "p2", Pools: []string{"host"}}, 50*time.Millisecond, nil, func(r string) { reason = r })
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	want := "pool host full (1/1, held by t1)"
	if reason != want {
		t.Fatalf("reason = %q, want %q", reason, want)
	}
}

func TestAutonomousBackpressureSkipsInteractive(t *testing.T) {
	o := Options{PollInterval: time.Millisecond}
	var block atomic.Bool
	block.Store(true)
	o.AutonomousBackpressure = func(p string) (bool, string) { return block.Load(), "backed off" }
	l := New(o)

	if err := l.Acquire(context.Background(), Request{Holder: "op", Pools: []string{"llm:a"}, Interactive: true}, time.Second, nil, nil); err != nil {
		t.Fatalf("interactive request must skip autonomous backpressure: %v", err)
	}
	err := l.Acquire(context.Background(), Request{Holder: "task", Pools: []string{"llm:a"}}, 30*time.Millisecond, nil, nil)
	if !errors.Is(err, ErrWaitTimeout) || !strings.Contains(err.Error(), "backed off") {
		t.Fatalf("autonomous request should time out blocked by the back-off, got %v", err)
	}
	block.Store(false)
	if err := l.Acquire(context.Background(), Request{Holder: "task", Pools: []string{"llm:a"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("admitted once the back-off clears: %v", err)
	}
}

// Three sessions that survived a restart are adopted over a pool whose cap is
// 2. Each holds one lease and is counted once, so the pool reads 3 held
// against 2, names all three holders, and a new request waits with a reason
// that names the pool.
func TestAdoptOverCapCountsEachSessionOnce(t *testing.T) {
	led := New(fast())
	led.SetLimit("pi-prompt", 2)
	for _, h := range []string{"task-a", "task-b", "task-c"} {
		led.Adopt(Request{Holder: h, PRDID: "p", Pools: []string{"host", "pi-prompt"}}, "sess-"+h)
	}
	// A second pass over the same sessions changes nothing.
	for _, h := range []string{"task-a", "task-b", "task-c"} {
		led.Adopt(Request{Holder: h, PRDID: "p", Pools: []string{"host", "pi-prompt"}}, "sess-"+h)
	}

	st := led.Snapshot()
	if len(st.Leases) != 3 {
		t.Fatalf("want 3 leases, got %+v", st.Leases)
	}
	var pi *PoolStatus
	for i := range st.Pools {
		if st.Pools[i].Name == "pi-prompt" {
			pi = &st.Pools[i]
		}
	}
	if pi == nil || pi.Held != 3 || pi.Limit != 2 {
		t.Fatalf("pi-prompt pool = %+v, want 3 held against a limit of 2", pi)
	}
	if got := strings.Join(pi.Holders, ","); got != "task-a,task-b,task-c" {
		t.Fatalf("pool holders = %q, want all three named", got)
	}

	err := led.Acquire(context.Background(), Request{Holder: "task-d", PRDID: "p", Pools: []string{"host", "pi-prompt"}}, 50*time.Millisecond, nil, nil)
	if !errors.Is(err, ErrWaitTimeout) || !strings.Contains(err.Error(), "pool pi-prompt full (3/2") {
		t.Fatalf("new admission must wait naming the pool, got %v", err)
	}
}

// Adopting a session whose lease was already re-keyed by a hand-off adds
// nothing, while an unrelated holder is still adopted.
func TestAdoptSkipsHolderThatWasHandedOff(t *testing.T) {
	led := New(fast())
	led.Adopt(Request{Holder: "task-a", PRDID: "p", Pools: []string{"pi-prompt"}}, "sess-a")
	if !led.Handoff("task-a", "verify:task-a") {
		t.Fatal("hand-off failed")
	}
	led.Adopt(Request{Holder: "task-a", PRDID: "p", Pools: []string{"pi-prompt"}}, "sess-a", "verify:task-a")
	led.Adopt(Request{Holder: "task-b", PRDID: "p", Pools: []string{"pi-prompt"}}, "sess-b", "verify:task-b")

	st := led.Snapshot()
	if len(st.Leases) != 2 || led.Held("task-a") || !led.Held("verify:task-a") || !led.Held("task-b") {
		t.Fatalf("want verify:task-a and task-b only, got %+v", st.Leases)
	}
}

// A hand-off moves the lease to the verifier's holder name without changing
// the count: three sessions stay three leases and three slots held.
func TestHandoffKeepsAdoptedCountAtThree(t *testing.T) {
	led := New(fast())
	led.SetLimit("pi-prompt", 2)
	for _, h := range []string{"task-a", "task-b", "task-c"} {
		led.Adopt(Request{Holder: h, PRDID: "p", Pools: []string{"pi-prompt"}}, "sess-"+h)
	}
	for _, h := range []string{"task-a", "task-b", "task-c"} {
		if !led.Handoff(h, "verify:"+h) {
			t.Fatalf("hand-off of %s failed", h)
		}
	}
	st := led.Snapshot()
	if len(st.Leases) != 3 || st.Pools[0].Held != 3 {
		t.Fatalf("after hand-off want 3 leases and 3 held, got leases=%+v pools=%+v", st.Leases, st.Pools)
	}
}
