package observer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Fixed clock: deterministic time for the collector's Now source.
type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFixedClock() *fixedClock {
	return &fixedClock{t: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeTicker drives ticks by hand from a channel; Stop only marks it so
// late ticks are dropped instead of panicking.
type fakeTicker struct {
	ch      chan time.Time
	stopped bool
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 16)}
}

func (f *fakeTicker) Chan() <-chan time.Time { return f.ch }

func (f *fakeTicker) Stop() { f.stopped = true }

func (f *fakeTicker) tick() {
	if f.stopped {
		return
	}
	select {
	case f.ch <- time.Time{}:
	default:
	}
}

// tickerRegistry stands in for the collector's NewTicker hook and lets
// tests tick every created ticker.
type tickerRegistry struct {
	mu   sync.Mutex
	list []*fakeTicker
}

func newTickerRegistry() *tickerRegistry { return &tickerRegistry{} }

func (r *tickerRegistry) newTicker(d time.Duration) Ticker {
	tk := newFakeTicker()
	r.mu.Lock()
	r.list = append(r.list, tk)
	r.mu.Unlock()
	return tk
}

func (r *tickerRegistry) tickAll() {
	r.mu.Lock()
	list := append([]*fakeTicker(nil), r.list...)
	r.mu.Unlock()
	for _, tk := range list {
		tk.tick()
	}
}

// fakePullFetcher records every call and signals a buffered channel after
// each completed fetch (so tests wait on completion, not on initiation).
type fakePullFetcher struct {
	mu      sync.Mutex
	calls   []string
	handler func(name string, ctx context.Context) (*StatsResponse, error)
	callChs map[string]chan struct{}
}

func newFakePullFetcher(handler func(name string, ctx context.Context) (*StatsResponse, error)) *fakePullFetcher {
	return &fakePullFetcher{handler: handler}
}

func (f *fakePullFetcher) Fetch(ctx context.Context, name string) (*StatsResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	ch := f.callChs[name]
	f.mu.Unlock()
	snap, err := f.handler(name, ctx)
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return snap, err
}

func (f *fakePullFetcher) callCh(name string) chan struct{} {
	ch := make(chan struct{}, 16)
	f.mu.Lock()
	if f.callChs == nil {
		f.callChs = make(map[string]chan struct{})
	}
	f.callChs[name] = ch
	f.mu.Unlock()
	return ch
}

func (f *fakePullFetcher) callsFor(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func waitForFetch(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// waitForLast polls c.Last(name) until ok(st) holds and returns the stored
// state. fakePullFetcher signals its channel as soon as Fetch returns, but
// PullCollector records the result only after that, so a test that reads Last
// straight after waitForFetch can race the store. The poll is bounded by a
// deadline and never loops unconditionally.
func waitForLast(t *testing.T, c *PullCollector, name string, ok func(PullState) bool, what string) PullState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var st PullState
	var found bool
	for {
		st, found = c.Last(name)
		if found && ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; last state = (%+v, found=%v)", what, st, found)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func testSnapshot() *StatsResponse {
	return &StatsResponse{V: 2, Host: Host{Name: "peer"}}
}

func newTestCollector(f *fakePullFetcher, clock *fixedClock, reg *tickerRegistry) *PullCollector {
	return NewPullCollector(PullCollectorOptions{
		Fetcher:   f,
		NewTicker: reg.newTicker,
		Now:       clock.Now,
	})
}

func TestObserverPullCollector_SuccessClearsStale(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	var mu sync.Mutex
	callN := 0
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		mu.Lock()
		callN++
		first := callN == 1
		mu.Unlock()
		if first {
			return nil, errors.New("boom")
		}
		return testSnapshot(), nil
	})

	c := newTestCollector(f, clock, reg)
	t.Cleanup(c.Stop)

	ch := f.callCh("a")
	c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})

	// First fetch fails → stale with reason error.
	waitForFetch(t, ch, "first failed fetch of a")
	st := waitForLast(t, c, "a", func(st PullState) bool {
		return st.Stale && st.StaleReason == ReasonError
	}, "first failed state of a")
	if st.LastAttemptAt != clock.Now() {
		t.Fatalf("LastAttemptAt = %v, want %v", st.LastAttemptAt, clock.Now())
	}
	if st.Source != PullSource {
		t.Fatalf("Source = %q, want %q", st.Source, PullSource)
	}

	// Second fetch succeeds → stale cleared, reason reset, snapshot kept.
	// The first (failed) state is already stored, so wait for the new state:
	// success recorded and staleness cleared.
	clock.advance(time.Hour)
	reg.tickAll()
	waitForFetch(t, ch, "second successful fetch of a")
	st = waitForLast(t, c, "a", func(st PullState) bool {
		return !st.Stale && st.StaleReason == ReasonNone && !st.LastSuccessAt.IsZero()
	}, "second successful state of a")
	if !st.LastSuccessAt.Equal(clock.Now()) {
		t.Fatalf("LastSuccessAt = %v, want %v", st.LastSuccessAt, clock.Now())
	}
	if st.LastSnapshot == nil || st.LastSnapshot.Host.Name != "peer" {
		t.Fatalf("LastSnapshot = %+v, want non-nil peer snapshot", st.LastSnapshot)
	}
}

func TestObserverPullCollector_ErrorSetsStaleWithReasonError(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		return nil, errors.New("transport down")
	})
	c := newTestCollector(f, clock, reg)
	t.Cleanup(c.Stop)

	ch := f.callCh("a")
	c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})
	waitForFetch(t, ch, "failing fetch of a")

	st := waitForLast(t, c, "a", func(st PullState) bool {
		return st.Stale && st.StaleReason == ReasonError
	}, "stale state of a after error")
	if st.LastSuccessAt != (time.Time{}) {
		t.Fatalf("LastSuccessAt = %v, want zero", st.LastSuccessAt)
	}
	if st.LastSnapshot != nil {
		t.Fatalf("LastSnapshot = %+v, want nil", st.LastSnapshot)
	}
}

func TestObserverPullCollector_TimeoutSetsReasonTimeout(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		return nil, context.DeadlineExceeded
	})
	c := newTestCollector(f, clock, reg)
	t.Cleanup(c.Stop)

	ch := f.callCh("a")
	c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})
	waitForFetch(t, ch, "timed-out fetch of a")

	waitForLast(t, c, "a", func(st PullState) bool {
		return st.Stale && st.StaleReason == ReasonTimeout
	}, "stale state of a after timeout")
}

func TestObserverPullCollector_BlockedPeerDoesNotDelayAnother(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	release := make(chan struct{})
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		if name == "slow" {
			select {
			case <-release:
				return nil, errors.New("slow finished late")
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return testSnapshot(), nil
	})
	c := newTestCollector(f, clock, reg)

	fastCh := f.callCh("fast")
	slowCh := f.callCh("slow")
	c.SetTargets([]PullTarget{
		{Name: "slow", Interval: time.Hour, Timeout: time.Hour, Enabled: true},
		{Name: "fast", Interval: time.Hour, Timeout: time.Hour, Enabled: true},
	})

	// fast completes while slow is still blocked in its fetch — a blocked
	// peer must never delay another peer.
	waitForFetch(t, fastCh, "fast fetch while slow is blocked")
	waitForLast(t, c, "fast", func(st PullState) bool {
		return st.LastSnapshot != nil && !st.Stale
	}, "recorded state of fast")
	if st, ok := c.Last("slow"); ok && st.LastSnapshot != nil {
		t.Fatalf("slow completed unexpectedly: %+v", st)
	}

	// Release slow: it then records a failure of its own.
	close(release)
	waitForFetch(t, slowCh, "slow fetch after release")
	waitForLast(t, c, "slow", func(st PullState) bool {
		return st.Stale && st.StaleReason == ReasonError
	}, "recorded stale state of slow")
	c.Stop()
}

func TestObserverPullCollector_DisabledTargetIsNeverPolled(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		return testSnapshot(), nil
	})
	c := newTestCollector(f, clock, reg)
	t.Cleanup(c.Stop)

	enabledCh := f.callCh("on")
	c.SetTargets([]PullTarget{
		{Name: "on", Interval: time.Hour, Timeout: time.Hour, Enabled: true},
		{Name: "off", Interval: time.Hour, Timeout: time.Hour, Enabled: false},
	})

	waitForFetch(t, enabledCh, "first fetch of enabled target")
	reg.tickAll()
	reg.tickAll()

	if n := f.callsFor("off"); n != 0 {
		t.Fatalf("disabled target polled %d times, want 0", n)
	}
	if _, ok := c.Last("off"); ok {
		t.Fatalf("Last(off) = found, want not found")
	}
	if c.workerDone("off") != nil {
		t.Fatal("disabled target has a worker, want none")
	}
}

func TestObserverPullCollector_SetTargetsRemovesGoroutine(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		return testSnapshot(), nil
	})
	c := newTestCollector(f, clock, reg)

	ch := f.callCh("a")
	c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})
	waitForFetch(t, ch, "first fetch of a")
	if done := c.workerDone("a"); done == nil {
		t.Fatal("worker for a not running, want running")
	}
	done := c.workerDone("a")

	// Remove the target: the worker must exit and stop consuming ticks.
	c.SetTargets(nil)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker for a did not exit after removal")
	}
	before := f.callsFor("a")
	reg.tickAll()
	if got := f.callsFor("a"); got != before {
		t.Fatalf("removed target polled again: calls = %d, want %d", got, before)
	}

	// Re-adding the same name starts a fresh worker.
	c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})
	waitForFetch(t, ch, "fetch of re-added a")
	c.Stop()
}

func TestObserverPullCollector_StopIsIdempotent(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		return testSnapshot(), nil
	})
	c := newTestCollector(f, clock, reg)

	c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})
	done := c.workerDone("a")
	if done == nil {
		t.Fatal("worker for a not running, want running")
	}

	// Stop may be called repeatedly without panic or deadlock.
	c.Stop()
	c.Stop()
	c.Stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not exit after Stop")
	}
}

func TestObserverPullCollector_ErrorReasonMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    string
		wrapped bool
	}{
		{"deadline", context.DeadlineExceeded, ReasonTimeout, false},
		{"http401", NewPullHTTPError(401, "bad token"), ReasonForbidden, false},
		{"http403", NewPullHTTPError(403, ""), ReasonForbidden, false},
		{"http500", NewPullHTTPError(500, "boom"), ReasonError, false},
		{"badpayload", fmt.Errorf("decode: %w", ErrBadPayload), ReasonBadPayload, true},
		{"notpermitted", fmt.Errorf("denied: %w", ErrNotPermitted), ReasonNotPermitted, true},
		{"wrapped401", fmt.Errorf("pull a: %w", NewPullHTTPError(401, "")), ReasonForbidden, true},
		{"plain", errors.New("dial refused"), ReasonError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFixedClock()
			reg := newTickerRegistry()
			f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
				return nil, tc.err
			})
			c := newTestCollector(f, clock, reg)
			t.Cleanup(c.Stop)

			ch := f.callCh("a")
			c.SetTargets([]PullTarget{{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true}})
			waitForFetch(t, ch, "fetch of a")

			st := waitForLast(t, c, "a", func(st PullState) bool {
				return st.Stale && st.StaleReason == tc.want
			}, "recorded stale state of a")
			if !st.Stale {
				t.Fatal("Stale = false, want true")
			}
			if st.StaleReason != tc.want {
				t.Fatalf("StaleReason = %q, want %q", st.StaleReason, tc.want)
			}
		})
	}
}

func TestObserverPullCollector_SnapshotSortedByName(t *testing.T) {
	clock := newFixedClock()
	reg := newTickerRegistry()
	f := newFakePullFetcher(func(name string, ctx context.Context) (*StatsResponse, error) {
		return testSnapshot(), nil
	})
	c := newTestCollector(f, clock, reg)
	t.Cleanup(c.Stop)

	chA := f.callCh("a")
	chB := f.callCh("b")
	c.SetTargets([]PullTarget{
		{Name: "b", Interval: time.Hour, Timeout: time.Hour, Enabled: true},
		{Name: "a", Interval: time.Hour, Timeout: time.Hour, Enabled: true},
	})
	waitForFetch(t, chA, "first fetch of a")
	waitForLast(t, c, "a", func(st PullState) bool { return st.LastSnapshot != nil }, "recorded state of a")
	waitForFetch(t, chB, "first fetch of b")
	waitForLast(t, c, "b", func(st PullState) bool { return st.LastSnapshot != nil }, "recorded state of b")

	snap := c.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot() len = %d, want 2", len(snap))
	}
	if snap[0].Name != "a" || snap[1].Name != "b" {
		t.Fatalf("Snapshot() order = %q, %q, want a, b", snap[0].Name, snap[1].Name)
	}
	for _, s := range snap {
		if s.Source != PullSource {
			t.Fatalf("Snapshot() Source = %q, want %q", s.Source, PullSource)
		}
	}

	if _, ok := c.Last("missing"); ok {
		t.Fatal("Last(missing) = found, want not found")
	}
}
