// Progress snapshot runner — takes the daily snapshot (and milestone snapshots
// on demand) and prunes old ones. The clock, the compute function and the wait
// between checks are all injected, so tests drive it with a fake clock and the
// manager wires the real ones.

package autonomous

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	// progressSnapshotStartDelay is how long after start the first check runs,
	// so a daemon boot is not slowed by computing the metrics.
	progressSnapshotStartDelay = 30 * time.Second
	// progressSnapshotInterval is how often the runner re-checks that today has
	// a snapshot (it also covers the date rolling over while the daemon runs).
	progressSnapshotInterval = time.Hour
)

// ProgressSnapshotSettings are the progress config values a snapshot needs,
// read each time one is taken so a config change applies without a restart.
type ProgressSnapshotSettings struct {
	RetentionDays int       // 0 = 90
	Lanes         int       // 0 = 5
	GapHours      float64   // 0 = 3
	Since         time.Time // zero = the first record's creation date
	BacklogPath   string    // resolved path; empty or unreadable = no backlog rows
}

// ProgressComputeFunc computes the current metrics.
type ProgressComputeFunc func(ctx context.Context) (ProgressMetrics, error)

// ProgressSnapshotRunner records progress snapshots. The zero value is not
// usable; build one with NewProgressSnapshotRunner.
type ProgressSnapshotRunner struct {
	store     *ProgressSnapshotStore
	compute   ProgressComputeFunc
	retention func() int

	// Now is the clock, Wait blocks for d or until ctx is done (false = ctx done).
	// Both default to the real ones and are replaced by tests.
	Now   func() time.Time
	Wait  func(ctx context.Context, d time.Duration) bool
	Delay time.Duration // first check after start
	Every time.Duration // re-check period

	mu sync.Mutex // serializes snapshot taking
}

// NewProgressSnapshotRunner returns a runner that records into store using
// compute. retention returns the retention in days (nil or 0 = 90).
func NewProgressSnapshotRunner(store *ProgressSnapshotStore, compute ProgressComputeFunc, retention func() int) *ProgressSnapshotRunner {
	return &ProgressSnapshotRunner{
		store:     store,
		compute:   compute,
		retention: retention,
		Now:       time.Now,
		Wait:      waitContext,
		Delay:     progressSnapshotStartDelay,
		Every:     progressSnapshotInterval,
	}
}

// waitContext sleeps for d, returning false if ctx ended first. The timer is
// local and always stopped.
func waitContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Store returns the snapshot store the runner records into.
func (r *ProgressSnapshotRunner) Store() *ProgressSnapshotStore { return r.store }

// EnsureDaily records today's snapshot when none exists for today's UTC date,
// then prunes snapshots past the retention. It reports whether it recorded one.
func (r *ProgressSnapshotRunner) EnsureDaily(ctx context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.Now()
	recorded := false
	if !r.store.HasDaily(now) {
		m, err := r.compute(ctx)
		if err != nil {
			return false, err
		}
		if err := r.store.RecordDaily(now, m); err != nil {
			return false, err
		}
		recorded = true
	}
	days := 0
	if r.retention != nil {
		days = r.retention()
	}
	r.store.Prune(days, now)
	return recorded, nil
}

// RecordMilestone records a milestone snapshot taken now, tagged with label.
func (r *ProgressSnapshotRunner) RecordMilestone(ctx context.Context, label string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, err := r.compute(ctx)
	if err != nil {
		return err
	}
	return r.store.RecordMilestone(r.Now(), m, label)
}

// Run takes the first check after Delay, then re-checks every Every, until ctx
// is done. A failed check is logged and tried again at the next one.
func (r *ProgressSnapshotRunner) Run(ctx context.Context) {
	every := r.Every
	if every <= 0 {
		every = progressSnapshotInterval
	}
	wait := r.Delay
	for r.Wait(ctx, wait) {
		if _, err := r.EnsureDaily(ctx); err != nil {
			log.Printf("[autonomous] progress snapshot: %v", err)
		}
		wait = every
	}
}

// NewProgressCompute returns a compute function that reads the records from src
// and the settings from settings on each call.
func NewProgressCompute(src ProgressSource, settings func() ProgressSnapshotSettings) ProgressComputeFunc {
	return func(ctx context.Context) (ProgressMetrics, error) {
		if err := ctx.Err(); err != nil {
			return ProgressMetrics{}, err
		}
		s := settings()
		var rows []BacklogRow
		if strings.TrimSpace(s.BacklogPath) != "" {
			// A missing backlog is not an error: the engine reports
			// backlog_available=false and counts no unfiled rows.
			if loaded, err := LoadBacklog(s.BacklogPath); err == nil {
				rows = loaded
			}
		}
		opts := ProgressOptions{Since: s.Since, Lanes: s.Lanes, GapHours: s.GapHours}
		return CollectProgress(src, rows, nil, opts), nil
	}
}

// EnableProgressSnapshots opens the snapshot store next to prds.jsonl and
// installs a runner that Start launches (the first check shortly after start,
// then hourly) and that records a milestone snapshot whenever a record is marked
// completed. settings is read on every snapshot, so config changes apply live.
// It returns the runner so callers can read the store for the trend view.
func (m *Manager) EnableProgressSnapshots(settings func() ProgressSnapshotSettings) (*ProgressSnapshotRunner, error) {
	store, skipped, err := NewProgressSnapshotStore(m.store.dir)
	if err != nil {
		return nil, err
	}
	if skipped > 0 {
		log.Printf("[autonomous] progress snapshots: skipped %d unreadable line(s) in %s", skipped, progressSnapshotFile)
	}
	r := NewProgressSnapshotRunner(store, NewProgressCompute(m.store, settings), func() int { return settings().RetentionDays })
	m.mu.Lock()
	m.progressSnaps = r
	m.mu.Unlock()
	return r, nil
}

// recordProgressMilestone saves a milestone snapshot for a record that was just
// marked completed. It never blocks the caller and a failure is only logged.
func (m *Manager) recordProgressMilestone(prdID string) {
	m.mu.Lock()
	r := m.progressSnaps
	m.mu.Unlock()
	if r == nil {
		return
	}
	m.progressSnapWG.Add(1)
	go func() {
		defer m.progressSnapWG.Done()
		if err := r.RecordMilestone(context.Background(), "completed:"+prdID); err != nil {
			log.Printf("[autonomous] progress snapshot: milestone for prd=%s: %v", prdID, err)
		}
	}()
}
