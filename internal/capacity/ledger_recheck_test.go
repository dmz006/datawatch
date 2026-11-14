package capacity

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// recheckLedger returns a ledger on the manual clock with pools a and b of one
// slot each, a held by "holder-a", plus the clock. A request waits on a with
// the clock's poll timer and its wait deadline pending (two timers).
func recheckLedger(t *testing.T) (*Ledger, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	l := New(Options{PollInterval: time.Second, Clock: clk})
	l.SetLimit("a", 1)
	l.SetLimit("b", 1)
	if err := l.Acquire(context.Background(), Request{Holder: "holder-a", Pools: []string{"a"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	return l, clk
}

// A recheck that names another placement on a wait pass moves the waiting
// request there and grants it in that pass, keeping the single request.
func TestAcquireRechecking_RetargetsWaiterOnAWaitPass(t *testing.T) {
	l, clk := recheckLedger(t)
	var passes atomic.Int32
	var move atomic.Bool
	recheck := func() (Request, bool) {
		passes.Add(1)
		if !move.Load() {
			return Request{}, false
		}
		return Request{Holder: "t1", Pools: []string{"b"}}, true
	}
	var reasons []string
	done := make(chan error, 1)
	go func() {
		done <- l.AcquireRechecking(context.Background(), Request{Holder: "t1", Pools: []string{"a"}}, time.Minute, nil,
			func(r string) { reasons = append(reasons, r) }, recheck)
	}()
	waitPending(t, clk, 2)

	clk.Advance(time.Second) // first pass: recheck declines, task keeps waiting
	waitPending(t, clk, 2)
	if passes.Load() != 1 {
		t.Fatalf("recheck passes = %d, want 1", passes.Load())
	}
	select {
	case err := <-done:
		t.Fatalf("returned while waiting: %v", err)
	default:
	}
	if got := l.waitersCount("t1"); got != 1 {
		t.Fatalf("waiters = %d, want 1", got)
	}

	move.Store(true)
	clk.Advance(time.Second) // second pass: recheck moves it onto the idle pool b
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was not granted after the recheck moved it")
	}
	ls := l.LeaseOf("t1")
	if ls == nil || len(ls.Pools) != 1 || ls.Pools[0] != "b" {
		t.Fatalf("lease = %+v, want pool b", ls)
	}
	if got := l.waitersCount("t1"); got != 0 {
		t.Fatalf("waiters after grant = %d, want 0", got)
	}
	if !l.Held("holder-a") {
		t.Fatal("holder-a lost its lease")
	}
}

// The wait bound keeps counting from when the request was first queued: a
// retarget does not restart it.
func TestAcquireRechecking_RetargetKeepsTheWaitBound(t *testing.T) {
	l, clk := recheckLedger(t)
	if err := l.Acquire(context.Background(), Request{Holder: "holder-b", Pools: []string{"b"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	moved := false
	recheck := func() (Request, bool) {
		if moved {
			return Request{}, false
		}
		moved = true
		return Request{Holder: "t1", Pools: []string{"b"}}, true // also full
	}
	done := make(chan error, 1)
	go func() {
		done <- l.AcquireRechecking(context.Background(), Request{Holder: "t1", Pools: []string{"a"}}, 5*time.Second, nil, nil, recheck)
	}()
	waitPending(t, clk, 2)
	clk.Advance(time.Second) // pass: retargeted onto the full pool b, still waiting
	waitPending(t, clk, 2)
	clk.Advance(10 * time.Second) // past the original bound
	select {
	case err := <-done:
		if !errors.Is(err, ErrWaitTimeout) {
			t.Fatalf("err = %v, want ErrWaitTimeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait bound did not fire after the retarget")
	}
	if got := l.waitersCount("t1"); got != 0 {
		t.Fatalf("waiters after timeout = %d, want 0", got)
	}
}

// A nil recheck is plain Acquire: the waiter is granted when the pool frees.
func TestAcquireRechecking_NilRecheckIsPlainAcquire(t *testing.T) {
	l, clk := recheckLedger(t)
	done := make(chan error, 1)
	go func() {
		done <- l.AcquireRechecking(context.Background(), Request{Holder: "t1", Pools: []string{"a"}}, time.Minute, nil, nil, nil)
	}()
	waitPending(t, clk, 2)
	clk.Advance(time.Second)
	waitPending(t, clk, 2)
	l.Release("holder-a")
	clk.Advance(time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was not granted when its pool freed")
	}
	if ls := l.LeaseOf("t1"); ls == nil || ls.Pools[0] != "a" {
		t.Fatalf("lease = %+v, want pool a", ls)
	}
}
