package capacity

import (
	"sync"
	"testing"
	"time"
)

// A restart that re-adopts more sessions than a pool's limit (eight workers on
// a pool of three) keeps every real session, blocks new admissions until the
// count is back under the cap, shows how far over the pool is, and raises one
// finding per pool, not one per session.
func TestAdoptOverLimitRaisesOneFindingAndBlocksNewWork(t *testing.T) {
	var mu sync.Mutex
	var got []string
	opt := fast()
	opt.OnAdoptOverLimit = func(pool string, held, limit int) {
		mu.Lock()
		got = append(got, pool)
		mu.Unlock()
		if held != 8 || limit != 3 {
			t.Errorf("finding for %s = %d/%d, want 8/3", pool, held, limit)
		}
	}
	l := New(opt)
	l.SetLimit("llm:pi-prompt", 3)
	l.BeginAdoption()
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		l.Adopt(Request{Holder: "t-" + id, Pools: []string{"llm:pi-prompt"}}, "sess-"+id)
	}
	l.MarkAdoptionComplete()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("findings = %d, want exactly 1 for the pool", n)
	}
	if err := acq(t, l, Request{Holder: "new", Pools: []string{"llm:pi-prompt"}}, 40*time.Millisecond); err == nil {
		t.Fatal("new work must wait while adopted sessions hold the pool over its limit")
	}
	for _, p := range l.Snapshot().Pools {
		if p.Name == "llm:pi-prompt" && (p.Held != 8 || p.Over != 5) {
			t.Fatalf("pool = %+v, want held 8, over 5", p)
		}
	}
}

// A restart within the limit raises nothing.
func TestAdoptWithinLimitRaisesNoFinding(t *testing.T) {
	called := make(chan struct{}, 1)
	opt := fast()
	opt.OnAdoptOverLimit = func(string, int, int) { called <- struct{}{} }
	l := New(opt)
	l.SetLimit("llm:pi-prompt", 3)
	l.BeginAdoption()
	l.Adopt(Request{Holder: "t-a", Pools: []string{"llm:pi-prompt"}}, "sess-a")
	l.MarkAdoptionComplete()
	select {
	case <-called:
		t.Fatal("no finding expected within the limit")
	case <-time.After(60 * time.Millisecond):
	}
}
