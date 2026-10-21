package session

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestAlertGate_FiresAfterSettle(t *testing.T) {
	g := NewAlertGate()
	var n int32
	g.Schedule("s", "k", 30*time.Millisecond, time.Minute, func() { atomic.AddInt32(&n, 1) })
	pollUntil(t, 10*time.Second, func() bool { return atomic.LoadInt32(&n) >= 1 }, "alert never fired")
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("want 1 fire, got %d", got)
	}
}

// settleSentinel schedules a throwaway alert on another session with a settle
// window longer than every timer under test, and blocks until it has fired.
// Timers run on the runtime timer heap in deadline order, so once the sentinel
// has fired any earlier-deadline timer that was still armed has run too; this
// turns "wait long enough that nothing fired" into an observed event.
func settleSentinel(t *testing.T, g *AlertGate, settle time.Duration) {
	t.Helper()
	var fired int32
	g.Schedule("sentinel", "k", settle, time.Minute, func() { atomic.AddInt32(&fired, 1) })
	pollUntil(t, 10*time.Second, func() bool { return atomic.LoadInt32(&fired) == 1 }, "sentinel alert never fired")
}

func TestAlertGate_CancelDropsPending(t *testing.T) {
	g := NewAlertGate()
	var n int32
	g.Schedule("s", "k", 30*time.Millisecond, time.Minute, func() { atomic.AddInt32(&n, 1) })
	g.Cancel("s")
	settleSentinel(t, g, 60*time.Millisecond)
	if atomic.LoadInt32(&n) != 0 || g.Cancelled != 1 {
		t.Fatalf("pending alert must be dropped: fires=%d cancelled=%d", atomic.LoadInt32(&n), g.Cancelled)
	}
}

// Rescheduling must stop the first timer and arm a fresh one. The first window
// is long and the second short, so no wall-clock arithmetic is needed: if the
// first timer survived it would fire (first > 0) before the sentinel does.
func TestAlertGate_RescheduleRestartsWindow(t *testing.T) {
	g := NewAlertGate()
	var first, second int32
	g.Schedule("s", "k", 150*time.Millisecond, time.Minute, func() { atomic.AddInt32(&first, 1) })
	g.Schedule("s", "k", 20*time.Millisecond, time.Minute, func() { atomic.AddInt32(&second, 1) })
	pollUntil(t, 10*time.Second, func() bool { return atomic.LoadInt32(&second) >= 1 }, "restarted window never fired")
	settleSentinel(t, g, 300*time.Millisecond)
	if f, sc := atomic.LoadInt32(&first), atomic.LoadInt32(&second); f != 0 || sc != 1 {
		t.Fatalf("window should have restarted: first=%d second=%d", f, sc)
	}
}

func TestAlertGate_RepeatSuppressedUntilKeyChanges(t *testing.T) {
	g := NewAlertGate()
	var n int32
	f := func() { atomic.AddInt32(&n, 1) }
	g.Schedule("s", "k", 0, time.Minute, f)
	g.Schedule("s", "k", 0, time.Minute, f)
	if atomic.LoadInt32(&n) != 1 || g.Repeats != 1 {
		t.Fatalf("identical repeat must be suppressed: fires=%d repeats=%d", n, g.Repeats)
	}
	g.Schedule("s", "k2", 0, time.Minute, f)
	if atomic.LoadInt32(&n) != 2 {
		t.Fatal("changed key must fire")
	}
	g.Forget("s")
	g.Schedule("s", "k2", 0, time.Minute, f)
	if atomic.LoadInt32(&n) != 3 {
		t.Fatal("Forget must clear repeat memory")
	}
}
