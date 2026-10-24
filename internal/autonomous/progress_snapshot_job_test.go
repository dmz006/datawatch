package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// snapClock is a fake clock the runner reads and a fake wait the test drives:
// each Wait call reports the requested duration on waits and blocks until the
// test sends on tick (true = the wait elapsed) or ctx ends.
type snapClock struct {
	mu    sync.Mutex
	now   time.Time
	tick  chan struct{}
	waits chan time.Duration
}

func newSnapClock(now time.Time) *snapClock {
	return &snapClock{now: now, tick: make(chan struct{}), waits: make(chan time.Duration, 16)}
}

func (c *snapClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *snapClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *snapClock) Wait(ctx context.Context, d time.Duration) bool {
	c.waits <- d
	select {
	case <-ctx.Done():
		return false
	case <-c.tick:
		return true
	}
}

type countingCompute struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *countingCompute) fn(ctx context.Context) (ProgressMetrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return ProgressMetrics{}, c.err
	}
	return ProgressMetrics{CompletedPRDs: c.calls}, nil
}

func (c *countingCompute) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func newTestRunner(t *testing.T, clk *snapClock, cc *countingCompute, retention int) (*ProgressSnapshotRunner, string) {
	t.Helper()
	dir := t.TempDir()
	store, _, err := NewProgressSnapshotStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := NewProgressSnapshotRunner(store, cc.fn, func() int { return retention })
	r.Now = clk.Now
	r.Wait = clk.Wait
	return r, dir
}

func nextSnapWait(t *testing.T, ch <-chan time.Duration) time.Duration {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not reach its next wait")
		return 0
	}
}

func TestProgressSnapshotRunnerEnsureDaily(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{}
	r, _ := newTestRunner(t, clk, cc, 0)
	ctx := context.Background()

	rec, err := r.EnsureDaily(ctx)
	if err != nil || !rec {
		t.Fatalf("first call must record: rec=%v err=%v", rec, err)
	}
	clk.advance(3 * time.Hour)
	rec, err = r.EnsureDaily(ctx)
	if err != nil || rec {
		t.Fatalf("same day must not record again: rec=%v err=%v", rec, err)
	}
	if cc.count() != 1 {
		t.Fatalf("compute must not run when today has a snapshot, ran %d times", cc.count())
	}
	clk.advance(24 * time.Hour)
	if rec, _ := r.EnsureDaily(ctx); !rec {
		t.Fatal("a new UTC day must record")
	}
	if n := len(r.Store().List(0, clk.Now())); n != 2 {
		t.Fatalf("want 2 daily snapshots, got %d", n)
	}
}

func TestProgressSnapshotRunnerEnsureDailyPrunes(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{}
	r, _ := newTestRunner(t, clk, cc, 5)
	_ = r.Store().RecordDaily(day(1, 6), ProgressMetrics{})
	_ = r.Store().RecordDaily(day(18, 6), ProgressMetrics{})
	if _, err := r.EnsureDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := r.Store().List(0, clk.Now())
	if len(got) != 2 || got[0].Date != "2026-10-18" {
		t.Fatalf("retention 5 must prune the old snapshot: %+v", got)
	}
	// Even when today already has a snapshot the prune runs.
	_ = r.Store().RecordDaily(day(10, 6), ProgressMetrics{})
	if _, err := r.EnsureDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(r.Store().List(0, clk.Now())); n != 2 {
		t.Fatalf("prune must run on every check, got %d", n)
	}
}

func TestProgressSnapshotRunnerComputeError(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{err: errors.New("boom")}
	r, _ := newTestRunner(t, clk, cc, 0)
	if rec, err := r.EnsureDaily(context.Background()); err == nil || rec {
		t.Fatalf("want the compute error, got rec=%v err=%v", rec, err)
	}
	if err := r.RecordMilestone(context.Background(), "x"); err == nil {
		t.Fatal("milestone must report the compute error")
	}
	if n := len(r.Store().List(0, clk.Now())); n != 0 {
		t.Fatalf("a failed compute must record nothing, got %d", n)
	}
}

func TestProgressSnapshotRunnerMilestone(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{}
	r, _ := newTestRunner(t, clk, cc, 0)
	for i := 0; i < 2; i++ {
		if err := r.RecordMilestone(context.Background(), "completed:ab12cd34"); err != nil {
			t.Fatal(err)
		}
	}
	got := r.Store().List(0, clk.Now())
	if len(got) != 2 || got[0].Reason != "milestone" || got[0].Label != "completed:ab12cd34" {
		t.Fatalf("milestones always append: %+v", got)
	}
}

func TestProgressSnapshotRunnerLoop(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{}
	r, dir := newTestRunner(t, clk, cc, 0)
	r.Delay = 30 * time.Second
	r.Every = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	if d := nextSnapWait(t, clk.waits); d != 30*time.Second {
		t.Fatalf("first wait is the start delay, got %v", d)
	}
	clk.tick <- struct{}{} // start delay elapses -> today's snapshot
	if d := nextSnapWait(t, clk.waits); d != time.Hour {
		t.Fatalf("then re-check hourly, got %v", d)
	}
	if cc.count() != 1 {
		t.Fatalf("want a start snapshot, compute ran %d times", cc.count())
	}
	if _, err := os.Stat(filepath.Join(dir, "progress-snapshots.jsonl")); err != nil {
		t.Fatalf("snapshot must be on disk: %v", err)
	}

	// Hourly re-checks on the same day do not duplicate.
	for i := 0; i < 3; i++ {
		clk.advance(time.Hour)
		clk.tick <- struct{}{}
		nextSnapWait(t, clk.waits)
	}
	if n := len(r.Store().List(0, clk.Now())); n != 1 || cc.count() != 1 {
		t.Fatalf("same-day re-checks must not duplicate: snapshots=%d compute=%d", n, cc.count())
	}

	// The date rolling over is picked up by the next re-check.
	clk.advance(24 * time.Hour)
	clk.tick <- struct{}{}
	nextSnapWait(t, clk.waits)
	if n := len(r.Store().List(0, clk.Now())); n != 2 {
		t.Fatalf("new day must add a snapshot, got %d", n)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return when the context is cancelled")
	}
}

func TestProgressSnapshotRunnerLoopSurvivesFailure(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{err: errors.New("boom")}
	r, _ := newTestRunner(t, clk, cc, 0)
	r.Every = 0 // falls back to the hourly default
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	nextSnapWait(t, clk.waits)
	clk.tick <- struct{}{}
	if d := nextSnapWait(t, clk.waits); d != time.Hour {
		t.Fatalf("a failed check must still re-check hourly, got %v", d)
	}
	cc.mu.Lock()
	cc.err = nil
	cc.mu.Unlock()
	clk.tick <- struct{}{}
	nextSnapWait(t, clk.waits)
	if n := len(r.Store().List(0, clk.Now())); n != 1 {
		t.Fatalf("the retry must record the snapshot, got %d", n)
	}
	cancel()
	<-done
}

func TestProgressSnapshotRunnerCancelBeforeFirstCheck(t *testing.T) {
	clk := newSnapClock(day(20, 9))
	cc := &countingCompute{}
	r, _ := newTestRunner(t, clk, cc, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	nextSnapWait(t, clk.waits)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel during the start delay must stop the runner")
	}
	if cc.count() != 0 {
		t.Fatal("no snapshot after cancel")
	}
}

func TestProgressSnapshotWaitContext(t *testing.T) {
	if !waitContext(context.Background(), time.Millisecond) {
		t.Fatal("an elapsed wait reports true")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitContext(ctx, time.Hour) {
		t.Fatal("a cancelled wait reports false")
	}
}

type fakeProgressSource struct{ prds []*PRD }

func (f fakeProgressSource) ListPRDs() []*PRD { return f.prds }

func TestProgressSnapshotComputeReadsSettings(t *testing.T) {
	dir := t.TempDir()
	backlog := filepath.Join(dir, "README.md")
	if err := os.WriteFile(backlog, []byte("| BL1 | something |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got ProgressSnapshotSettings
	calls := 0
	compute := NewProgressCompute(fakeProgressSource{}, func() ProgressSnapshotSettings {
		calls++
		got = ProgressSnapshotSettings{Lanes: 3, GapHours: 2, BacklogPath: backlog}
		return got
	})
	m, err := compute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || m.Lanes != 3 {
		t.Fatalf("settings must be read per call and lanes applied: calls=%d lanes=%d", calls, m.Lanes)
	}
	// A missing backlog is not an error.
	missing := NewProgressCompute(fakeProgressSource{}, func() ProgressSnapshotSettings {
		return ProgressSnapshotSettings{BacklogPath: filepath.Join(dir, "none.md")}
	})
	if _, err := missing(context.Background()); err != nil {
		t.Fatalf("missing backlog must not fail: %v", err)
	}
	// A nil source and a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compute(ctx); err == nil {
		t.Fatal("a cancelled context must fail the compute")
	}
	nilSrc := NewProgressCompute(nil, func() ProgressSnapshotSettings { return ProgressSnapshotSettings{} })
	if _, err := nilSrc(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProgressSnapshotManagerMilestoneHook(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Without the feature enabled the hook is a no-op.
	m.recordProgressMilestone("none0001")
	m.progressSnapWG.Wait()

	r, err := m.EnableProgressSnapshots(func() ProgressSnapshotSettings { return ProgressSnapshotSettings{RetentionDays: 30} })
	if err != nil {
		t.Fatal(err)
	}
	m.recordProgressMilestone("ab12cd34")
	m.progressSnapWG.Wait()
	got := r.Store().List(0, time.Now())
	if len(got) != 1 || got[0].Reason != "milestone" || got[0].Label != "completed:ab12cd34" {
		t.Fatalf("completion must leave a milestone snapshot: %+v", got)
	}
	// Persisted next to the other autonomous data.
	if _, err := os.Stat(filepath.Join(m.store.dir, "progress-snapshots.jsonl")); err != nil {
		t.Fatalf("snapshots must live with the autonomous data: %v", err)
	}
}

func TestProgressSnapshotManagerHookToleratesFailure(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.EnableProgressSnapshots(func() ProgressSnapshotSettings { return ProgressSnapshotSettings{} })
	if err != nil {
		t.Fatal(err)
	}
	r.compute = func(context.Context) (ProgressMetrics, error) { return ProgressMetrics{}, errors.New("boom") }
	m.recordProgressMilestone("ab12cd34") // must only log
	m.progressSnapWG.Wait()
	if n := len(r.Store().List(0, time.Now())); n != 0 {
		t.Fatalf("failed snapshot records nothing, got %d", n)
	}
}

func TestProgressSnapshotManagerReloadsStore(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.EnableProgressSnapshots(func() ProgressSnapshotSettings { return ProgressSnapshotSettings{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.EnsureDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m2.EnableProgressSnapshots(func() ProgressSnapshotSettings { return ProgressSnapshotSettings{} })
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Store().HasDaily(time.Now()) {
		t.Fatal("a restarted manager must see today's snapshot and not take another")
	}
}

func TestProgressSnapshotManagerStartRunsRunner(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.EnableProgressSnapshots(func() ProgressSnapshotSettings { return ProgressSnapshotSettings{} })
	if err != nil {
		t.Fatal(err)
	}
	clk := newSnapClock(day(20, 9))
	r.Now = clk.Now
	r.Wait = clk.Wait
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	defer m.Stop()

	nextSnapWait(t, clk.waits)
	clk.tick <- struct{}{}
	nextSnapWait(t, clk.waits)
	if !r.Store().HasDaily(clk.Now()) {
		t.Fatal("Start must take the start snapshot")
	}
	// Stop cancels the manager context, which ends the runner's wait.
	m.Stop()
	select {
	case clk.tick <- struct{}{}:
		t.Fatal("the runner must have stopped waiting")
	case <-time.After(200 * time.Millisecond):
	}
}
