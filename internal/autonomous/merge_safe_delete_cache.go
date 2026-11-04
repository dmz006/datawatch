package autonomous

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// safeDeleteCacheTTL is how long a repository's safe-to-delete report is
// served from cache without recomputing: a read finds the entry within
// this window and returns it immediately (stale = false). A read that
// finds the entry older than this returns the cached report at once
// (stale = true) and starts one background refresh.
const safeDeleteCacheTTL = 60 * time.Second

// safeDeleteRefreshTimeout bounds a background refresh so a slow git run
// cannot hang the manager forever while reads keep serving the last good
// report.
const safeDeleteRefreshTimeout = 60 * time.Second

// safeDeleteSlowWarn is the duration a safe-to-delete computation may take
// before it is counted as a slow report and a single warning line is
// logged (duration, threshold and worktree count — never names or paths).
const safeDeleteSlowWarn = 3 * time.Second

// safeDeleteCacheEntry is one cached report plus the clock time it was
// computed at (the value reported as computed_at).
type safeDeleteCacheEntry struct {
	report     SafeDeleteReport
	computedAt time.Time // UTC
}

// safeDeleteColdCall is one in-flight cold computation that concurrent
// readers share; done is closed exactly once, after report/err are set.
type safeDeleteColdCall struct {
	done   chan struct{}
	report SafeDeleteReport
	err    error
}

// safeDeleteCache caches MergeSafeToDelete reports per repository path
// for safeDeleteCacheTTL, refreshing in the background once the entry is
// stale. The clock and the compute function are injectable so the
// behaviour is testable without real git and without sleeping.
type safeDeleteCache struct {
	mu      sync.Mutex
	now     func() time.Time
	compute func(ctx context.Context, repo string) (SafeDeleteReport, error)

	entries    map[string]safeDeleteCacheEntry
	inflight   map[string]*safeDeleteColdCall
	refreshing map[string]bool // one background refresh per repository

	statsMu sync.Mutex
	stats   SafeDeleteStats
}

func newSafeDeleteCache() *safeDeleteCache {
	return &safeDeleteCache{
		now: time.Now,
		compute: func(context.Context, string) (SafeDeleteReport, error) {
			return SafeDeleteReport{}, errors.New("safeDeleteCache: no compute wired")
		},
		entries:    make(map[string]safeDeleteCacheEntry),
		inflight:   make(map[string]*safeDeleteColdCall),
		refreshing: make(map[string]bool),
	}
}

// get returns the report for repo:
//
//   - cold (no entry yet): compute synchronously, single-flight, sharing
//     one computation (or one error, unwrapped so errors.Is matches)
//     with any reader that is racing it. The entry check and the
//     in-flight decision happen under one lock, so a reader that misses
//     the stored entry always sees the in-flight computation instead of
//     starting a second one.
//   - fresh (age <= safeDeleteCacheTTL): the cached report at once,
//     stale = false, no compute.
//   - stale (age > safeDeleteCacheTTL): the cached report at once,
//     stale = true, and exactly one background refresh while none is in
//     flight; a refresh failure keeps the last good report.
func (c *safeDeleteCache) get(ctx context.Context, repo string) (SafeDeleteReport, error) {
	c.mu.Lock()
	if e, ok := c.entries[repo]; ok {
		fresh := c.now().Sub(e.computedAt) <= safeDeleteCacheTTL
		if !fresh && !c.refreshing[repo] {
			// Stale and unrefreshed: start the one background refresh the
			// repository gets at a time.
			c.refreshing[repo] = true
			c.mu.Unlock()
			c.startRefresh(repo)
		} else {
			c.mu.Unlock()
		}
		out := e.report
		out.Stale = !fresh
		out.ComputedAt = e.computedAt.UTC().Format(time.RFC3339)
		return out, nil
	}

	// No entry: decide the winner in the same critical section as the
	// entry check, so no reader can both miss the entry and miss the
	// in-flight computation.
	call := &safeDeleteColdCall{done: make(chan struct{})}
	winner, isWinner := c.inflight[repo], false
	if winner == nil {
		c.inflight[repo] = call
		winner, isWinner = call, true
	}
	c.mu.Unlock()

	if !isWinner {
		// Another reader is already computing for this repository: share
		// its outcome instead of running a second computation.
		select {
		case <-winner.done:
		case <-ctx.Done():
			return SafeDeleteReport{}, ctx.Err()
		}
		if winner.err != nil {
			return SafeDeleteReport{}, winner.err
		}
		return winner.report, nil
	}

	start := c.now()
	report, err := c.compute(ctx, repo)
	computedAt := c.now().UTC()
	stamped := report
	stamped.ComputedAt = computedAt.Format(time.RFC3339)
	c.recordResult(start, computedAt, report, err)
	c.mu.Lock()
	if c.inflight[repo] == call {
		// Still the registered computation: publish a successful result
		// (a failure is never cached) and retire the in-flight slot.
		if err == nil {
			c.entries[repo] = safeDeleteCacheEntry{report: stamped, computedAt: computedAt}
		}
		delete(c.inflight, repo)
	}
	call.report, call.err = stamped, err
	c.mu.Unlock()
	close(call.done)
	return stamped, err
}

// startRefresh runs one bounded background refresh (the caller already
// marked the repository in-flight) and either replaces the entry on
// success or keeps the last good report on failure, so a failing refresh
// neither drops the report nor blocks later reads.
func (c *safeDeleteCache) startRefresh(repo string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), safeDeleteRefreshTimeout)
		defer cancel()
		start := c.now()
		report, err := c.compute(ctx, repo)
		finished := c.now().UTC()
		c.recordResult(start, finished, report, err)
		c.mu.Lock()
		c.refreshing[repo] = false
		if err == nil {
			c.entries[repo] = safeDeleteCacheEntry{report: report, computedAt: finished}
		}
		c.mu.Unlock()
		if err != nil {
			log.Printf("[autonomous] safe-to-delete cache refresh: repo %q: %v", repo, err)
		}
	}()
}

// statsSnapshot returns a copy of the cache's counters.
func (c *safeDeleteCache) statsSnapshot() SafeDeleteStats {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	return c.stats
}

// recordResult updates the cache counters for one finished computation
// (cold or background): a success counts as a refresh and records its
// completion time and worktree count, a failure counts as a refresh
// failure, the duration of the most recent computation is always kept, and
// a computation longer than safeDeleteSlowWarn counts as a slow report and
// is logged exactly once — with the duration in milliseconds, the
// threshold and the worktree count, never the repository, branch or
// worktree paths.
func (c *safeDeleteCache) recordResult(start, end time.Time, report SafeDeleteReport, err error) {
	dur := end.Sub(start)
	c.statsMu.Lock()
	if err == nil {
		c.stats.Refreshes++
		c.stats.LastComputedAt = end
		c.stats.Worktrees = len(report.Safe) + len(report.Held)
	} else {
		c.stats.RefreshFailures++
	}
	c.stats.LastDurationMs = dur.Milliseconds()
	slow := dur > safeDeleteSlowWarn
	if slow {
		c.stats.SlowReports++
	}
	worktrees := c.stats.Worktrees
	c.statsMu.Unlock()
	if slow {
		log.Printf("[autonomous] safe-to-delete computation took %d ms, above the %s warning threshold (%d worktrees)",
			dur.Milliseconds(), safeDeleteSlowWarn, worktrees)
	}
}
