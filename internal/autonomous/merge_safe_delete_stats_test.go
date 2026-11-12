package autonomous

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

// sdWarnLines returns the log lines captured in buf that carry the
// [autonomous] slow-report warning marker.
func sdWarnLines(buf string) []string {
	var lines []string
	for _, line := range strings.Split(buf, "\n") {
		if strings.Contains(line, "[autonomous]") && strings.Contains(line, "warning threshold") {
			lines = append(lines, line)
		}
	}
	return lines
}

// sdCaptureLog redirects the standard logger's output to a buffer for the
// duration of fn and returns everything fn let it print.
func sdCaptureLog(fn func()) string {
	prev := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	fn()
	return buf.String()
}

// TestSafeDeleteStatsCountersColdAndRefresh covers the happy counters: a
// cold computation plus one background refresh are both counted as
// successful refreshes, the last duration and computed-at come from the
// most recent one, and the worktree count is that of its report.
func TestSafeDeleteStatsCountersColdAndRefresh(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		return SafeDeleteReport{
			Safe: []SafeDeleteEntry{{Branch: "a", Worktree: "/wt/a"}},
			Held: []SafeDeleteHeld{{Branch: "b"}, {Branch: "c"}},
		}, nil
	}

	if _, err := c.get(context.Background(), sdTestRepo); err != nil {
		t.Fatalf("cold read: %v", err)
	}
	st := c.statsSnapshot()
	if st.Refreshes != 1 || st.RefreshFailures != 0 || st.SlowReports != 0 {
		t.Fatalf("after cold: %+v", st)
	}
	if st.Worktrees != 3 {
		t.Errorf("Worktrees = %d, want 3 (1 safe + 2 held)", st.Worktrees)
	}
	if !st.LastComputedAt.Equal(base) {
		t.Errorf("LastComputedAt = %v, want %v", st.LastComputedAt, base)
	}

	// Background refresh: advance past the TTL so the next read is stale.
	clock.advance(safeDeleteCacheTTL + time.Second)
	if _, err := c.get(context.Background(), sdTestRepo); err != nil {
		t.Fatalf("stale read: %v", err)
	}
	sdWaitUntil(t, "the background refresh to finish", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.refreshing[sdTestRepo]
	})
	st = c.statsSnapshot()
	if st.Refreshes != 2 {
		t.Errorf("Refreshes = %d, want 2 (cold + one background refresh)", st.Refreshes)
	}
	if st.RefreshFailures != 0 {
		t.Errorf("RefreshFailures = %d, want 0", st.RefreshFailures)
	}
	if !st.LastComputedAt.Equal(base.Add(safeDeleteCacheTTL + time.Second)) {
		t.Errorf("LastComputedAt = %v, want the refresh time", st.LastComputedAt)
	}

	// The manager method exposes the same counters.
	m := &Manager{cfg: Config{}, safeDeleteCache: c}
	mst := m.SafeDeleteStats()
	if mst != st {
		t.Errorf("Manager.SafeDeleteStats() = %+v, want the cache snapshot %+v", mst, st)
	}
}

// TestSafeDeleteStatsCountersFailure covers the failure counters: a failed
// cold computation counts as a refresh failure, stays out of the success
// counters, records no worktrees and no computed-at, and still records the
// duration.
func TestSafeDeleteStatsCountersFailure(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	c := newSafeDeleteCache()
	c.now = clock.now
	var errBoom = errors.New("cold boom")
	c.compute = func(context.Context, string) (SafeDeleteReport, error) {
		return SafeDeleteReport{}, errBoom
	}

	if _, err := c.get(context.Background(), sdTestRepo); !errors.Is(err, errBoom) {
		t.Fatalf("cold read: %v", err)
	}
	st := c.statsSnapshot()
	if st.Refreshes != 0 {
		t.Errorf("Refreshes = %d, want 0 (a failed computation is not a refresh)", st.Refreshes)
	}
	if st.RefreshFailures != 1 {
		t.Errorf("RefreshFailures = %d, want 1", st.RefreshFailures)
	}
	if st.Worktrees != 0 {
		t.Errorf("Worktrees = %d, want 0 (no successful report yet)", st.Worktrees)
	}
	if !st.LastComputedAt.IsZero() {
		t.Errorf("LastComputedAt = %v, want the zero time (no success yet)", st.LastComputedAt)
	}
}

// TestSafeDeleteStatsSlowComputationWarnsExactlyOnce covers the slow path:
// a computation whose duration exceeds safeDeleteSlowWarn (the injected
// clock advances 4 seconds inside the fake compute) is counted as a slow
// report and produces exactly one warning line naming the duration in
// milliseconds, the threshold and the worktree count.
func TestSafeDeleteStatsSlowComputationWarnsExactlyOnce(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		// The fake compute takes 4 seconds of injected time — over the
		// 3-second threshold by one second.
		clock.advance(4 * time.Second)
		return SafeDeleteReport{
			Safe: []SafeDeleteEntry{{Branch: "a"}},
			Held: []SafeDeleteHeld{{Branch: "b"}},
		}, nil
	}

	out := sdCaptureLog(func() {
		if _, err := c.get(context.Background(), sdTestRepo); err != nil {
			t.Fatalf("cold read: %v", err)
		}
	})

	warns := sdWarnLines(out)
	if len(warns) != 1 {
		t.Fatalf("warning lines = %d (%q), want exactly 1", len(warns), out)
	}
	line := warns[0]
	for _, want := range []string{"[autonomous]", "4000 ms", safeDeleteSlowWarn.String(), "2 worktrees"} {
		if !strings.Contains(line, want) {
			t.Errorf("warning line %q does not contain %q", line, want)
		}
	}

	st := c.statsSnapshot()
	if st.SlowReports != 1 {
		t.Errorf("SlowReports = %d, want 1", st.SlowReports)
	}
	if st.Refreshes != 1 {
		t.Errorf("Refreshes = %d, want 1 (a slow computation still succeeds)", st.Refreshes)
	}
	if st.LastDurationMs != 4000 {
		t.Errorf("LastDurationMs = %d, want 4000", st.LastDurationMs)
	}
}

// TestSafeDeleteStatsFastComputationWarnsNone covers the boundary side: a
// computation within safeDeleteSlowWarn emits no warning line and does not
// count as slow.
func TestSafeDeleteStatsFastComputationWarnsNone(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	c := newSafeDeleteCache()
	c.now = clock.now
	// 2 seconds: inside the 3-second threshold.
	clockAdvanceInCompute := func() { clock.advance(2 * time.Second) }
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		clockAdvanceInCompute()
		return SafeDeleteReport{Safe: []SafeDeleteEntry{{Branch: "a"}}}, nil
	}

	out := sdCaptureLog(func() {
		if _, err := c.get(context.Background(), sdTestRepo); err != nil {
			t.Fatalf("cold read: %v", err)
		}
	})

	if warns := sdWarnLines(out); len(warns) != 0 {
		t.Fatalf("fast computation produced %d warning line(s): %s", len(warns), out)
	}
	st := c.statsSnapshot()
	if st.SlowReports != 0 {
		t.Errorf("SlowReports = %d, want 0 for a computation inside the threshold", st.SlowReports)
	}
	if st.Refreshes != 1 {
		t.Errorf("Refreshes = %d, want 1", st.Refreshes)
	}
	if st.LastDurationMs != 2000 {
		t.Errorf("LastDurationMs = %d, want 2000", st.LastDurationMs)
	}
}

// TestSafeDeleteStatsManagerZeroWhenNoCache covers the manager method
// with no cache created yet: it returns the zero snapshot instead of
// creating anything.
func TestSafeDeleteStatsManagerZeroWhenNoCache(t *testing.T) {
	m := &Manager{cfg: Config{}}
	st := m.SafeDeleteStats()
	if st != (SafeDeleteStats{}) {
		t.Errorf("SafeDeleteStats() with no cache = %+v, want the zero value", st)
	}
	if m.safeDeleteCache != nil {
		t.Errorf("SafeDeleteStats() created the cache")
	}
}
