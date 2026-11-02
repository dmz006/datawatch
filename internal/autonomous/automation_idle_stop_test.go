// Regression test: Coordinator.Stop must wait for the idle loop to exit
// before returning, so no tick (and the passes a tick runs) can start or
// finish while Stop is in flight. The harness holds the loop blocked inside
// a tick via the idleTickHook test seam, calls Stop, and asserts that Stop
// does not return while the loop is still inside that tick.

package autonomous

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoordinatorStopWaitsForIdleLoop(t *testing.T) {
	h := newCoordHarness(t, 1)
	h.c.idleTick = time.Millisecond

	entered := make(chan struct{}) // closed on the hook's first entry
	gate := make(chan struct{})    // hook blocks here until the test releases it
	enterOnce := sync.Once{}
	closeGate := sync.OnceFunc(func() { close(gate) })
	var hookCalls int64

	h.c.idleTickHook = func() {
		atomic.AddInt64(&hookCalls, 1)
		enterOnce.Do(func() { close(entered) })
		<-gate
	}

	// The loop can exit (or be unblocked) even when the test fails: the gate
	// is closed exactly once.
	t.Cleanup(closeGate)

	h.c.Start()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("idle loop never entered its first tick within 5s")
	}

	stopped := make(chan struct{})
	go func() {
		h.c.Stop()
		close(stopped)
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-stopped:
		t.Fatal("Stop returned while the idle loop was still blocked inside a tick; Stop must wait for the idle loop to exit before returning")
	default:
	}

	closeGate()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return within 5s after the idle loop unblocked")
	}

	first := atomic.LoadInt64(&hookCalls)
	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt64(&hookCalls); got != first {
		t.Fatalf("idle tick hook ran %d times before Stop returned and %d times after; no tick may run after Stop returns", first, got)
	}
}
