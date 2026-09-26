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
	time.Sleep(80 * time.Millisecond)
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("want 1 fire, got %d", n)
	}
}

func TestAlertGate_CancelDropsPending(t *testing.T) {
	g := NewAlertGate()
	var n int32
	g.Schedule("s", "k", 40*time.Millisecond, time.Minute, func() { atomic.AddInt32(&n, 1) })
	g.Cancel("s")
	time.Sleep(90 * time.Millisecond)
	if atomic.LoadInt32(&n) != 0 || g.Cancelled != 1 {
		t.Fatalf("pending alert must be dropped: fires=%d cancelled=%d", n, g.Cancelled)
	}
}

func TestAlertGate_RescheduleRestartsWindow(t *testing.T) {
	g := NewAlertGate()
	var n int32
	f := func() { atomic.AddInt32(&n, 1) }
	g.Schedule("s", "k", 60*time.Millisecond, time.Minute, f)
	time.Sleep(40 * time.Millisecond)
	g.Schedule("s", "k", 60*time.Millisecond, time.Minute, f)
	time.Sleep(40 * time.Millisecond)
	if atomic.LoadInt32(&n) != 0 {
		t.Fatal("window should have restarted")
	}
	time.Sleep(60 * time.Millisecond)
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("want exactly 1 fire, got %d", n)
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
