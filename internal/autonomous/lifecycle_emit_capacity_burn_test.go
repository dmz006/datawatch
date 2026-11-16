// Tests for the capacity-wait and burn-guard rung events in
// lifecycle_emit_capacity_burn.go.

package autonomous

import (
	"sync"
	"testing"
	"time"
)

type capBurnEvents struct {
	mu  sync.Mutex
	evs []LifecycleEvent
}

func (c *capBurnEvents) add(ev LifecycleEvent) {
	c.mu.Lock()
	c.evs = append(c.evs, ev)
	c.mu.Unlock()
}

// waitOf waits until want events of kind arrived (bounded) plus a settle
// window, then returns them; the bus delivers on its own goroutine.
func (c *capBurnEvents) waitOf(t *testing.T, kind string, want int) []LifecycleEvent {
	t.Helper()
	settledCount(t, want, func() int { return len(c.of(kind)) })
	return c.of(kind)
}

func (c *capBurnEvents) of(kind string) []LifecycleEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []LifecycleEvent
	for _, ev := range c.evs {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func newCapBurnManager(t *testing.T) (*Manager, *capBurnEvents, *time.Time, *sync.Mutex) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.CapacityWaitTimeoutSeconds = 100 // threshold is half: 50s
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	now := time.Unix(1_700_000_000, 0)
	var mu sync.Mutex
	m.orphanNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	col := &capBurnEvents{}
	cancel := m.Subscribe(LifecycleFilter{}, col.add)
	t.Cleanup(func() { cancel(); m.lifecycleBus().Close() })
	return m, col, &now, &mu
}

func TestCapacityWaitEvent(t *testing.T) {
	m, col, now, mu := newCapBurnManager(t)
	advance := func(d time.Duration) { mu.Lock(); *now = now.Add(d); mu.Unlock() }
	task := &Task{ID: "t1", PRDID: "p1", StoryID: "s1"}

	m.beginCapacityWait(task)
	advance(49 * time.Second)
	m.sweepCapacityWaits()
	if n := len(col.waitOf(t, KindCapacityWaitTooLong, 0)); n != 0 {
		t.Fatalf("emitted %d before the threshold, want 0", n)
	}
	advance(2 * time.Second)
	m.sweepCapacityWaits()
	evs := col.waitOf(t, KindCapacityWaitTooLong, 1)
	if len(evs) != 1 || evs[0].TaskID != "t1" || evs[0].PRDID != "p1" {
		t.Fatalf("events after the threshold = %+v, want one for t1/p1", evs)
	}
	// Same episode, still waiting: no second event, even on repeated waiting calls.
	m.beginCapacityWait(task)
	advance(500 * time.Second)
	m.sweepCapacityWaits()
	if n := len(col.waitOf(t, KindCapacityWaitTooLong, 1)); n != 1 {
		t.Fatalf("emitted %d within one episode, want 1", n)
	}
	// A new episode emits again.
	m.endCapacityWait("t1")
	m.beginCapacityWait(task)
	advance(51 * time.Second)
	m.sweepCapacityWaits()
	if n := len(col.waitOf(t, KindCapacityWaitTooLong, 2)); n != 2 {
		t.Fatalf("emitted %d after a second episode, want 2", n)
	}
	// A wait that ends before the threshold emits nothing.
	m.endCapacityWait("t1")
	m.beginCapacityWait(task)
	advance(10 * time.Second)
	m.endCapacityWait("t1")
	advance(100 * time.Second)
	m.sweepCapacityWaits()
	if n := len(col.waitOf(t, KindCapacityWaitTooLong, 2)); n != 2 {
		t.Fatalf("emitted %d for an ended episode, want 2", n)
	}
}

func TestBurnGuardRungEvent(t *testing.T) {
	m, col, _, _ := newCapBurnManager(t)

	m.NoteBurnGuardRung("", "lower_inflight")
	m.NoteBurnGuardRung("lower_inflight", "lower_inflight") // unchanged: nothing
	m.NoteBurnGuardRung("lower_inflight", "")
	evs := col.waitOf(t, KindBurnGuardRungChanged, 2)
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2", len(evs))
	}
	seen := map[string]string{}
	for _, ev := range evs {
		seen[ev.OldStatus] = ev.NewStatus
	}
	if seen[""] != "lower_inflight" || seen["lower_inflight"] != "" {
		t.Fatalf("old/new rung pairs = %v", seen)
	}
}
