package capacity

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// The wait bound counts from the moment the request was queued. Time spent in
// the onWait callback (here a 4s clock jump) is part of the wait, not a reason
// to start the bound over, so a 10s bound is spent after 10s in all.
func TestAcquireDeadlineAnchoredAtQueueTime(t *testing.T) {
	clk := newFakeClock()
	l := New(Options{Clock: clk, PollInterval: time.Minute})
	l.SetLimit("p", 1)
	if err := l.Acquire(context.Background(), Request{Holder: "a", Pools: []string{"p"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	var fired atomic.Bool
	onWaitDone := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "b", Pools: []string{"p"}}, 10*time.Second, nil, func(string) {
			if fired.CompareAndSwap(false, true) {
				clk.Advance(4 * time.Second)
				close(onWaitDone)
			}
		})
	}()
	select {
	case <-onWaitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("request never queued")
	}
	select {
	case err := <-done:
		t.Fatalf("timed out after 4s of a 10s bound: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	// 4s + 6s = 10s since the request was queued: the bound is spent. A bound
	// restarted after the callback would run until 14s and not fire here.
	clk.Advance(6 * time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, ErrWaitTimeout) {
			t.Fatalf("want ErrWaitTimeout, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait bound did not count from queue time")
	}
}
