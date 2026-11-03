package capacity

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A reason-text change must not extend the wait bound, which counts from the
// moment the request was queued.
func TestAcquireDeadlineAnchoredAtQueueTime(t *testing.T) {
	clk := newFakeClock()
	l := New(Options{Clock: clk, PollInterval: time.Second})
	l.SetLimit("p", 1)
	if err := l.Acquire(context.Background(), Request{Holder: "a", Pools: []string{"p"}}, time.Minute, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), Request{Holder: "b", Pools: []string{"p"}}, 10*time.Second, nil, func(string) { clk.Advance(4 * time.Second) })
	}()
	for i := 0; i < 20; i++ {
		time.Sleep(5 * time.Millisecond)
		clk.Advance(time.Second)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrWaitTimeout) {
			t.Fatalf("want ErrWaitTimeout, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not time out")
	}
}
