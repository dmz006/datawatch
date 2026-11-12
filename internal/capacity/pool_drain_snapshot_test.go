package capacity

import (
	"context"
	"sync"
	"testing"
	"time"
)

// drainClockNow is the fixed wall clock the drain snapshot tests run against.
var drainClockNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// fixedDrainClock returns drainClockNow for Now and a real timer for After.
type fixedDrainClock struct{}

func (fixedDrainClock) Now() time.Time                         { return drainClockNow }
func (fixedDrainClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// drainRecord stands in for one daemon node view: it keeps a copy of the
// drain and permission fields and evaluates them at call time, the way the
// daemon's nodeHealthView.state does (a copy, not the registry record).
type drainRecord struct {
	mu      sync.Mutex
	drained bool
	until   time.Time
	denied  []string
}

func (r *drainRecord) set(drained bool, until time.Time, denied []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drained = drained
	r.until = until
	r.denied = append([]string(nil), denied...)
}

// state evaluates the copied record against the test clock, the way the
// daemon does: a drain is active only while its until (if any) is still
// ahead of now. It is the ledger's Options.NodeState hook.
func (r *drainRecord) state(name string) NodeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name != "gpu-a" {
		return NodeState{}
	}
	ns := NodeState{
		Drained:         r.drained && (r.until.IsZero() || drainClockNow.Before(r.until)),
		DeniedConsumers: append([]string(nil), r.denied...),
	}
	if ns.Drained && !r.until.IsZero() {
		ns.DrainUntil = r.until
	}
	if !ns.Drained {
		ns.DrainUntil = time.Time{}
	}
	return ns
}

// nodePool finds the node:gpu-a pool in st (fatal when absent).
func nodePool(t *testing.T, st Status) *PoolStatus {
	t.Helper()
	for i := range st.Pools {
		if st.Pools[i].Name == "node:gpu-a" {
			return &st.Pools[i]
		}
	}
	t.Fatalf("snapshot has no node:gpu-a pool: %+v", st.Pools)
	return nil
}

func TestPoolDrainSnapshotShowsDrainedPoolWithHeldLease(t *testing.T) {
	rec := &drainRecord{}
	until := drainClockNow.Add(time.Hour)
	rec.set(true, until, nil)
	led := New(Options{Clock: fixedDrainClock{}, NodeState: rec.state})
	led.SetLimit("node:gpu-a", 2)
	if err := led.Acquire(context.Background(), Request{
		Holder: "task-a", PRDID: "prd-a", Pools: []string{"node:gpu-a"}, Node: "gpu-a",
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	st := led.Snapshot()
	p := nodePool(t, st)
	if !p.Drained {
		t.Fatalf("drained node pool must show Drained=true: %+v", p)
	}
	if !p.DrainUntil.Equal(until) {
		t.Fatalf("DrainUntil = %v, want %v", p.DrainUntil, until)
	}
	if p.Held != 1 {
		t.Fatalf("a drained pool keeps its held count: Held = %d, want 1", p.Held)
	}
}

func TestPoolDrainSnapshotUndrainedAfterClear(t *testing.T) {
	rec := &drainRecord{}
	rec.set(true, drainClockNow.Add(time.Hour), nil)
	led := New(Options{Clock: fixedDrainClock{}, NodeState: rec.state})
	led.SetLimit("node:gpu-a", 2)
	if err := led.Acquire(context.Background(), Request{
		Holder: "task-a", PRDID: "prd-a", Pools: []string{"node:gpu-a"}, Node: "gpu-a",
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	// The operator clears the drain; the next snapshot must read it as open.
	rec.set(false, time.Time{}, nil)
	st := led.Snapshot()
	p := nodePool(t, st)
	if p.Drained || !p.DrainUntil.IsZero() {
		t.Fatalf("cleared drain must not show: drained=%v until=%v", p.Drained, p.DrainUntil)
	}
	if p.Held != 1 {
		t.Fatalf("undrain does not release holders: Held = %d, want 1", p.Held)
	}
}

func TestPoolDrainSnapshotExpiredDrainNotDrained(t *testing.T) {
	rec := &drainRecord{}
	// The stored drain's until is in the past: evaluated at now it is not
	// active, so the pool must read as undrained.
	rec.set(true, drainClockNow.Add(-time.Minute), nil)
	led := New(Options{Clock: fixedDrainClock{}, NodeState: rec.state})
	led.SetLimit("node:gpu-a", 2)
	if err := led.Acquire(context.Background(), Request{
		Holder: "task-a", PRDID: "prd-a", Pools: []string{"node:gpu-a"}, Node: "gpu-a",
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	st := led.Snapshot()
	p := nodePool(t, st)
	if p.Drained || !p.DrainUntil.IsZero() {
		t.Fatalf("expired drain must read as not drained: drained=%v until=%v", p.Drained, p.DrainUntil)
	}
	if p.Held != 1 {
		t.Fatalf("Held = %d, want 1 (the lease is untouched)", p.Held)
	}
}

func TestPoolDrainSnapshotDeniedConsumersPassThrough(t *testing.T) {
	rec := &drainRecord{}
	denied := []string{"council", "ask"}
	rec.set(true, time.Time{}, denied)
	led := New(Options{Clock: fixedDrainClock{}, NodeState: rec.state})
	led.SetLimit("node:gpu-a", 2)
	if err := led.Acquire(context.Background(), Request{
		Holder: "task-a", PRDID: "prd-a", Pools: []string{"node:gpu-a"}, Node: "gpu-a",
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	// The snapshot runs under the ledger lock, so the hook's read of the
	// denial list must pass through verbatim.
	got := rec.state("gpu-a")
	want := []string{"council", "ask"}
	if len(got.DeniedConsumers) != len(want) {
		t.Fatalf("DeniedConsumers = %v, want %v", got.DeniedConsumers, want)
	}
	for i := range want {
		if got.DeniedConsumers[i] != want[i] {
			t.Fatalf("DeniedConsumers = %v, want %v", got.DeniedConsumers, want)
		}
	}

	st := led.Snapshot()
	p := nodePool(t, st)
	if !p.Drained {
		t.Fatalf("drained node pool must show Drained=true: %+v", p)
	}
	if !p.DrainUntil.IsZero() {
		t.Fatalf("open-ended drain must carry no DrainUntil: %v", p.DrainUntil)
	}
}
