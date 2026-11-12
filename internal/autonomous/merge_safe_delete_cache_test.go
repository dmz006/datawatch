package autonomous

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// sdComputeCalls is a locked counter of compute calls, safe to use from the
// cache's background goroutines and the test body.
type sdComputeCalls struct {
	mu sync.Mutex
	n  int
}

func (c *sdComputeCalls) inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *sdComputeCalls) snap() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// safeDeleteFixedClock is a test clock: its current time is moved explicitly, so
// the cache tests never sleep.
type safeDeleteFixedClock struct {
	mu      sync.Mutex
	current time.Time
}

func newFixedClock(start time.Time) *safeDeleteFixedClock {
	return &safeDeleteFixedClock{current: start.Truncate(time.Second)}
}

func (c *safeDeleteFixedClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *safeDeleteFixedClock) advance(d time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(d)
	c.mu.Unlock()
}

// sdTestRepo is the cache's default repository-path key.
const sdTestRepo = "/tmp/safe-delete-cache-test/repo"

// sdTestReport builds a distinct cached report: its held entry names the
// repository and the report's version, so tests can tell copies apart.
func sdTestReport(repo, tag string) SafeDeleteReport {
	return SafeDeleteReport{
		Safe:      []SafeDeleteEntry{},
		Held:      []SafeDeleteHeld{{Branch: "automaton/fixture", Note: repo + " " + tag}},
		HeldDirty: 0,
	}
}

// sameReport compares two reports ignoring the serve-time Stale flag.
func sameReport(a, b SafeDeleteReport) bool {
	return a.ComputedAt == b.ComputedAt &&
		a.HeldDirty == b.HeldDirty &&
		reflect.DeepEqual(a.Safe, b.Safe) &&
		reflect.DeepEqual(a.Held, b.Held)
}

// waitFor blocks until ch closes, with a deadline so a goroutine
// scheduling delay cannot read as a failure. It never sleeps to
// simulate cache behaviour.
func sdWaitFor(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-ch:
			return
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// sdWaitUntil polls cond (from the test body) until it holds or a
// deadline passes; failures in cond's target are never swallowed but the
// wait itself is bounded.
func sdWaitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSafeDeleteCacheColdReadComputesOnce covers (a): a cold read computes
// the report synchronously, stores it with its computed time, and returns
// it with computed_at set and stale false; a second cold read after no
// store entry exists again must compute again.
func TestSafeDeleteCacheColdReadComputesOnce(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	calls := &sdComputeCalls{}
	var repoSeen string
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		calls.inc()
		repoSeen = repo
		return sdTestReport(repo, "cold"), nil
	}

	out, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("cold read: %v", err)
	}
	if calls.snap() != 1 {
		t.Fatalf("compute calls = %d, want 1", calls.snap())
	}
	if repoSeen != sdTestRepo {
		t.Errorf("compute repo = %q, want %q", repoSeen, sdTestRepo)
	}
	if out.Stale {
		t.Errorf("a fresh cold read reports stale = true")
	}
	if want := base.Format(time.RFC3339); out.ComputedAt != want {
		t.Errorf("ComputedAt = %q, want %q", out.ComputedAt, want)
	}

	// A different (also cold) repository key computes independently.
	other := "/tmp/safe-delete-cache-test/repo-two"
	otherOut, err := c.get(context.Background(), other)
	if err != nil {
		t.Fatalf("cold read for the other repo: %v", err)
	}
	if calls.snap() != 2 {
		t.Errorf("compute calls after the other repo's cold read = %d, want 2", calls.snap())
	}
	if otherOut.Held[0].Note != other+" cold" {
		t.Errorf("other-repo report note = %q, want %q", otherOut.Held[0].Note, other+" cold")
	}
	if out.Held[0].Note != sdTestRepo+" cold" {
		t.Errorf("original report note = %q, want %q", out.Held[0].Note, sdTestRepo+" cold")
	}
}

// TestSafeDeleteCacheWarmReadWithinLifetimeMakesZeroComputeCalls covers
// (b): reads while the entry is within safeDeleteCacheTTL return the
// cached report at once, without computing.
func TestSafeDeleteCacheWarmReadWithinLifetimeMakesZeroComputeCalls(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	calls := &sdComputeCalls{}
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		calls.inc()
		return sdTestReport(repo, "first"), nil
	}

	first, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("cold read: %v", err)
	}
	callsAfterCold := calls.snap()

	clock.advance(safeDeleteCacheTTL - time.Second) // still within the lifetime
	second, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("warm read: %v", err)
	}
	if calls.snap() != callsAfterCold {
		t.Fatalf("compute calls = %d, want %d (a warm read must not compute)", calls.snap(), callsAfterCold)
	}
	if second.Stale {
		t.Errorf("a warm read within the lifetime reports stale = true")
	}
	if want := base.Format(time.RFC3339); second.ComputedAt != want {
		t.Errorf("warm-read ComputedAt = %q, want the original %q", second.ComputedAt, want)
	}
	if !sameReport(second, first) {
		t.Fatalf("warm read = %+v, want the cached report %+v", second, first)
	}
}

// TestSafeDeleteCacheStaleReadReturnsAtOnceWithOneBackgroundRefresh covers
// (b) stale: a read after the clock passes the lifetime returns the cached
// report at once with stale true, exactly one background refresh runs, a
// read during that refresh starts none, and the refreshed entry is served
// fresh afterwards.
func TestSafeDeleteCacheStaleReadReturnsAtOnceWithOneBackgroundRefresh(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	refreshStarted := make(chan struct{})
	refreshReleased := make(chan struct{})
	unblock := make(chan struct{})
	calls := &sdComputeCalls{}
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		calls.inc()
		if calls.snap() == 1 {
			return sdTestReport(repo, "v1"), nil
		}
		// The background refresh: hold it in flight until the test has
		// finished its assertions, then succeed.
		close(refreshStarted)
		<-unblock
		close(refreshReleased)
		return sdTestReport(repo, "v2"), nil
	}

	v1, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("cold read: %v", err)
	}
	if calls.snap() != 1 || v1.Stale {
		t.Fatalf("cold read: calls=%d stale=%v", calls.snap(), v1.Stale)
	}

	clock.advance(safeDeleteCacheTTL + time.Second) // past the lifetime
	stale1, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("stale read 1: %v", err)
	}
	if !stale1.Stale || !sameReport(stale1, v1) {
		t.Fatalf("stale read 1 = %+v (stale=%v), want the cached report %+v served stale", stale1, stale1.Stale, v1)
	}
	if want := base.Format(time.RFC3339); stale1.ComputedAt != want {
		t.Errorf("stale read 1 ComputedAt = %q, want the original %q", stale1.ComputedAt, want)
	}

	// The stale read started exactly one background refresh ...
	sdWaitFor(t, refreshStarted, "the stale read to start a background refresh")
	// ... and a read during that refresh starts none (no second compute).
	stale2, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("stale read 2: %v", err)
	}
	if !stale2.Stale || !sameReport(stale2, v1) {
		t.Fatalf("stale read 2 during the refresh = %+v (stale=%v), want the cached report %+v", stale2, stale2.Stale, v1)
	}

	close(unblock)
	sdWaitFor(t, refreshReleased, "the background refresh to compute")
	sdWaitUntil(t, "the refresh to store its report", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.refreshing[sdTestRepo]
	})
	if calls.snap() != 2 {
		t.Fatalf("compute calls after the refresh = %d, want 2 (exactly one background refresh)", calls.snap())
	}
	refreshed, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("read after the refresh: %v", err)
	}
	if refreshed.Stale {
		t.Errorf("the refreshed report is served stale")
	}
	if note := refreshed.Held[0].Note; note != sdTestRepo+" v2" {
		t.Errorf("refreshed report note = %q, want %q", note, sdTestRepo+" v2")
	}
	if calls.snap() != 2 {
		t.Errorf("compute calls = %d, want 2 (the post-refresh read must be warm)", calls.snap())
	}
}

// TestSafeDeleteCacheFailedRefreshKeepsLastGoodReport covers (c): a failed
// background refresh keeps the last good report and its computed_at, and
// the entry stays stale so the next read tries again.
func TestSafeDeleteCacheFailedRefreshKeepsLastGoodReport(t *testing.T) {
	var errRefresh = errors.New("refresh boom")
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	failStarted := make(chan struct{})
	failReleased := make(chan struct{})
	retryStarted := make(chan struct{})
	retryReleased := make(chan struct{})
	failUnblock := make(chan struct{})
	retryUnblock := make(chan struct{})
	calls := &sdComputeCalls{}
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		calls.inc()
		switch calls.snap() {
		case 1:
			return sdTestReport(repo, "good"), nil
		case 2:
			// The first (failing) background refresh: hold the failure
			// in flight until the test asserts, then return the error.
			close(failStarted)
			<-failUnblock
			close(failReleased)
			return SafeDeleteReport{}, errRefresh
		default:
			// The retry the next stale read must start.
			close(retryStarted)
			<-retryUnblock
			close(retryReleased)
			return sdTestReport(repo, "recovered"), nil
		}
	}

	good, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("cold read: %v", err)
	}
	goodComputedAt := good.ComputedAt
	if goodComputedAt == "" {
		t.Fatalf("the cold read did not stamp ComputedAt")
	}

	clock.advance(safeDeleteCacheTTL + time.Second)
	stale1, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("stale read 1: %v", err)
	}
	if !stale1.Stale || !sameReport(stale1, good) {
		t.Fatalf("stale read 1 = %+v (stale=%v), want the cached good report served stale", stale1, stale1.Stale)
	}
	sdWaitFor(t, failStarted, "the failing refresh to start")
	close(failUnblock)
	sdWaitFor(t, failReleased, "the failing refresh to return its error")
	// failReleased closes before compute returns, so the refresh is still
	// registered as running for a moment; the next read only retries once
	// that registration is gone.
	sdWaitUntil(t, "the failed refresh to finish", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.refreshing[sdTestRepo]
	})

	// The failed refresh must have kept the last good report and its
	// computed time, and the entry must still be stale.
	c.mu.Lock()
	e, ok := c.entries[sdTestRepo]
	c.mu.Unlock()
	if !ok {
		t.Fatalf("the failed refresh dropped the last good entry")
	}
	if !sameReport(e.report, good) {
		t.Fatalf("the failed refresh replaced the last good report: %+v", e.report)
	}
	if e.computedAt.UTC().Format(time.RFC3339) != goodComputedAt {
		t.Fatalf("the failed refresh changed computed_at to %v, want %v", e.computedAt, goodComputedAt)
	}
	if calls.snap() != 2 {
		t.Fatalf("compute calls = %d, want 2 (cold + one failed refresh)", calls.snap())
	}

	// The entry stays stale: the next read tries again.
	stale2, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("stale read 2: %v", err)
	}
	if !stale2.Stale || !sameReport(stale2, good) {
		t.Fatalf("stale read 2 = %+v (stale=%v), want the cached good report served stale", stale2, stale2.Stale)
	}
	sdWaitFor(t, retryStarted, "the retry refresh after the failed one")
	if calls.snap() != 3 {
		t.Fatalf("compute calls = %d, want 3 (the next read must retry the refresh)", calls.snap())
	}
	close(retryUnblock)
	sdWaitFor(t, retryReleased, "the recovered retry to compute")
	sdWaitUntil(t, "the recovered retry to store its report", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.refreshing[sdTestRepo]
	})
	if calls.snap() != 3 {
		t.Fatalf("compute calls after the recovered retry = %d, want 3", calls.snap())
	}
	recovered, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("read after the recovered retry: %v", err)
	}
	if note := recovered.Held[0].Note; note != sdTestRepo+" recovered" {
		t.Errorf("recovered report note = %q, want %q", note, sdTestRepo+" recovered")
	}
}

// TestSafeDeleteCacheConcurrentColdReadsRunOneCompute covers the single-
// flight rule: concurrent cold reads of the same repository share one
// computation and all get the same result.
func TestSafeDeleteCacheConcurrentColdReadsRunOneCompute(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	computeStarted := make(chan struct{})
	unblock := make(chan struct{})
	calls := &sdComputeCalls{}
	c := newSafeDeleteCache()
	c.now = clock.now
	const readers = 4
	type result struct {
		report SafeDeleteReport
		err    error
	}
	results := make(chan result, readers)
	var wg sync.WaitGroup
	reader := func() {
		defer wg.Done()
		r, err := c.get(context.Background(), sdTestRepo)
		results <- result{r, err}
	}

	// The first reader is in its cold computation (held on unblock) before
	// the rest are started, so every later reader necessarily finds the
	// in-flight computation still open and joins it: exactly one compute.
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		if calls.snap() == 0 {
			close(computeStarted)
		}
		<-unblock
		calls.inc()
		return sdTestReport(repo, "once"), nil
	}

	wg.Add(1)
	go reader()
	sdWaitFor(t, computeStarted, "the first cold computation to start")
	for i := 1; i < readers; i++ {
		wg.Add(1)
		go reader()
	}
	close(unblock)
	wg.Wait()
	close(results)

	if calls.snap() != 1 {
		t.Fatalf("compute calls = %d, want 1 (concurrent cold reads share one computation)", calls.snap())
	}
	var first SafeDeleteReport
	seen := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent cold read %d: %v", seen, r.err)
		}
		if seen == 0 {
			first = r.report
		} else if !sameReport(r.report, first) {
			t.Fatalf("concurrent cold read %d = %+v, want the shared result %+v", seen, r.report, first)
		}
		seen++
	}
	if seen != readers {
		t.Fatalf("collected %d results, want %d", seen, readers)
	}
}

// TestSafeDeleteCacheChangedRepositoryRecomputes covers per-repository
// keying: a different repository path has no cached entry and therefore
// recomputes, while the first repository's entry stays warm.
func TestSafeDeleteCacheChangedRepositoryRecomputes(t *testing.T) {
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	calls := &sdComputeCalls{}
	c := newSafeDeleteCache()
	c.now = clock.now
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		calls.inc()
		return sdTestReport(repo, "x"), nil
	}

	first, err1 := c.get(context.Background(), sdTestRepo)
	second, err2 := c.get(context.Background(), sdTestRepo+"/changed")
	if err1 != nil || err2 != nil {
		t.Fatalf("reads: %v, %v", err1, err2)
	}
	if calls.snap() != 2 {
		t.Fatalf("compute calls = %d, want 2 (a changed repository path recomputes)", calls.snap())
	}
	if first.Held[0].Note != sdTestRepo+" x" || second.Held[0].Note != sdTestRepo+"/changed x" {
		t.Fatalf("reports not keyed by repository: %+v / %+v", first.Held[0], second.Held[0])
	}
	before := calls.snap()
	again, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("re-read of the first repo: %v", err)
	}
	if calls.snap() != before {
		t.Fatalf("compute calls = %d, want %d (the first repo's entry stays warm)", calls.snap(), before)
	}
	if !sameReport(again, first) {
		t.Fatalf("re-read of the first repo = %+v, want the cached %+v", again, first)
	}
}

// TestSafeDeleteCacheColdFailureSurfacesUnwrapped covers (d): a failed
// cold computation returns the error to every concurrent reader, unwrapped
// so errors.Is still matches, and nothing is cached. The second reader is
// started while the first is held inside the failing computation, so the
// second is guaranteed to share that in-flight failure instead of running
// its own compute.
func TestSafeDeleteCacheColdFailureSurfacesUnwrapped(t *testing.T) {
	var errBoom = errors.New("cold boom")
	base := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)
	clock := newFixedClock(base)
	started := make(chan struct{})
	unblock := make(chan struct{})
	c := newSafeDeleteCache()
	c.now = clock.now
	calls := &sdComputeCalls{}
	startedOnce := sync.OnceFunc(func() { close(started) })
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) {
		startedOnce()
		<-unblock
		calls.inc()
		return SafeDeleteReport{}, errBoom
	}

	var wg sync.WaitGroup
	reader := func() {
		defer wg.Done()
		_, err := c.get(context.Background(), sdTestRepo)
		if !errors.Is(err, errBoom) {
			t.Errorf("cold failure: errors.Is(err, errBoom) = false for %v", err)
		}
	}

	wg.Add(1)
	go reader()
	sdWaitFor(t, started, "the cold computation to start")
	wg.Add(1)
	go reader()
	close(unblock)
	wg.Wait()

	if calls.snap() < 1 || calls.snap() > 2 {
		t.Errorf("compute calls = %d, want 1 (shared in-flight failure) or 2 (second reader legitimately recomputed after the failure)", calls.snap())
	}
	c.mu.Lock()
	_, hasEntry := c.entries[sdTestRepo]
	c.mu.Unlock()
	if hasEntry {
		t.Errorf("a failed cold computation must not cache a report")
	}

	// A later cold read after the failure must compute again (there is
	// nothing cached to share) — with a compute that now succeeds.
	ok := sdTestReport(sdTestRepo, "recovered")
	ok.ComputedAt = base.UTC().Format(time.RFC3339)
	c.compute = func(ctx context.Context, repo string) (SafeDeleteReport, error) { return ok, nil }
	out, err := c.get(context.Background(), sdTestRepo)
	if err != nil {
		t.Fatalf("post-failure cold read: %v", err)
	}
	if !sameReport(out, ok) {
		t.Errorf("post-failure cold read = %+v, want %+v", out, ok)
	}
}

// TestSafeDeleteCacheManagerKeepsNoBaseRepoError covers the Manager seam:
// with no worktree base repo configured, MergeSafeToDelete must still
// return ErrMergeNoBaseRepo before touching the cache.
func TestSafeDeleteCacheManagerKeepsNoBaseRepoError(t *testing.T) {
	m := &Manager{cfg: Config{}}
	_, err := m.MergeSafeToDelete(context.Background())
	if !errors.Is(err, ErrMergeNoBaseRepo) {
		t.Fatalf("MergeSafeToDelete without a base repo: errors.Is(err, ErrMergeNoBaseRepo) = false for %v", err)
	}
	if m.safeDeleteCache != nil {
		t.Errorf("MergeSafeToDelete without a base repo created the cache")
	}
}
