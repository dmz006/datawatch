package capacity

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetarget_MovesQueueEntryToNewPool pins the queued-request re-queue
// behavior: one holder's live wait is cancelled on its old pool and
// re-queued on the new pool, keeping exactly one request for it.
func TestRetarget_MovesQueueEntryToNewPool(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	ctx := context.Background()

	// t1 holds node:a's only slot; t2 is queued for it; t3 holds b so the
	// moved entry stays queued instead of being granted.
	if err := l.Acquire(ctx, Request{Holder: "t1", PRDID: "p1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(ctx, Request{Holder: "t3", PRDID: "p3", Pools: []string{"node:b"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(ctx, Request{Holder: "t2", PRDID: "p2", Pools: []string{"node:a"}}, 3*time.Second, nil, nil)
	}()
	waitForWaiters(t, l, 1)
	if got := l.waitersCount("t2"); got != 1 {
		t.Fatalf("t2 waiters before retarget: %d, want 1", got)
	}

	if !l.Retarget("t2", Request{Holder: "t2", PRDID: "p2", Pools: []string{"node:b"}}) {
		t.Fatal("Retarget t2 to node:b: want true")
	}
	if got := l.waitersCount("t2"); got != 1 {
		t.Fatalf("t2 waiters after retarget: %d, want exactly 1", got)
	}
	if got := l.waitersCount("t1"); got != 0 {
		t.Fatalf("t1 waiters: %d, want 0", got)
	}

	// t1 frees node:a: t2 must NOT be granted — it no longer waits on a.
	l.Release("t1")
	time.Sleep(100 * time.Millisecond)
	if got := l.waitersCount("t2"); got != 1 {
		t.Fatalf("t2 waiters after node:a freed: %d, want 1 (it waits on node:b)", got)
	}
	if l.Held("t2") {
		t.Fatal("t2 granted on the old pool right after the retarget")
	}

	// t3 frees node:b: t2 is granted exactly once there — proof the entry moved.
	l.Release("t3")
	select {
	case err := <-done:
		if err == nil {
			// Grant completed: t2 now holds its lease exactly once.
			if !l.Held("t2") {
				t.Fatal("t2 should hold its lease after the grant")
			}
			if got := l.waitersCount("t2"); got != 0 {
				t.Fatalf("t2 waiters after grant: %d, want 0", got)
			}
		} else if !errors.Is(err, ErrWaitTimeout) {
			t.Fatalf("unexpected error: %v", err)
		} else {
			t.Fatal("t2 timed out despite being retargeted to a free pool")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("t2 grant should complete after the retarget")
	}
}

// TestRetarget_SamePlacementIsNoop pins that a re-queue request that
// already matches the queue entry is a no-op — retargeting the same task
// twice leaves exactly one live request on the current pool.
func TestRetarget_SamePlacementIsNoop(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	if err := l.Acquire(context.Background(), Request{Holder: "t1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(context.Background(), Request{Holder: "t3", Pools: []string{"node:b"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = l.Acquire(context.Background(), Request{Holder: "t2", Pools: []string{"node:a"}}, 3*time.Second, nil, nil)
	}()
	waitForWaiters(t, l, 1)

	req := Request{Holder: "t2", Pools: []string{"node:b"}}
	if !l.Retarget("t2", req) {
		t.Fatal("first retarget: want true")
	}
	if l.Retarget("t2", req) {
		t.Fatal("identical retarget: want false (no-op)")
	}
	if got := l.waitersCount("t2"); got != 1 {
		t.Fatalf("t2 waiters after identical retarget: %d, want exactly 1", got)
	}
}

// TestRetarget_RefusesUnknownAndHeld pins the two refusal cases: a holder
// with no live request (unknown) and a holder that has already been
// granted (holding a lease) are both left alone.
func TestRetarget_RefusesUnknownAndHeld(t *testing.T) {
	l := New(fast())
	l.SetLimit("node:a", 2)
	if err := l.Acquire(context.Background(), Request{Holder: "t1", Pools: []string{"node:a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	if l.Retarget("ghost", Request{Holder: "ghost", Pools: []string{"node:a"}}) {
		t.Fatal("unknown holder: want false")
	}
	if l.Retarget("t1", Request{Holder: "t1", Pools: []string{"node:a"}}) {
		t.Fatal("held lease: want false")
	}
	if !l.Held("t1") {
		t.Fatal("held lease should be untouched by a refused retarget")
	}
}

// waitForWaiters polls the internal waiter queue until it has n entries
// (the second Acquire happens in a goroutine).
func waitForWaiters(t *testing.T, l *Ledger, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		got := 0
		for _, w := range l.waiters {
			if w.req.Holder == "t2" {
				got++
			}
		}
		l.mu.Unlock()
		if got == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("queue never reached %d t2 waiters", n)
}

// waitersCount reports how many live queue entries name the holder.
func (l *Ledger) waitersCount(holder string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, w := range l.waiters {
		if w.req.Holder == holder {
			n++
		}
	}
	return n
}
